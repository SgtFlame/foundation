package ical

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
	"time"
)

// The child process that parses one .ics (forge 28b), as
// text.ChildTranscripts reads one transcript: a calendar file comes from a
// stranger (a mail attachment, a subscribed feed) and may be built to
// attack its reader, so a server never parses it in its own process.
// Child parses it in a child process of the same binary with an empty
// environment but the request, stdin the bytes, stdout the JSON answer
// (capped), 5 seconds, 128 MB of heap and two children at a time.

// Limits on the child.
const (
	ChildTimeout = 5 * time.Second
	ChildMemory  = 128 << 20
	answerMax    = 8 << 20
)

const childEnv = "FOUNDATION_ICAL_CHILD"

// Reader parses .ics bytes: Child in a server.
type Reader func(ctx context.Context, raw []byte) (Parsed, error)

var slots = make(chan struct{}, 2)

// childWait is how long the parent waits; a variable for tests.
var childWait = ChildTimeout + 2*time.Second

// ChildRequested tells whether this process was started to parse one
// .ics: a program offering Child calls it first thing in main (and a test
// binary in TestMain) and then ServeChild.
func ChildRequested() bool { return os.Getenv(childEnv) == "1" }

type answer struct {
	Parsed *Parsed `json:"parsed,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// ServeChild parses one .ics from stdin, writes it as JSON to stdout and
// exits, limiting its own heap, stack and time.
func ServeChild() {
	debug.SetMemoryLimit(ChildMemory / 2)
	debug.SetMaxStack(32 << 20)
	done := func(a answer, code int) {
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
			if sample[0].Value.Kind() == metrics.KindUint64 && sample[0].Value.Uint64() > ChildMemory {
				done(answer{Error: "the calendar file needs too much memory to read"}, 3)
			}
			if time.Since(start) > ChildTimeout {
				done(answer{Error: "the calendar file takes too long to read"}, 3)
			}
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, MaxBytes+1))
	if err != nil {
		done(answer{Error: "the calendar file could not be read"}, 2)
	}
	p, err := Parse(raw)
	if err != nil {
		done(answer{Error: err.Error()}, 0)
	}
	done(answer{Parsed: &p}, 0)
}

// Child parses .ics files in child processes of exe (this program, which
// calls ChildRequested and ServeChild first thing).
func Child(exe string) Reader {
	return func(ctx context.Context, raw []byte) (Parsed, error) {
		if len(raw) > MaxBytes {
			return Parsed{}, ErrTooLarge
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return Parsed{}, ctx.Err()
		}
		defer func() { <-slots }()
		run, cancel := context.WithTimeout(ctx, childWait)
		defer cancel()
		cmd := exec.CommandContext(run, exe)
		cmd.Env = []string{childEnv + "=1"}
		cmd.Dir = os.TempDir()
		cmd.Stdin = bytes.NewReader(raw)
		var out capped
		out.max = answerMax
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		var a answer
		if jerr := json.Unmarshal(out.b.Bytes(), &a); jerr != nil || a.Parsed == nil && a.Error == "" {
			if ctx.Err() != nil {
				return Parsed{}, ctx.Err()
			}
			if run.Err() != nil {
				return Parsed{}, errors.New("the calendar file takes too long to read")
			}
			if err != nil {
				return Parsed{}, errors.New("the calendar file could not be read safely")
			}
			return Parsed{}, errors.New("the calendar file could not be read")
		}
		if a.Error != "" {
			return Parsed{}, errors.New(a.Error)
		}
		return *a.Parsed, nil
	}
}

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
