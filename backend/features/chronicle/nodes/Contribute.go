// Package chroniclenodes makes Chronicle's pipeline Cognition nodes
// (Index.md › seam 3, Cognition.md): each step function of chronicle/steps
// becomes a node type, its model calls' prompts Go templates in the node's
// config (forge 06i), and the built-in Personal chat mode is a mode graph
// running a whole round with them: Chronicle memory, the workbench and
// Volumes as context, the tools, the streamed reply, the title and the
// memory update. It is an editable built-in, offered as the default mode.
package chroniclenodes

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// PersonalID is the built-in Personal graph's ID.
const PersonalID = "personal"

// Deps are what the nodes run with, once their services exist.
type Deps struct {
	Steps   chroniclesteps.Steps
	Entries chronicleservice.Service // indexes the entries the memory update changed
}

// The data types Chronicle contributes.
const (
	TypeEntryRefs  = "EntryRef[]"
	TypeCandidates = "Candidates"
	TypeSelection  = "Selection"
	TypeChanges    = "Changes"
)

//go:embed personal.json
var personal []byte

const source = "chronicle"

var log = logging.In("chronicle")

// Contribute registers the node types, the data types, the Personal graph
// and its mode. deps returns the step functions and the entries service
// once they exist.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	steps := func() chroniclesteps.Steps { return deps().Steps }
	for i, t := range []cognition.DataType{
		{Descriptor: extensions.Descriptor{ID: TypeEntryRefs}, Color: "#8d6e63", Description: "Chronicle entries, by ID"},
		{Descriptor: extensions.Descriptor{ID: TypeCandidates}, Color: "#ab47bc", Description: "The entries a round may use: the checked ones and the others stage 1 chooses from"},
		{Descriptor: extensions.Descriptor{ID: TypeSelection}, Color: "#6a1b9a", Description: "The entries stage 1 chose, ranked"},
		{Descriptor: extensions.Descriptor{ID: TypeChanges}, Color: "#5d4037", Description: "The entries the memory update changed and proposed"},
	} {
		t.Order, t.Source = 200+i, source
		cognition.Types.Register(reg, t)
	}

	node := func(order int, t cognition.NodeType) {
		// They need a chat round's context: only chat modes and summarizers run them
		t.Order, t.Source, t.Category, t.Kinds = order, source, "Chronicle", []string{cognition.KindMode, cognition.KindSummarizer}
		cognition.Nodes.Register(reg, t)
	}
	node(300, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.settings"},
		Example:     "Chronicle settings from On user message's Settings, feeding Candidate entries' Checked and Retrieve",
		Title:       "Chronicle settings",
		Description: "Reads the conversation's Chronicles settings, with their defaults",
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.DataPin("settings", "Settings", "json", "The chronicle context settings")},
			Out: []cognition.Pin{
				cognition.DataPin("checked", "Checked", TypeEntryRefs, "The entries checked in the Chronicles view"),
				cognition.DataPin("topK", "Entries", "number", "How many entries stage 1 may choose, besides the checked ones"),
				cognition.DataPin("retrieve", "Retrieve", "number", "How many entries retrieval adds to the candidates"),
				cognition.DataPin("match", "Match", "bool", "Whether stage 1 runs"),
				cognition.DataPin("update", "Update", "bool", "Whether the memory update runs: Chronicles and Update memory are both on"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			raw, err := cognition.Get[json.RawMessage](in, "settings")
			if err != nil {
				return nil, "", err
			}
			s := SettingsOf(x.ServiceArgs, raw)
			var checked []uint64
			for _, id := range s.Checked {
				if n, err := strconv.ParseUint(id, 10, 64); err == nil {
					checked = append(checked, n)
				}
			}
			return cognition.Values{
				"checked": checked, "topK": s.TopK, "retrieve": max(2*s.TopK, 10),
				"match": s.On(s.Match), "update": s.On(s.Enabled) && s.On(s.Update),
			}, "", nil
		},
	})
	node(310, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.candidates"},
		Example:     "Candidate entries from the message, window and checked entries, before Match context",
		Title:       "Candidate entries",
		Description: "Gathers the entries a round may use: the checked ones and their parents, the ones retrieval ranks highest, the ones used last round, and their links",
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("text", "Text", "string", "The user's message"),
				cognition.DataPin("window", "Window", conversationnodes.TypeRounds, "The rounds sent as history"),
				cognition.DataPin("checked", "Checked", TypeEntryRefs, "The entries checked in the Chronicles view, always kept"),
				cognition.DataPin("retrieve", "Retrieve", "number", "How many entries retrieval adds at most; 0 adds none"),
			},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("candidates", "Candidates", TypeCandidates, "The checked entries and the others stage 1 may choose from")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			args := chroniclesteps.CandidatesIn{ServiceArgs: x.ServiceArgs}
			if err := errors.Join(into(in, "text", &args.Text), into(in, "window", &args.Window), into(in, "checked", &args.Checked), into(in, "retrieve", &args.Retrieve)); err != nil {
				return nil, "", err
			}
			c, err := steps().Candidates(args)
			if err != nil {
				return nil, "", log.Wrap(err)
			}
			return cognition.Values{"candidates": c}, cognition.Then, nil
		},
	})
	node(320, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.match"},
		Example:     "Match context choosing up to Entries candidates for the reply",
		Title:       "Match context",
		Effects:     true,
		External:    true, // a model call
		Description: "Stage 1: a fast model call chooses the candidates the reply needs; the checked entries are always kept. Its prompts are Go templates over topK, text, window and candidates.",
		Config:      json.RawMessage(promptsSchema("topK, text (the message), window (each text, reply) and candidates (each id, type, title, summary, active)")),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("text", "Text", "string", "The user's message"),
				cognition.DataPin("window", "Window", conversationnodes.TypeRounds, "The rounds sent as history"),
				cognition.DataPin("candidates", "Candidates", TypeCandidates, "The entries to choose from"),
				cognition.DataPin("topK", "Entries", "number", "Entries chosen at most, besides the checked ones"),
				cognition.DataPin("match", "Match", "bool", "False keeps the candidates' order without asking the model"),
			},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("selection", "Selection", TypeSelection, "The entries chosen, ranked")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			args := chroniclesteps.MatchIn{ServiceArgs: x.ServiceArgs, Env: x.Env()}
			if err := x.Config(&args.Prompts); err != nil {
				return nil, "", err
			}
			match := true
			if err := errors.Join(into(in, "text", &args.Text), into(in, "window", &args.Window), into(in, "candidates", &args.Candidates), into(in, "topK", &args.TopK), into(in, "match", &match)); err != nil {
				return nil, "", err
			}
			args.Skip = !match
			return cognition.Values{"selection": steps().Match(args)}, cognition.Then, nil
		},
	})
	node(330, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.contextItems"},
		Example:     "Entries as context from Match context's Selection, returned as the mode's items",
		Title:       "Entries as context",
		Description: "Turns the chosen entries into pinned, citable context items, each with why it is in the round",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("selection", "Selection", TypeSelection, "The entries stage 1 chose")},
			Out: []cognition.Pin{cognition.DataPin("items", "Items", conversationnodes.TypeContextItems, "The entries as pinned context items")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			sel, err := cognition.Get[chroniclesteps.Selection](in, "selection")
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"items": ContextItems(sel)}, "", nil
		},
	})
	node(340, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.usedEntries"},
		Example:     "Used entries from On reply's Segments, feeding Update memory",
		Title:       "Used entries",
		Description: "The Chronicle entries among the segments a round injected",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("segments", "Segments", conversationnodes.TypeSegments, "What the round injected")},
			Out: []cognition.Pin{cognition.DataPin("used", "Used", TypeEntryRefs, "The Chronicle entries among them")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			segments, err := cognition.Get[[]retrieval.RoundSegment](in, "segments")
			if err != nil {
				return nil, "", err
			}
			var used []uint64
			for _, seg := range segments {
				if seg.SourceID == chroniclesteps.SourceID {
					if id, err := strconv.ParseUint(seg.SegmentKey, 10, 64); err == nil {
						used = append(used, id)
					}
				}
			}
			return cognition.Values{"used": used}, "", nil
		},
	})
	node(350, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.update"},
		Example:     "Update memory after On reply when Chronicle settings' Update is on",
		Title:       "Update memory",
		Effects:     true,
		External:    true, // a model call
		Description: "The memory update: rewrites the summaries of the used entries the round adds facts to, and proposes new entries. Its prompts are Go templates over maxProposals, round, used, types and titles.",
		Config:      json.RawMessage(promptsSchema("maxProposals, round (text, reply), used (each id, type, title, shortSummary, fullSummary), types (each id, description) and titles", `"maxProposals": {"type": "integer", "title": "Proposals", "min": 0, "max": 10, "description": "New entries proposed at most"}`)),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("round", "Round", conversationnodes.TypeRound, "The round to learn from"),
				cognition.DataPin("used", "Used", TypeEntryRefs, "The entries the round injected: the only ones it may update"),
			},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("changes", "Changes", TypeChanges, "The entries changed and proposed")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			config := struct {
				MaxProposals *int `json:"maxProposals"`
				chroniclesteps.Prompts
			}{}
			if len(x.Node.Config) > 0 {
				if err := json.Unmarshal(x.Node.Config, &config); err != nil {
					return nil, "", log.Code("bad_node_config").Wrapf(err, "the node's config is not valid")
				}
			}
			args := chroniclesteps.UpdateIn{ServiceArgs: x.ServiceArgs, MaxProposals: 3, Prompts: config.Prompts, Env: x.Env()}
			if config.MaxProposals != nil {
				args.MaxProposals = *config.MaxProposals
			}
			if err := errors.Join(into(in, "round", &args.Round), into(in, "used", &args.Used)); err != nil {
				return nil, "", err
			}
			changes, err := steps().Update(args)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"changes": changes}, cognition.Then, nil
		},
	})

	node(360, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.changes"},
		Example:     "Memory changes after Update memory, whose titles a Template turns into Emit followed's summary",
		Title:       "Memory changes",
		Effects:     true,
		Description: "Indexes the entries the memory update changed, so retrieval finds their new summaries, and gives what changed: the titles updated and proposed, and the data the browser's Chronicles control shows",
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("changes", "Changes", TypeChanges, "What the memory update did")},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("changed", "Changed", "bool", "Whether it updated or proposed any entry"),
				cognition.DataPin("updated", "Updated", "json", "The titles of the entries updated, without markdown"),
				cognition.DataPin("proposed", "Proposed", "json", "The titles of the entries proposed, without markdown"),
				cognition.DataPin("data", "Data", "json", "The entries changed, for Emit followed chronicle"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			changes, err := cognition.Get[chroniclesteps.Changes](in, "changes")
			if err != nil {
				return nil, "", err
			}
			out, ids := Changed(changes)
			if d := deps(); len(ids) > 0 && d.Entries != nil {
				if resp := d.Entries.Reindex(chronicleservice.ReindexArgs{ServiceArgs: x.ServiceArgs, EntryIDs: ids}); resp.Common.Code >= 300 {
					log.Warn(x.Context(), "cannot index the entries the memory update changed: %s", resp.Common.Message)
				}
			}
			updated, proposed := []string{}, []string{}
			for _, e := range out.Updated {
				updated = append(updated, plain(e.Title))
			}
			for _, e := range out.Proposed {
				proposed = append(proposed, plain(e.Title))
			}
			return cognition.Values{"changed": len(updated)+len(proposed) > 0, "updated": updated, "proposed": proposed, "data": out}, cognition.Then, nil
		},
	})

	var g cognition.Graph
	if err := json.Unmarshal(personal, &g); err != nil {
		panic("chroniclenodes: personal.json: " + err.Error()) // embedded, so a test catches it
	}
	cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: PersonalID, Order: 100, Source: source}, Graph: g, Editable: true})
	conversation.Modes.Register(reg, conversation.ModeEntry{Descriptor: extensions.Descriptor{ID: PersonalID, Order: 100, Source: source},
		Label: "Personal", Icon: "🧠", GraphRef: PersonalID, Default: true})
}

