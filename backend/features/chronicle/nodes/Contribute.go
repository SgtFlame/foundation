// Package chroniclenodes makes Chronicle's pipeline a Cognition graph
// (Index.md › seam 3, Cognition.md): each step function of chronicle/steps
// becomes a node type, and the built-in Personal graph wires them as
// chroniclecontext ran them in slice 6. The chronicle context provider and
// round follower now run Personal's two events instead of calling the
// steps themselves.
package chroniclenodes

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
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

// The outputs of Personal's Return nodes.
const (
	OutItems   = "items"   // []conversation.ContextItem: the chosen entries, pinned
	OutChanges = "changes" // chroniclesteps.Changes: what the memory update did
)

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

// Contribute registers the node types, the data types and the Personal
// graph. steps returns the step functions once their services exist.
func Contribute(reg *extensions.Registry, steps func() chroniclesteps.Steps) {
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
		t.Order, t.Source, t.Category = order, source, "Chronicle"
		cognition.Nodes.Register(reg, t)
	}
	node(300, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "chronicle.settings"},
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
		Title:       "Candidate entries",
		Description: "Gathers the entries a round may use: the checked ones and their parents, the ones retrieval ranks highest, the ones used last round, and their links",
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("text", "Text", "string", "The user's message"),
				cognition.DataPin("window", "Window", conversationnodes.TypeRounds, "The rounds sent as history"),
				cognition.DataPin("checked", "Checked", TypeEntryRefs, ""),
				cognition.DataPin("retrieve", "Retrieve", "number", "How many entries retrieval adds at most; 0 adds none"),
			},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("candidates", "Candidates", TypeCandidates, "")},
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
		Title:       "Match context",
		Description: "Stage 1: a fast model call chooses the candidates the reply needs; the checked entries are always kept",
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("text", "Text", "string", "The user's message"),
				cognition.DataPin("window", "Window", conversationnodes.TypeRounds, "The rounds sent as history"),
				cognition.DataPin("candidates", "Candidates", TypeCandidates, ""),
				cognition.DataPin("topK", "Entries", "number", "Entries chosen at most, besides the checked ones"),
				cognition.DataPin("match", "Match", "bool", "False keeps the candidates' order without asking the model"),
			},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("selection", "Selection", TypeSelection, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			args := chroniclesteps.MatchIn{ServiceArgs: x.ServiceArgs}
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
		Title:       "Entries as context",
		Description: "Turns the chosen entries into pinned, citable context items, each with why it is in the round",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("selection", "Selection", TypeSelection, "")},
			Out: []cognition.Pin{cognition.DataPin("items", "Items", conversationnodes.TypeContextItems, "")},
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
		Title:       "Used entries",
		Description: "The Chronicle entries among the segments a round injected",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("segments", "Segments", conversationnodes.TypeSegments, "")},
			Out: []cognition.Pin{cognition.DataPin("used", "Used", TypeEntryRefs, "")},
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
		Title:       "Update memory",
		Description: "The memory update: rewrites the summaries of the used entries the round adds facts to, and proposes new entries",
		Config:      json.RawMessage(`{"type": "object", "properties": {"maxProposals": {"type": "integer", "title": "Proposals", "min": 0, "max": 10, "description": "New entries proposed at most"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("round", "Round", conversationnodes.TypeRound, "The round to learn from"),
				cognition.DataPin("used", "Used", TypeEntryRefs, "The entries the round injected: the only ones it may update"),
			},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("changes", "Changes", TypeChanges, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			config := struct {
				MaxProposals *int `json:"maxProposals"`
			}{}
			if len(x.Node.Config) > 0 {
				if err := json.Unmarshal(x.Node.Config, &config); err != nil {
					return nil, "", log.Code("bad_node_config").Wrapf(err, "the node's config is not valid")
				}
			}
			args := chroniclesteps.UpdateIn{ServiceArgs: x.ServiceArgs, MaxProposals: 3}
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

	var g cognition.Graph
	if err := json.Unmarshal(personal, &g); err != nil {
		panic("chroniclenodes: personal.json: " + err.Error()) // embedded, so a test catches it
	}
	cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: PersonalID, Order: 100, Source: source}, Graph: g})
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
