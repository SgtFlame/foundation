package chroniclesteps

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

// fakeChat answers each feature with its canned reply, or fails it.
type fakeChat struct {
	replies map[string]string
	got     []chatservice.CompleteArgs
}

func (f *fakeChat) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	f.got = append(f.got, args)
	reply, ok := f.replies[args.Meta.Feature]
	if !ok {
		return chatservice.CompleteResponse{Common: features.ServiceResponse{Code: http.StatusServiceUnavailable, Message: "no key"}}
	}
	return chatservice.CompleteResponse{Common: features.OK(), Message: chatservice.Message{Role: chatservice.RoleAssistant, Content: reply}}
}

func (f *fakeChat) Stream(chatservice.StreamArgs) *chatservice.StreamRequest { panic("not used") }

type sections map[string]string

func (s sections) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	if v, ok := s[section]; ok {
		return json.RawMessage(v), nil
	}
	return nil, context.Canceled
}

// fakeRetriever ranks the entries it was given, in order.
type fakeRetriever struct {
	ids []uint64
	got []retrievalservice.RetrieveArgs
}

func (f *fakeRetriever) Retrieve(args retrievalservice.RetrieveArgs) retrievalservice.RetrieveResponse {
	f.got = append(f.got, args)
	resp := retrievalservice.RetrieveResponse{Common: features.OK()}
	for i, id := range f.ids {
		key := strconv.FormatUint(id, 10)
		resp.Segments = append(resp.Segments, retrieval.Scored{Segment: retrieval.Segment{SourceID: SourceID, ID: key, Key: key}, Score: 1 / float64(i+1), RRF: 0.1})
	}
	return resp
}

type fixture struct {
	steps     Steps
	chat      *fakeChat
	retriever *fakeRetriever
	entries   map[string]chronicle.Entry
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(append(chronicle.Models(), conversation.Models()...), retrieval.Models()...)...); err != nil {
		t.Fatal(err)
	}
	f := &fixture{chat: &fakeChat{replies: map[string]string{}}, retriever: &fakeRetriever{}, entries: map[string]chronicle.Entry{}}
	f.steps = Steps{DB: gdb, Chat: f.chat, Retriever: func() retrievalservice.Service { return f.retriever },
		Config: sections{
			aisettings.StagesID: `{"match": "fast", "memory": "nope"}`,
			aisettings.LevelsID: `{"levels": [{"id": "fast"}, {"id": "standard", "default": true}]}`,
		}}
	p := features.LocalPrincipal()
	add := func(name, typ string, parent string, status string) {
		e := chronicle.Entry{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), Type: typ, Title: name, ShortSummary: "About " + name + ".",
			FullSummary: name + " facts.", Fields: "{}", ParentID: f.entries[parent].ID, Status: status}
		if err := gdb.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		f.entries[name] = e
	}
	add("Family", "note", "", chronicle.StatusActive)
	add("Ada", "entity", "Family", chronicle.StatusActive)
	add("Byron", "entity", "Family", chronicle.StatusActive)
	add("Engine", "note", "", chronicle.StatusActive)
	add("Babbage", "entity", "", chronicle.StatusActive)
	add("Maybe", "note", "", chronicle.StatusProposed)
	gdb.Create(&chronicle.EntryLink{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), FromID: f.entries["Engine"].ID, ToID: f.entries["Babbage"].ID, Kind: chronicle.LinkRelated})
	return f
}

func (f *fixture) id(name string) uint64 { return f.entries[name].ID }

func (f *fixture) args() features.ServiceArgs {
	return features.ServiceArgs{Principal: features.LocalPrincipal(), Meta: features.Meta{ConversationID: 7, RoundID: 8}}
}

func titles(cs []Candidate) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Entry.Title+":"+c.Why)
	}
	return strings.Join(out, " ")
}

func TestCandidates(t *testing.T) {
	f := newFixture(t)
	// The previous round chose Engine
	p := features.LocalPrincipal()
	f.steps.DB.Create(&retrieval.RoundSegment{Tenanted: db.TenantOf(p), RoundID: 41, SourceID: SourceID, SegmentKey: strconv.FormatUint(f.id("Engine"), 10), Pinned: true})
	f.retriever.ids = []uint64{f.id("Byron"), f.id("Maybe"), f.id("Engine")}

	got, err := f.steps.Candidates(CandidatesIn{ServiceArgs: f.args(), Text: "who?", Window: []conversation.Round{{Model: db.Model{ID: 40}}, {Model: db.Model{ID: 41}}},
		Checked: []uint64{f.id("Ada")}, Retrieve: 10, LexicalSemantic: 9})
	if err != nil {
		t.Fatal(err)
	}
	if s := titles(got.Checked); s != "Ada:checked Family:parent" {
		t.Errorf("checked: %s", s)
	}
	// Retrieved first (never a proposed entry), then active, then linked:
	// Babbage is related to Engine, and Byron is the checked Ada's sibling, not her neighbour
	if s := titles(got.Others); s != "Byron:retrieved Engine:retrieved Babbage:linked" {
		t.Errorf("others: %s", s)
	}
	if r := f.retriever.got[0]; r.LexicalSemantic != 5 || len(r.Sources) != 1 || r.Sources[0] != SourceID || r.TopK != 10 {
		t.Errorf("retrieve args %+v", r)
	}
}

