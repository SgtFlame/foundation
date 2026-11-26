package text

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"time"
)

// The child process that parses one transcript (forge 18e), as forge's
// mime.ChildPDF reads one PDF: a transcript file comes from a stranger and
// may be built to attack its reader, so a server never parses it in its
// own process. ChildTranscripts parses it in a child process of the same
// binary with an empty environment but the request, stdin the bytes,
// stdout the JSON answer (capped), a time limit, a heap limit and two
// children at a time. A crash, a loop or a decompression bomb ends the
// child, not the server, and the transcript is reported unreadable.

// Limits on the child.
const (
	// TranscriptTimeout is how long a transcript's child may take.
	TranscriptTimeout = 10 * time.Second
	// TranscriptMemory is the heap a transcript's child may use before it
	// is ended.
	TranscriptMemory = 256 << 20
	// transcriptAnswerMax caps the child's stdout: MaxTranscriptChars of
	// text at most 4 bytes a character (doubled for a quote escaped), with
	// every line's speaker, time and keys, and the speakers.
	transcriptAnswerMax = MaxTranscriptChars*4 + MaxTranscriptLines*(maxSpeakerChars*4+128) + MaxTranscriptLines*(maxSpeakerChars*4+8) + 4096
)

const (
	transcriptChildEnv = "FOUNDATION_TRANSCRIPT_CHILD"
	transcriptNameEnv  = "FOUNDATION_TRANSCRIPT_NAME"
	transcriptTypeEnv  = "FOUNDATION_TRANSCRIPT_TYPE"
)

// TranscriptReader parses a transcript: ChildTranscripts in a server.
type TranscriptReader func(ctx context.Context, name, contentType string, raw []byte) (Transcript, error)

// transcriptSlots bounds how many transcript children run at once in this
// process.
var transcriptSlots = make(chan struct{}, 2)

// transcriptWait is how long the parent waits for a child before killing
// it; a variable so tests can shorten it.
var transcriptWait = TranscriptTimeout + 2*time.Second

// TranscriptChildRequested tells whether this process was started to parse
// one transcript: a program offering ChildTranscripts calls it first thing
// in main (and a test binary in TestMain) and then ServeTranscriptChild.
func TranscriptChildRequested() bool { return os.Getenv(transcriptChildEnv) == "1" }

type transcriptAnswer struct {
	Transcript *Transcript `json:"transcript,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// ServeTranscriptChild parses one transcript from stdin (its name and
// content type from the environment), writes it as JSON to stdout and
// exits. It limits itself as well: its heap, its stack and its time,
// ending the process when one is passed.
func ServeTranscriptChild() {
	debug.SetMemoryLimit(TranscriptMemory / 2)
	debug.SetMaxStack(64 << 20)
	done := func(a transcriptAnswer, code int) {
		e := json.NewEncoder(os.Stdout)
		e.SetEscapeHTML(false)
		_ = e.Encode(a)
		os.Exit(code)
	}
	go func() {
		sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		start := time.Now()
		for range time.Tick(20 * time.Millisecond) {
			metrics.Read(sample)
			if sample[0].Value.Kind() == metrics.KindUint64 && sample[0].Value.Uint64() > TranscriptMemory {
				done(transcriptAnswer{Error: "the transcript needs too much memory to read"}, 3)
			}
			if time.Since(start) > TranscriptTimeout {
				done(transcriptAnswer{Error: "the transcript takes too long to read"}, 3)
			}
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, MaxTranscriptBytes+1))
	if err != nil {
		done(transcriptAnswer{Error: "the transcript could not be read"}, 2)
	}
	t, err := ParseTranscript(os.Getenv(transcriptNameEnv), os.Getenv(transcriptTypeEnv), raw)
	if err != nil {
		done(transcriptAnswer{Error: err.Error()}, 0)
	}
	done(transcriptAnswer{Transcript: &t}, 0)
}

// ChildTranscripts parses transcripts in child processes of exe (this
// program, which calls TranscriptChildRequested and ServeTranscriptChild
// first thing): an empty environment but for the request, stdin the file,
// stdout at most the answer's cap, killed after TranscriptTimeout (and a
// little), two at a time.
func ChildTranscripts(exe string) TranscriptReader {
	return func(ctx context.Context, name, contentType string, raw []byte) (Transcript, error) {
		if len(raw) > MaxTranscriptBytes {
			return Transcript{}, ErrTranscriptTooLarge
		}
		select {
		case transcriptSlots <- struct{}{}:
		case <-ctx.Done():
			return Transcript{}, ctx.Err()
		}
		defer func() { <-transcriptSlots }()
		run, cancel := context.WithTimeout(ctx, transcriptWait)
		defer cancel()
		cmd := exec.CommandContext(run, exe)
		cmd.Env = []string{transcriptChildEnv + "=1", transcriptNameEnv + "=" + envValue(name), transcriptTypeEnv + "=" + envValue(contentType)}
		cmd.Dir = os.TempDir()
		cmd.Stdin = bytes.NewReader(raw)
		var out capped
		out.max = transcriptAnswerMax
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		var a transcriptAnswer
		if jerr := json.Unmarshal(out.b.Bytes(), &a); jerr != nil || a.Transcript == nil && a.Error == "" {
			if ctx.Err() != nil {
				return Transcript{}, ctx.Err()
			}
			if run.Err() != nil {
				return Transcript{}, errors.New("the transcript takes too long to read")
			}
			if err != nil {
				return Transcript{}, errors.New("the transcript could not be read safely")
			}
			return Transcript{}, errors.New("the transcript could not be read")
		}
		if a.Error != "" {
			return Transcript{}, errors.New(a.Error)
		}
		return *a.Transcript, nil
	}
}

// envValue is a name or content type fit for the child's environment: no
// NUL or line breaks, at most 255 bytes.
func envValue(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == 0 || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	if len(s) > 255 {
		s = strings.ToValidUTF8(s[:255], "")
	}
	return s
}

// capped is a buffer that keeps at most max bytes.
type capped struct {
	b   bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		c.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