// promptsSchema is the config schema of a node whose model call has
// system and prompt templates over data, with more properties first.
func promptsSchema(data string, more ...string) string {
	props := append(more,
		`"system": {"type": "string", "title": "Instructions", "format": "multiline", "description": "A Go template over `+data+`; empty uses the default"}`,
		`"prompt": {"type": "string", "title": "Message", "format": "multiline", "description": "A Go template over `+data+`; empty uses the default"}`)
	return `{"type": "object", "properties": {` + strings.Join(props, ", ") + `}}`
}

// Followed is the data of a "followed" event of the chronicle follower,
// which the browser's Chronicles control reads.
type Followed struct {
	Updated  []EntryRef `json:"updated,omitempty"`
	Proposed []EntryRef `json:"proposed,omitempty"`
}

// EntryRef names an entry in Followed.
type EntryRef struct {
	ID    uint64 `json:"id,string"`
	Title string `json:"title"`
}

// Changed is what the memory update did as Followed, with the ids of the
// entries it updated, which need indexing again.
func Changed(changes chroniclesteps.Changes) (Followed, []uint64) {
	var out Followed
	var ids []uint64
	for _, e := range changes.Updated {
		out.Updated = append(out.Updated, EntryRef{ID: e.ID, Title: e.Title})
		ids = append(ids, e.ID)
	}
	for _, e := range changes.Proposed {
		out.Proposed = append(out.Proposed, EntryRef{ID: e.ID, Title: e.Title})
	}
	return out, ids
}

