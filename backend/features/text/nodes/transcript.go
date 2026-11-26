package textnodes

import (
	"context"
	"fmt"
	"io"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/text"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// TranscriptID is Parse transcript's node type id (forge 18e).
const TranscriptID = "text.transcript"

// TranscriptDeps are what Parse transcript needs, made after the registry.
type TranscriptDeps struct {
	// Read parses a transcript: text.ChildTranscripts in a server, never
	// text.ParseTranscript in the server's own process. Without it nothing
	// is parsed.
	Read text.TranscriptReader
	// Open opens a tenant's uploaded file (records/files Store.Open).
	// Without it files are not read.
	Open func(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error)
}

// ContributeTranscripts registers Parse transcript; deps gives its
// dependencies once made.
func ContributeTranscripts(reg *extensions.Registry, deps func() TranscriptDeps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: TranscriptID, Source: source, Order: 473}, Title: "Parse transcript", Category: "Text",
		Description: "Reads a meeting transcript into numbered lines with their speakers: an uploaded file (a file field's value, read from the run's tenant) or pasted text. Formats: plain text (\"Speaker: text\", \"[00:01:02] Speaker: text\", or a header \"Speaker Name  0:03\" over the turn's lines), WebVTT (Teams <v> voice tags, Zoom \"Name: \" prefixes), SRT, .docx (only word/document.xml's paragraphs) and JSON from meeting tools (a list under utterances, sentences, segments, transcript, entries, results, lines, messages or monologues). Consecutive cues or entries of one speaker are merged into one line of at most 1000 characters. The file is parsed in a limited child process, never in the server, with at most 10 MB read, 5000 lines of 4000 characters and 2 000 000 characters in all (Truncated says when something was cut). It never fails the run: OK is false and Error says why in plain words (no file or text, the file is missing or too large, not a transcript, malformed), for a job to record.",
		Example:     "Parse transcript of the meeting record's transcript file, then Ask model to pull the requirements from its lines, citing each by its number",
		Tags:        []string{"transcript", "meeting", "vtt", "srt", "docx", "import", "speakers", "text"},
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent},
		Sample: cognition.Values{"lines": []any{map[string]any{"n": 1, "speaker": "Anna", "at": "00:00:03", "text": "Good morning."}},
			"speakers": []any{"Anna"}, "format": "text", "truncated": false, "ok": true, "error": ""},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				{ID: "file", Name: "File", Type: "json", Optional: true, Description: "An uploaded file as a file field holds it, {id, name, size, contentType}, or its id; read in place of Text"},
				{ID: "text", Name: "Text", Type: "string", Optional: true, Description: "Pasted transcript text, read when no file is given"},
				{ID: "name", Name: "Name", Type: "string", Optional: true, Description: "A file name hinting the format (meeting.vtt), when the file has none or for pasted text; sniffed from the content otherwise"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("lines", "Lines", "json", "The lines in order: [{n, speaker, at, text}], n from 1, at hh:mm:ss when known, speaker empty when unknown"),
				cognition.DataPin("speakers", "Speakers", "json", "The distinct speakers in order of first appearance"),
				cognition.DataPin("format", "Format", "string", "text, vtt, srt, docx or json"),
				cognition.DataPin("truncated", "Truncated", "bool", "Whether lines or text were cut to the limits"),
				cognition.DataPin("ok", "OK", "bool", "Whether the transcript was read"),
				cognition.DataPin("error", "Error", "string", "Why it was not read, in plain words; empty when OK"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			fail := func(msg string) (cognition.Values, string, error) {
				return cognition.Values{"lines": []any{}, "speakers": []any{}, "format": "", "truncated": false, "ok": false, "error": msg}, cognition.Then, nil
			}
			var d TranscriptDeps
			if deps != nil {
				d = deps()
			}
			name, _ := cognition.Get[string](in, "name")
			pasted, _ := cognition.Get[string](in, "text")
			var fileID, contentType string
			switch f := in["file"].(type) {
			case string:
				fileID = f
			case map[string]any:
				fileID, _ = f["id"].(string)
				if n, _ := f["name"].(string); n != "" {
					name = n
				}
				contentType, _ = f["contentType"].(string)
			}
			var raw []byte
			switch {
			case fileID != "":
				if d.Open == nil {
					return fail("files cannot be read here")
				}
				rc, ref, found, err := d.Open(x.Context(), x.Principal.TenantID, fileID)
				if err != nil {
					return fail("the file could not be opened")
				}
				if !found {
					return fail("the file is missing: it may have been deleted")
				}
				raw, err = io.ReadAll(io.LimitReader(rc, text.MaxTranscriptBytes+1))
				rc.Close()
				if err != nil {
					return fail("the file could not be read")
				}
				if len(raw) > text.MaxTranscriptBytes {
					return fail(fmt.Sprintf("the file is larger than %d MB", text.MaxTranscriptBytes>>20))
				}
				if ref.Name != "" {
					name = ref.Name
				}
				if ref.ContentType != "" {
					contentType = ref.ContentType
				}
			case pasted != "":
				raw = []byte(pasted)
			default:
				return fail("no file or text was given")
			}
			if d.Read == nil {
				return fail("transcripts cannot be read here")
			}
			t, err := d.Read(x.Context(), name, contentType, raw)
			if err != nil {
				return fail(err.Error())
			}
			lines, err := plain(t.Lines)
			if err != nil {
				return nil, "", err
			}
			speakers := make([]any, len(t.Speakers))
			for i, s := range t.Speakers {
				speakers[i] = s
			}
			return cognition.Values{"lines": lines, "speakers": speakers, "format": t.Format, "truncated": t.Truncated, "ok": true, "error": ""}, cognition.Then, nil
		},
	})
}
