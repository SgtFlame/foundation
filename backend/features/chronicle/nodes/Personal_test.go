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
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/nodes"
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
	toolsnodes.Contribute(reg, func() toolsnodes.Deps { return toolsnodes.Deps{Chat: f.chat, Registry: reg} })
	Contribute(reg, func() Deps { return Deps{Steps: f.steps} })
	f.engine = engine.New(gdb, reg, sections{"chat": `{"systemPrompt": "Be kind.", "titleLevel": "fast"}`})
	return f
}

func (f *fixture) args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal(), Meta: features.Meta{ConversationID: 7, RoundID: 8}}
}

func TestPersonalIsValid(t *testing.T) {
	f := newFixture(t)
	g, ok := f.engine.Graph(f.args(), PersonalID)
	if !ok {
		t.Fatal("Personal is not registered")
	}
	if res := f.engine.Resolve(g); len(res.Problems) > 0 {
		t.Errorf("Personal has problems: %+v", res.Problems)
	}
	if b, _ := f.engine.BuiltIn(PersonalID); !b.Editable {
		t.Error("Personal is not an editable built-in")
	}
}

// The seed's prompts are the steps' defaults, so editing either is seen.
func TestPersonalSeedsTheDefaultPrompts(t *testing.T) {
	f := newFixture(t)
	g, _ := f.engine.Graph(f.args(), PersonalID)
	for id, want := range map[string][2]string{"match": {chroniclesteps.MatchSystem, chroniclesteps.MatchPrompt}, "update": {chroniclesteps.UpdateSystem, chroniclesteps.UpdatePrompt}} {
		n, _ := g.NodeByID(id)
		var c chroniclesteps.Prompts
		_ = json.Unmarshal(n.Config, &c)
		if c.System != want[0] || c.Prompt != want[1] {
			t.Errorf("%s's prompts are not the defaults", id)
		}
	}
}

// host is a chat round as Personal's chat nodes see it.
type host struct {
	turn      conversation.Turn
	settings  map[string]string
	replied   conversation.ReplyArgs
	reply     string
	segments  []retrieval.RoundSegment
	gathered  [][]string
	title     string
	followed  []conversation.Followed
	providers []string
}

func (h *host) Turn() conversation.Turn           { return h.turn }
func (h *host) Settings(p string) json.RawMessage { return json.RawMessage(h.settings[p]) }
func (h *host) ServerTools() []tools.Tool {
	return []tools.Tool{{Descriptor: extensions.Descriptor{ID: "kb.search"}, Effect: tools.EffectRead}}
}
func (h *host) ClientTools() []tools.Tool                  { return nil }
func (h *host) Say(string) error                           { return nil }
func (h *host) SetTitle(title string)                      { h.title = title }
func (h *host) Followed(_ string, f conversation.Followed) { h.followed = append(h.followed, f) }
func (h *host) Gather(providers []string) []conversation.ContextItem {
	h.gathered = append(h.gathered, providers)
	return []conversation.ContextItem{{Kind: "workbench", Title: "Workspace", Text: "a note is open", Score: 1}}
}
func (h *host) AddContext(system string, items []conversation.ContextItem, header, cite string) (string, []retrieval.RoundSegment) {
	var b strings.Builder
	b.WriteString(system + "\n\n" + header + "\n")
	for _, item := range items {
		if item.Segment != nil {
			seg := *item.Segment
			seg.Number = len(h.segments) + 1
			h.segments = append(h.segments, retrieval.RoundSegmentFrom(h.turn.Round.ID, h.turn.Round.Tenanted, seg))
		}
		b.WriteString("## " + item.Title + "\n" + item.Text + "\n")
	}
	return b.String(), h.segments
}
func (h *host) Reply(args conversation.ReplyArgs) (conversation.ReplyOutcome, error) {
	h.replied = args
	r := h.turn.Round
	r.Reply, r.Status = h.reply, 200
	return conversation.ReplyOutcome{Round: r, OK: true, Segments: h.segments}, nil
}

func (f *fixture) round(t *testing.T, h *host) engine.Result {
	t.Helper()
	all := map[string]json.RawMessage{}
	for k, v := range h.settings {
		all[k] = json.RawMessage(v)
	}
	res, err := f.engine.Run(engine.RunArgs{ServiceArgs: f.args(), GraphID: PersonalID, Event: conversationnodes.EventUserMessage, Trigger: "chat", Host: h,
		Inputs: cognition.Values{conversationnodes.PinText: h.turn.Round.Text, conversationnodes.PinWindow: h.turn.Window, conversationnodes.PinSettings: all,
			conversationnodes.PinTitle: h.turn.Conversation.Title, conversationnodes.PinRound: h.turn.Round}})
	if err != nil {
		t.Fatalf("Personal failed on %s: %v", res.FailedNode, err)
	}
	return res
}

