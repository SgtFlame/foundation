// Package textnodes contributes the text nodes (forge 18b): Text diff, a
// Myers diff by lines or words whose hunks carry their markdown heading
// path, and Anchor resolve, which finds a short quote again in a text that
// may have changed; and (forge 18d) Three-way merge. All are pure and
// allowed in every graph kind. Parse transcript (forge 18e, transcript.go,
// ContributeTranscripts) reads a meeting transcript through a child
// process and is allowed only in kinds that may do I/O.
package textnodes

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/text"
)

const source = "text"

// Node type ids.
const (
	DiffID   = "text.diff"
	AnchorID = "text.anchor"
	MergeID  = "text.merge3"
	PartsID  = "text.sections"
)

// Contribute registers the nodes.
func Contribute(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: DiffID, Source: source, Order: 470}, Title: "Text diff", Category: "Text",
		Description: "The changes between two texts as hunks, by lines (the default) or by words, using a Myers diff: each hunk {op: insert | delete | replace | move, a: {start, end}, b: {start, end}, aLines, bLines: {start, end}, before, after, text, headingPath}, where a and b are character offsets (end exclusive) in the old and new text, lines are 1-based, and headingPath is the markdown headings the change falls under (the old text's for a delete). A line diff gives a block taken out in one place and put back unchanged in another as one move. Sections are the parts of both texts under the headings the changes touch (each heading to the next), for a model shown only what changed in a long text.",
		Example:     "Text diff of a board version's parent text and its text, by lines, for the semantic diff's prompt",
		Tags:        []string{"diff", "compare", "markdown", "changes", "text"},
		Config:      json.RawMessage(`{"type":"object","properties":{"granularity":{"type":"string","title":"Granularity","enum":["line","word"],"description":"Lines or words; the Granularity pin wins"}}}`),
		Sample:      cognition.Values{"hunks": []any{}, "sections": []any{}, "changed": false},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "a", Name: "Old", Type: "string", Optional: true, Description: "The old text; empty when unconnected"},
				{ID: "b", Name: "New", Type: "string", Optional: true, Description: "The new text; empty when unconnected"},
				{ID: "granularity", Name: "Granularity", Type: "string", Optional: true, Description: "line or word, in place of its config's"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("hunks", "Hunks", "json", "The changes in order: [{op, a, b, aLines, bLines, before, after, text, headingPath}]"),
				cognition.DataPin("sections", "Sections", "json", "The parts under the headings the changes touch, once each: [{headingPath, a, b}], a and b the part's text in each, from its heading to the next (empty where it is missing)"),
				cognition.DataPin("changed", "Changed", "bool", "Whether the texts differ"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Granularity string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			a, _ := cognition.Get[string](in, "a")
			b, _ := cognition.Get[string](in, "b")
			g, _ := cognition.Get[string](in, "granularity")
			if g == "" {
				g = c.Granularity
			}
			hunks := text.Diff(a, b, g)
			out, err := plain(hunks)
			if err != nil {
				return nil, "", err
			}
			sections, err := plain(text.ChangedSections(a, b, hunks))
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"hunks": out, "sections": sections, "changed": len(hunks) > 0}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: AnchorID, Source: source, Order: 471}, Title: "Anchor resolve", Category: "Text",
		Description: "Finds a short quote in a text that may have changed since it was taken: exactly under its markdown heading path, then exactly anywhere, then with spaces, case and typographic quotes ignored, then by its words with a few changed. Where it occurs more than once, the context (words around it when it was taken) picks one. Gives where it is as character offsets (end exclusive), the headings it now falls under, and whether it was found exactly. For checking a model's quotes, keeping highlights and suggestions on their text, and merges. Quotes (forge 18c) finds many at once in the same text: [{id, quote, headingPath, context}] gives Anchors, [{id, found, start, end, headingPath, exact, score}] in the same order, for a query placing every thread of a board.",
		Example:     "Anchor resolve each change's before quote in the parent text, dropping the quotes it does not find",
		Tags:        []string{"anchor", "quote", "find", "markdown", "highlight", "text"},
		Sample:      cognition.Values{"found": false, "start": 0, "end": 0, "headingPath": []any{}, "exact": false, "score": 0, "anchors": []any{}},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "text", Name: "Text", Type: "string", Optional: true, Description: "The text to look in"},
				{ID: "quote", Name: "Quote", Type: "string", Optional: true, Description: "The words to find; never found when empty"},
				{ID: "headingPath", Name: "Heading path", Type: "json", Optional: true, Description: "The headings it was under, outermost first, e.g. [\"Booking\", \"Payments\"]; looked under first"},
				{ID: "context", Name: "Context", Type: "string", Optional: true, Description: "Words that were around it, to pick one of several occurrences"},
				{ID: "quotes", Name: "Quotes", Type: "json", Optional: true, Description: "Many quotes to find in the same text: [{id, quote, headingPath, context}] (forge 18c)"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("found", "Found", "bool", "Whether it was found"),
				cognition.DataPin("start", "Start", "number", "Where it starts, a character offset"),
				cognition.DataPin("end", "End", "number", "Where it ends, exclusive"),
				cognition.DataPin("headingPath", "Heading path", "json", "The headings it now falls under"),
				cognition.DataPin("exact", "Exact", "bool", "Whether it was found as it is, rather than with spaces, case or a few words different"),
				cognition.DataPin("score", "Score", "number", "How alike the place found is to the quote, 1 when exact"),
				cognition.DataPin("anchors", "Anchors", "json", "Each of Quotes found, in order: [{id, found, start, end, headingPath, exact, score}]"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			s, _ := cognition.Get[string](in, "text")
			q, _ := cognition.Get[string](in, "quote")
			ctx, _ := cognition.Get[string](in, "context")
			var path []string
			if raw, err := json.Marshal(in["headingPath"]); err == nil {
				_ = json.Unmarshal(raw, &path)
			}
			a := text.Resolve(s, q, path, ctx)
			var many []struct {
				ID          string   `json:"id"`
				Quote       string   `json:"quote"`
				HeadingPath []string `json:"headingPath"`
				Context     string   `json:"context"`
			}
			if raw, err := json.Marshal(in["quotes"]); err == nil {
				_ = json.Unmarshal(raw, &many)
			}
			anchors := make([]any, len(many))
			for i, m := range many {
				r := text.Resolve(s, m.Quote, m.HeadingPath, m.Context)
				anchors[i] = map[string]any{"id": m.ID, "found": r.Found, "start": r.Start, "end": r.End, "headingPath": strings(r.HeadingPath), "exact": r.Exact, "score": r.Score}
			}
			return cognition.Values{"found": a.Found, "start": a.Start, "end": a.End, "headingPath": strings(a.HeadingPath), "exact": a.Exact, "score": a.Score, "anchors": anchors}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: PartsID, Source: source, Order: 473}, Title: "Split sections", Category: "Text",
		Description: "Splits a markdown text into its sections for search (forge 18f): each heading's own part, from its heading line to the next heading of any level, with its heading path (outermost first), its own heading and its 1-based lines; the text before the first heading is a section with an empty path. A section longer than Max characters is cut at blank lines into pieces with the same path. Sections holding only spaces are left out.",
		Example:     "Split sections of a board version's text, for the sections a Blackboards search finds",
		Tags:        []string{"sections", "split", "chunk", "headings", "markdown", "search", "text"},
		Config:      json.RawMessage(`{"type":"object","properties":{"maxChars":{"type":"integer","title":"Max characters","min":0,"description":"Cut longer sections at blank lines; 0 never cuts. The Max pin wins"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "text", Name: "Text", Type: "string", Optional: true, Description: "The markdown text"},
				{ID: "maxChars", Name: "Max", Type: "number", Optional: true, Description: "Max characters, in place of its config's"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("sections", "Sections", "json", "The sections in order: [{headingPath, heading, text, startLine, endLine}]"),
				cognition.DataPin("count", "Count", "number", "How many"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ MaxChars int }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			s, _ := cognition.Get[string](in, "text")
			if m, err := cognition.Get[float64](in, "maxChars"); err == nil && m > 0 {
				c.MaxChars = int(m)
			}
			out, err := plain(text.Parts(s, c.MaxChars))
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"sections": out, "count": len(out)}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: MergeID, Source: source, Order: 472}, Title: "Three-way merge", Category: "Text",
		Description: "Merges two texts changed from one base, by lines: a region only one side changed takes that side's text, a region both changed alike takes it once, and a region both changed differently is a conflict. Conflicts are [{index, base, source, target, headingPath, baseLines, merged: {start, end}, text, resolved}], where merged is where the region's text sits in the merged text (character offsets, end exclusive) and text what was put there: the resolution given for its index, else the target's text (or the source's, with prefer source). Run it once to find the conflicts, then again with resolutions [{index, text}] to write the merged text. Clean is true when nothing is left in conflict.",
		Example:     "Three-way merge of a board's text at a branch's base, on the branch and on main, for the semantic merge",
		Tags:        []string{"merge", "three-way", "conflict", "branch", "markdown", "text"},
		Config:      json.RawMessage(`{"type":"object","properties":{"prefer":{"type":"string","title":"In a conflict","enum":["target","source"],"description":"Whose text fills a conflict without a resolution; the target's by default"}}}`),
		Sample:      cognition.Values{"merged": "", "conflicts": []any{}, "clean": true, "sourceChanged": false, "targetChanged": false},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "base", Name: "Base", Type: "string", Optional: true, Description: "The text both sides started from"},
				{ID: "source", Name: "Source", Type: "string", Optional: true, Description: "One side's text (the branch merged in)"},
				{ID: "target", Name: "Target", Type: "string", Optional: true, Description: "The other side's text (the branch merged into)"},
				{ID: "resolutions", Name: "Resolutions", Type: "json", Optional: true, Description: "Text for conflicts by index: [{index, text}]"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("merged", "Merged", "string", "The merged text"),
				cognition.DataPin("conflicts", "Conflicts", "json", "The regions both sides changed differently: [{index, base, source, target, headingPath, baseLines, merged, text, resolved}]"),
				cognition.DataPin("clean", "Clean", "bool", "Whether no conflict is left without a resolution"),
				cognition.DataPin("sourceChanged", "Source changed", "bool", "Whether the source differs from the base"),
				cognition.DataPin("targetChanged", "Target changed", "bool", "Whether the target differs from the base"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Prefer string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			base, _ := cognition.Get[string](in, "base")
			src, _ := cognition.Get[string](in, "source")
			tgt, _ := cognition.Get[string](in, "target")
			var given []struct {
				Index int    `json:"index"`
				Text  string `json:"text"`
			}
			if raw, err := json.Marshal(in["resolutions"]); err == nil {
				_ = json.Unmarshal(raw, &given)
			}
			res := map[int]string{}
			for _, g := range given {
				res[g.Index] = g.Text
			}
			r := text.Merge3(base, src, tgt, res, c.Prefer)
			conflicts, err := plain(r.Conflicts)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"merged": r.Merged, "conflicts": conflicts, "clean": r.Clean, "sourceChanged": r.SourceChanged, "targetChanged": r.TargetChanged}, "", nil
		},
	})
}

// strings is a heading path as plain values.
func strings(path []string) []any {
	out := make([]any, len(path))
	for i, h := range path {
		out[i] = h
	}
	return out
}

// plain turns a Go value into the plain JSON values graphs pass.
func plain(v any) ([]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := []any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}