func TestMatch(t *testing.T) {
	f := newFixture(t)
	candidates := Candidates{
		Checked: []Candidate{{Entry: f.entries["Ada"], Why: WhyChecked}},
		Others:  []Candidate{{Entry: f.entries["Byron"], Why: WhyRetrieved}, {Entry: f.entries["Engine"], Why: WhyActive, Active: true}, {Entry: f.entries["Babbage"], Why: WhyLinked}},
	}
	in := MatchIn{ServiceArgs: f.args(), Text: "And Babbage?", Window: []conversation.Round{{Text: "Tell me about the engine", Reply: "It computes."}}, Candidates: candidates, TopK: 2}

	// Without a key, the candidates' order is kept
	sel := f.steps.Match(in)
	if sel.Matched || len(sel.Entries) != 3 || sel.Entries[0].Entry.Title != "Ada" || sel.Entries[2].Entry.Title != "Engine" || sel.Entries[2].Rank != 3 {
		t.Fatalf("fallback %+v", sel)
	}
	call := f.chat.got[0]
	if call.Level != "fast" || call.Meta.Feature != "chronicle-match" || call.Meta.RoundID != 8 || len(call.JSONSchema) == 0 ||
		!strings.Contains(call.Messages[0].Content, "Tell me about the engine") ||
		!strings.Contains(call.Messages[0].Content, "- id "+strconv.FormatUint(f.id("Engine"), 10)+" (note, used in the previous reply): Engine — About Engine.") {
		t.Errorf("stage 1 call %+v", call)
	}

	// Stage 1 chooses by rank; unknown and repeated IDs are dropped
	f.chat.replies["chronicle-match"] = "```json\n" + `{"shift": true, "entries": [{"id": "` + strconv.FormatUint(f.id("Engine"), 10) + `", "rank": 2},
		{"id": "` + strconv.FormatUint(f.id("Babbage"), 10) + `", "rank": 1}, {"id": "123", "rank": 3}, {"id": "` + strconv.FormatUint(f.id("Babbage"), 10) + `", "rank": 4}]}` + "\n```"
	sel = f.steps.Match(in)
	if !sel.Matched || !sel.Shift || len(sel.Entries) != 3 || sel.Entries[1].Entry.Title != "Babbage" || sel.Entries[2].Entry.Title != "Engine" {
		t.Errorf("matched %+v", sel)
	}

	in.Skip = true
	if sel := f.steps.Match(in); sel.Matched || len(f.chat.got) != 2 {
		t.Errorf("skipped stage 1 still called the model: %+v", sel)
	}
}

func TestUpdate(t *testing.T) {
	f := newFixture(t)
	round := conversation.Round{Model: db.Model{ID: 99}, ConversationID: 7, Text: "Ada wrote the first program.", Reply: "Yes, in 1843."}
	in := UpdateIn{ServiceArgs: f.args(), Round: round, Used: []uint64{f.id("Ada")}, MaxProposals: 1}

	if _, err := f.steps.Update(in); err == nil {
		t.Fatal("a failed call is not an error")
	}
	if call := f.chat.got[0]; call.Level != "" || call.Meta.Feature != "chronicle-memory" {
		t.Errorf("an unknown stage level should use the default level: %+v", call)
	}

	adaID, byronID := strconv.FormatUint(f.id("Ada"), 10), strconv.FormatUint(f.id("Byron"), 10)
	f.chat.replies["chronicle-memory"] = `{"updates": [
		{"id": "` + adaID + `", "shortSummary": "Wrote the first program.", "fullSummary": "Ada facts. Wrote the first program in 1843."},
		{"id": "` + byronID + `", "shortSummary": "Not used.", "fullSummary": "Must not change."}],
	  "proposals": [
		{"type": "note", "title": "first program", "shortSummary": "s", "fullSummary": "f", "parentId": "` + adaID + `"},
		{"type": "note", "title": "ADA", "shortSummary": "dup", "fullSummary": "dup"},
		{"type": "note", "title": "Bernoulli numbers", "shortSummary": "over the limit", "fullSummary": "f"}]}`
	in.MaxProposals = 1
	changes, err := f.steps.Update(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Updated) != 1 || changes.Updated[0].ShortSummary != "Wrote the first program." || len(changes.Proposed) != 1 ||
		changes.Proposed[0].Status != chronicle.StatusProposed || changes.Proposed[0].ParentID != f.id("Ada") {
		t.Fatalf("changes %+v", changes)
	}
	var byron chronicle.Entry
	f.steps.DB.First(&byron, f.id("Byron"))
	if byron.ShortSummary != "About Byron." {
		t.Errorf("an unused entry was updated: %+v", byron)
	}
	var records []chronicle.EntryRound
	f.steps.DB.Order("id").Find(&records)
	if len(records) != 2 || records[0].Action != chronicle.ActionUpdated || records[0].RoundID != 99 ||
		!strings.Contains(records[0].Previous, `"shortSummary":"About Ada."`) || records[1].Action != chronicle.ActionCreated {
		t.Errorf("records %+v", records)
	}
}
