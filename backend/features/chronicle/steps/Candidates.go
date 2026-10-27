package chroniclesteps

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

// Why a candidate is one.
const (
	WhyChecked   = "checked"   // the user checked it for the conversation
	WhyParent    = "parent"    // a parent of a checked entry
	WhyActive    = "active"    // chosen for the previous round
	WhyRetrieved = "retrieved" // retrieval ranked it for the message
	WhyLinked    = "linked"    // a parent, child or related entry of a checked or active one
)

// CandidatesIn is the round as the Candidates step sees it.
type CandidatesIn struct {
	features.ServiceArgs
	Text    string               // the user's message
	Window  []conversation.Round // the rounds sent as history, oldest first
	Checked []uint64             // the entries the user checked for the conversation
	// Retrieve is how many entries retrieval adds at most; 0 adds none.
	Retrieve        int
	LexicalSemantic int // retrieval's Axis 1, −5…5
}

// Candidate is an entry stage 1 may choose, and why it is one.
type Candidate struct {
	Entry     chronicle.Entry
	Why       string
	Active    bool              // chosen for the previous round, whatever else made it a candidate
	Retrieved *retrieval.Scored // how retrieval scored it, when it did
}

// Candidates are the entries a round may use: the checked ones, always
// kept, and the others stage 1 chooses from.
type Candidates struct {
	Checked []Candidate // checked entries, then their parents
	Others  []Candidate // retrieved best first, then active, then linked
}

// Candidates gathers the round's candidate entries (Chronicle.md › Send
// pipeline, step 2): the checked entries with their parents (the
// parent-inclusion rule), the entries chosen for the previous round of the
// window, the entries linked to either, and the entries retrieval's
// chronicle source ranks highest for the message. Proposed entries are
// never candidates.
func (s Steps) Candidates(in CandidatesIn) (Candidates, error) {
	var out Candidates
	byID, err := s.visible(in.ServiceArgs, chronicle.StatusActive)
	if err != nil || len(byID) == 0 {
		return out, err
	}
	seen := map[uint64]bool{}
	add := func(list *[]Candidate, id uint64, why string, scored *retrieval.Scored) {
		if e, ok := byID[id]; ok && !seen[id] {
			seen[id] = true
			*list = append(*list, Candidate{Entry: e, Why: why, Retrieved: scored})
		}
	}

	checked := map[uint64]bool{}
	for _, id := range in.Checked {
		checked[id] = true
	}
	for _, id := range chronicle.WithAncestors(in.Checked, byID) {
		why := WhyParent
		if checked[id] {
			why = WhyChecked
		}
		add(&out.Checked, id, why, nil)
	}

	if s.Retriever != nil && in.Retrieve > 0 {
		if service := s.Retriever(); service != nil {
			resp := service.Retrieve(retrievalservice.RetrieveArgs{
				ServiceArgs: in.ServiceArgs, Query: in.Text, TopK: min(in.Retrieve, 50),
				LexicalSemantic: max(-5, min(in.LexicalSemantic, 5)), Sources: []string{SourceID},
			})
			if resp.Common.Code == http.StatusOK {
				for _, seg := range resp.Segments {
					id, _ := strconv.ParseUint(seg.Key, 10, 64)
					add(&out.Others, id, WhyRetrieved, &seg)
				}
			} else {
				log.Warn(in.Context(), "retrieval failed with %d, so stage 1 sees no retrieved entries: %s", resp.Common.Code, resp.Common.Message)
			}
		}
	}

	active, err := s.active(in)
	if err != nil {
		return out, err
	}
	for _, id := range active {
		add(&out.Others, id, WhyActive, nil)
	}
	isActive := map[uint64]bool{}
	for _, id := range active {
		isActive[id] = true
	}
	for i := range out.Others {
		out.Others[i].Active = isActive[out.Others[i].Entry.ID]
	}

	// One hop from what is checked or active: parents, children and related links
	hubs := slices.Clone(in.Checked)
	for _, id := range active {
		if _, ok := byID[id]; ok {
			hubs = append(hubs, id)
		}
	}
	if len(hubs) > 0 {
		var links []chronicle.EntryLink
		if err := chronicle.Owned(s.DB.WithContext(in.Context()), in.Principal).Where("from_id IN ? OR to_id IN ?", hubs, hubs).Find(&links).Error; err != nil {
			return out, err
		}
		hub := map[uint64]bool{}
		for _, id := range hubs {
			hub[id] = true
		}
		var linked []uint64
		for _, l := range links {
			linked = append(linked, l.FromID, l.ToID)
		}
		for _, e := range byID {
			if hub[e.ParentID] || (hub[e.ID] && e.ParentID != 0) {
				linked = append(linked, e.ID, e.ParentID)
			}
		}
		slices.SortFunc(linked, func(a, b uint64) int { return cmp.Compare(a, b) }) // a stable order for the prompt
		for _, id := range linked {
			add(&out.Others, id, WhyLinked, nil)
		}
	}
	return out, nil
}

// active is the entries chosen for the last round of the window: its
// pinned chronicle segments.
func (s Steps) active(in CandidatesIn) ([]uint64, error) {
	if len(in.Window) == 0 {
		return nil, nil
	}
	last := in.Window[len(in.Window)-1]
	var segments []retrieval.RoundSegment
	err := s.DB.WithContext(in.Context()).Scopes(db.InTenant(in.Principal.TenantID)).
		Where("round_id = ? AND source_id = ? AND pinned", last.ID, SourceID).Order("number").Find(&segments).Error
	ids := make([]uint64, 0, len(segments))
	for _, seg := range segments {
		if id, err := strconv.ParseUint(seg.SegmentKey, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, err
}
