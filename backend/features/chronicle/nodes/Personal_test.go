package chroniclenodes

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
	"github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
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

type fakeRetriever struct{ ids []uint64 }

func (f *fakeRetriever) Retrieve(retrievalservice.RetrieveArgs) retrievalservice.RetrieveResponse {
	resp := retrievalservice.RetrieveResponse{Common: features.OK()}
	for i, id := range f.ids {
		key := strconv.FormatUint(id, 10)
		resp.Segments = append(resp.Segments, retrieval.Scored{Segment: retrieval.Segment{SourceID: chroniclesteps.SourceID, ID: key, Key: key}, Score: 1 / float64(i+1), RRF: 0.1})
	}
	return resp
}

type fixture struct {
	steps   chroniclesteps.Steps
	chat    *fakeChat
	engine  *engine.Engine
	entries map[string]chronicle.Entry
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(append(append(chronicle.Models(), conversation.Models()...), retrieval.Models()...), cognition.Models()...)...); err != nil {
		t.Fatal(err)
	}
	f := &fixture{chat: &fakeChat{replies: map[string]string{}}, entries: map[string]chronicle.Entry{}}
	p := features.LocalPrincipal()
	for _, name := range []string{"Ada", "Byron", "Engine"} {
		e := chronicle.Entry{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), Type: "entity", Title: name, ShortSummary: "About " + name + ".",
			FullSummary: name + " facts.", Fields: "{}", Status: chronicle.StatusActive}
		if err := gdb.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		f.entries[name] = e
	}
	retriever := &fakeRetriever{ids: []uint64{f.entries["Engine"].ID, f.entries["Byron"].ID}}
	f.steps = chroniclesteps.Steps{DB: gdb, Chat: f.chat, Retriever: func() retrievalservice.Service { return retriever },
		Config: sections{
			aisettings.StagesID: `{"match": "fast", "memory": "fast"}`,
			aisettings.LevelsID: `{"levels": [{"id": "fast"}, {"id": "standard", "default": true}]}`,
		}}
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	conversationnodes.Contribute(reg)
	Contribute(reg, func() chroniclesteps.Steps { return f.steps })
	f.engine = engine.New(gdb, reg, nil)
	return f
}

func (f *fixture) args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal(), Meta: features.Meta{ConversationID: 7, RoundID: 8}}
}

func TestPersonalIsValid(t *testing.T) {
	f := newFixture(t)
	g, ok := f.engine.Graph(PersonalID)
	if !ok {
		t.Fatal("Personal is not registered")
	}
	if res := f.engine.Resolve(g); len(res.Problems) > 0 {
		t.Errorf("Personal has problems: %+v", res.Problems)
	}
}

// Personal's On user message gives the items slice 6's provider made by
// calling the steps itself.
func TestPersonalGathersAsTheStepsDo(t *testing.T) {
	f := newFixture(t)
	ada, engineID := strconv.FormatUint(f.entries["Ada"].ID, 10), strconv.FormatUint(f.entries["Engine"].ID, 10)
	f.chat.replies["chronicle-match"] = `{"shift": false, "entries": [{"id": "` + engineID + `", "rank": 1}]}`
	window := []conversation.Round{{Model: db.Model{ID: 40}, Text: "Hi", Reply: "Hello"}}
	settings := json.RawMessage(`{"topK": 2, "checked": ["` + ada + `"]}`)

	candidates, err := f.steps.Candidates(chroniclesteps.CandidatesIn{ServiceArgs: f.args(), Text: "the engine?", Window: window, Checked: []uint64{f.entries["Ada"].ID}, Retrieve: 10})
	if err != nil {
		t.Fatal(err)
	}
	want := ContextItems(f.steps.Match(chroniclesteps.MatchIn{ServiceArgs: f.args(), Text: "the engine?", Window: window, Candidates: candidates, TopK: 2}))

	res, err := f.engine.Run(engine.RunArgs{ServiceArgs: f.args(), GraphID: PersonalID, Event: conversationnodes.EventUserMessage, Trigger: "chat",
		Inputs: cognition.Values{conversationnodes.PinText: "the engine?", conversationnodes.PinWindow: window, conversationnodes.PinSettings: settings}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := cognition.Get[[]conversation.ContextItem](res.Outputs, OutItems)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(gotJSON) != string(wantJSON) || len(got) != 2 || got[1].Title != "Engine" {
		t.Errorf("items\n got %s\nwant %s", gotJSON, wantJSON)
	}
	// The graph's stage 1 call is attributed to its run and node
	if call := f.chat.got[len(f.chat.got)-1]; call.Meta.RunID != res.RunID || call.Meta.NodeID != "match" || call.Meta.RoundID != 8 {
		t.Errorf("stage 1 meta %+v", call.Meta)
	}
}

func TestPersonalUpdatesMemoryOnlyWhenOn(t *testing.T) {
	f := newFixture(t)
	ada := strconv.FormatUint(f.entries["Ada"].ID, 10)
	f.chat.replies["chronicle-memory"] = `{"updates": [{"id": "` + ada + `", "shortSummary": "Programmer.", "fullSummary": "Ada facts. Programmer."}], "proposals": []}`
	round := conversation.Round{Model: db.Model{ID: 8}, ConversationID: 7, Text: "Ada programmed.", Reply: "Indeed."}
	segments := []retrieval.RoundSegment{{SourceID: chroniclesteps.SourceID, SegmentKey: ada}, {SourceID: "library", SegmentKey: "x"}}
	reply := func(settings string) (cognition.Values, error) {
		res, err := f.engine.Run(engine.RunArgs{ServiceArgs: f.args(), GraphID: PersonalID, Event: conversationnodes.EventReply,
			Inputs: cognition.Values{conversationnodes.PinRound: round, conversationnodes.PinSegments: segments, conversationnodes.PinSettings: json.RawMessage(settings)}})
		return res.Outputs, err
	}

	for _, off := range []string{`{"update": false}`, `{"enabled": false}`} {
		if out, err := reply(off); err != nil || len(out) != 0 || len(f.chat.got) != 0 {
			t.Errorf("%s: %v, %v, %d calls", off, out, err, len(f.chat.got))
		}
	}
	out, err := reply(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := cognition.Get[chroniclesteps.Changes](out, OutChanges)
	if err != nil || len(changes.Updated) != 1 || changes.Updated[0].ShortSummary != "Programmer." {
		t.Errorf("changes %+v, %v", changes, err)
	}
	if call := f.chat.got[0]; call.Meta.NodeID != "update" || !strings.Contains(call.Messages[0].Content, "## id "+ada) || !strings.Contains(call.Messages[0].Content, "Ada programmed.") {
		t.Errorf("memory update call %+v", call)
	}
}