// A round in Personal chooses entries as the steps do, adds them with the
// workbench to the chat's system prompt, replies with the tools, names the
// conversation and updates the entries it used.
func TestPersonalRunsARound(t *testing.T) {
	f := newFixture(t)
	ada, engineID := strconv.FormatUint(f.entries["Ada"].ID, 10), strconv.FormatUint(f.entries["Engine"].ID, 10)
	f.chat.replies["chronicle-match"] = `{"shift": false, "entries": [{"id": "` + engineID + `", "rank": 1}]}`
	f.chat.replies["chat-title"] = `"The Analytical Engine."`
	f.chat.replies["chronicle-memory"] = `{"updates": [{"id": "` + engineID + `", "shortSummary": "Babbage's.", "fullSummary": "Engine facts. Babbage's."}], "proposals": []}`
	window := []conversation.Round{{Model: db.Model{ID: 40}, Text: "Hi", Reply: "Hello"}}
	h := &host{
		turn:     conversation.Turn{Round: conversation.Round{Model: db.Model{ID: 8}, ConversationID: 7, Text: "the engine?"}, Window: window},
		settings: map[string]string{"chronicle": `{"topK": 2, "checked": ["` + ada + `"]}`, "tools": `{"enabled": true}`},
		reply:    "Babbage designed it.",
	}

	candidates, err := f.steps.Candidates(chroniclesteps.CandidatesIn{ServiceArgs: f.args(), Text: "the engine?", Window: window, Checked: []uint64{f.entries["Ada"].ID}, Retrieve: 10})
	if err != nil {
		t.Fatal(err)
	}
	want := ContextItems(f.steps.Match(chroniclesteps.MatchIn{ServiceArgs: f.args(), Text: "the engine?", Window: window, Candidates: candidates, TopK: 2}))
	f.chat.got = nil

	res := f.round(t, h)
	system := h.replied.System
	if !strings.HasPrefix(system, "Be kind.") || !strings.Contains(system, "# Tools\nYou can call tools") || !strings.Contains(system, "# Context\n") {
		t.Errorf("system prompt:\n%s", system)
	}
	for _, item := range append(want, conversation.ContextItem{Title: "Workspace"}) {
		if !strings.Contains(system, "## "+item.Title+"\n") {
			t.Errorf("the prompt lacks %s", item.Title)
		}
	}
	if len(h.gathered) != 1 || strings.Join(h.gathered[0], ",") != "workbench,rag" {
		t.Errorf("gathered %v", h.gathered)
	}
	if len(h.replied.Tools) != 1 || h.replied.Tools[0].ID != "kb.search" || len(h.replied.Messages) != 3 || h.replied.Messages[2].Content != "the engine?" {
		t.Errorf("reply args %+v", h.replied)
	}
	features := []string{}
	for _, c := range f.chat.got {
		features = append(features, c.Meta.Feature)
	}
	if strings.Join(features, ",") != "chronicle-match,chat-title,chronicle-memory" {
		t.Fatalf("model calls %v", features)
	}
	// Each call is attributed to the run and its node
	if call := f.chat.got[0]; call.Meta.RunID != res.RunID || call.Meta.NodeID != "match" || call.Meta.RoundID != 8 {
		t.Errorf("stage 1 meta %+v", call.Meta)
	}
	if call := f.chat.got[1]; call.Level != "fast" || !strings.Contains(call.Messages[0].Content, "Assistant: Babbage designed it.") {
		t.Errorf("title call %+v", call)
	}
	if h.title != "The Analytical Engine" {
		t.Errorf("title %q", h.title)
	}
	if call := f.chat.got[2]; call.Meta.NodeID != "update" || !strings.Contains(call.Messages[0].Content, "## id "+engineID) {
		t.Errorf("memory update call %+v", call)
	}
	if len(h.followed) != 1 || h.followed[0].Summary != "Memory: updated *Engine*" || !strings.Contains(string(h.followed[0].Data), `"title":"Engine"`) {
		t.Errorf("followed %+v", h.followed)
	}
}

// With memory and tools off, a named conversation's round only replies.
func TestPersonalWithMemoryOff(t *testing.T) {
	f := newFixture(t)
	h := &host{
		turn:     conversation.Turn{Conversation: conversation.Conversation{Title: "Named"}, Round: conversation.Round{Model: db.Model{ID: 8}, Text: "hi"}},
		settings: map[string]string{"chronicle": `{"enabled": false}`, "tools": `{"enabled": false}`},
		reply:    "Hello.",
	}
	f.round(t, h)
	if len(f.chat.got) != 0 || h.title != "" || len(h.followed) != 0 {
		t.Errorf("calls %+v, title %q, followed %+v", f.chat.got, h.title, h.followed)
	}
	if len(h.replied.Tools) != 0 || strings.Contains(h.replied.System, "# Tools") {
		t.Errorf("reply args %+v", h.replied)
	}
}