// plain is a title without the characters markdown would read.
func plain(title string) string {
	return strings.NewReplacer("*", "", "_", " ").Replace(title)
}

// into sets *p to input id, converted as cognition.Get converts it, and
// leaves it alone when the input is absent.
func into[T any](in cognition.Values, id string, p *T) error {
	if _, ok := in[id]; !ok {
		return nil
	}
	v, err := cognition.Get[T](in, id)
	if err == nil {
		*p = v
	}
	return err
}

// Settings are the conversation's Chronicle settings: the chronicle
// context provider's.
type Settings struct {
	Enabled *bool `json:"enabled"`
	TopK    int   `json:"topK"`   // entries chosen at most, besides the checked ones
	Match   *bool `json:"match"`  // stage 1; off keeps retrieval's order
	Update  *bool `json:"update"` // the memory update after each reply
	// Checked are the entries the user checked for the conversation in the
	// Chronicles view, as IDs. Not in the schema: it is the conversation's
	// alone, never a default.
	Checked []string `json:"checked,omitempty"`
}

// On reports whether an optional switch is on: unset is on.
func (s Settings) On(b *bool) bool {
	return b == nil || *b
}

// SettingsOf decodes the settings, with TopK defaulted and bounded.
func SettingsOf(args features.ServiceArgs, raw json.RawMessage) Settings {
	var s Settings
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			log.Warn(args.Context(), "the Chronicles settings are not valid, so their defaults apply: %v", err)
		}
	}
	if s.TopK <= 0 {
		s.TopK = 5
	}
	s.TopK = min(s.TopK, 20)
	return s
}

// entryText bounds an entry's text in the prompt.
const entryText = 8 << 10

// ID is the chronicle context provider's ID, which its items' Refs start with.
const ID = "chronicle"

// ContextItems are the chosen entries as pinned, citable items.
func ContextItems(sel chroniclesteps.Selection) []conversation.ContextItem {
	items := make([]conversation.ContextItem, 0, len(sel.Entries))
	for _, c := range sel.Entries {
		e := c.Entry
		id := strconv.FormatUint(e.ID, 10)
		seg := retrieval.Scored{
			Segment: retrieval.Segment{SourceID: chroniclesteps.SourceID, ID: id, Key: id, Title: e.Title, Text: clip(e.Text(), entryText),
				Location: retrieval.Location{EntryID: e.ID}},
			Keys: []string{id}, AxisMult: 1, Grounding: 1, Pinned: true, Reason: reason(c, sel.Matched),
		}
		if r := c.Retrieved; r != nil {
			seg.Score, seg.RRF, seg.AxisMult, seg.Grounding, seg.VectorRank, seg.KeywordRank = r.Score, r.RRF, r.AxisMult, r.Grounding, r.VectorRank, r.KeywordRank
		}
		seg.Tokens = (len(seg.Text) + 3) / 4
		items = append(items, conversation.ContextItem{Kind: "chronicle", Ref: ID + ":" + id, Title: e.Title, Text: seg.Text, Tokens: seg.Tokens, Score: seg.Score, Segment: &seg})
	}
	return items
}

// reason says why a chosen entry is in the round, for the Context inspector.
func reason(c chroniclesteps.Selected, matched bool) string {
	switch {
	case c.Why == chroniclesteps.WhyChecked || c.Why == chroniclesteps.WhyParent:
		return c.Why
	case matched:
		return "stage 1 · " + c.Why
	}
	return c.Why
}

// clip cuts s to about n bytes on a rune boundary, marking the cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n] + "…"
}
