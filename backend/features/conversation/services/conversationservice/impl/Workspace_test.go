package impl

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/tools"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/context"
	"github.com/SgtFlame/foundation/backend/features/retrieval/retrievaltest"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

// recorded keeps every retrieval a round asks for.
type recorded struct {
	retrievalservice.Service
	mu  sync.Mutex
	got []retrievalservice.RetrieveArgs
}

func (r *recorded) Retrieve(args retrievalservice.RetrieveArgs) retrievalservice.RetrieveResponse {
	r.mu.Lock()
	r.got = append(r.got, args)
	r.mu.Unlock()
	return r.Service.Retrieve(args)
}

func (r *recorded) take() []retrievalservice.RetrieveArgs {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.got
	r.got = nil
	return out
}

const (
	support uint64 = 9101
	hr      uint64 = 9102
)

// forge 16c: a round searches the Volumes and uses the Chronicle entries
// of the workspace it was sent from, read on the server from the caller's
// own; RAG Search's gather and kb.search alike.
func TestRoundsFollowTheirWorkspace(t *testing.T) {
	chat := &scriptedChat{}
	s, _ := newTestService(t, &fakeChat{})
	s.chat = chat
	root := t.TempDir()
	stores := retrievaltest.Contribute(s.registry, root)
	rec := &recorded{Service: retrievaltest.Service(s.registry, s.db)}
	retrievalcontext.Contribute(s.registry, func() retrievalservice.Service { return rec })
	kbtools.Contribute(s.registry, func() kbtools.Deps { return kbtools.Deps{Root: root, Retrieval: rec} })
	stores.Library.Add(support, "support/refunds.md", "Vacation refunds are paid within five days.")
	stores.Library.Add(hr, "hr/leave.md", "Vacation leave needs a manager's approval.")

	var checked [][]string
	conversation.ContextProviders.Register(s.registry, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: conversation.ChronicleID}, Defaults: json.RawMessage(`{"enabled": true}`),
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			var v struct {
				Checked []string `json:"checked"`
			}
			_ = json.Unmarshal(args.Settings, &v)
			checked = append(checked, v.Checked)
			return nil, nil
		},
	})
	// Ana's workspaces: Support leaves HR out and checks entry 7; Default is
	// not made yet, so it has the defaults
	var asked []string
	s.workspaces = func(args features.ServiceArgs, id string) (conversation.WorkspaceKnowledge, bool) {
		asked = append(asked, id)
		if args.Principal.UserID != features.LocalPrincipal().UserID {
			return conversation.WorkspaceKnowledge{}, false // another person's workspaces are none
		}
		switch id {
		case "support":
			return conversation.WorkspaceKnowledge{ID: id, Name: "Support", ExceptVolumeIDs: []uint64{hr}, CheckedEntries: []string{"7"}}, true
		case "hr":
			return conversation.WorkspaceKnowledge{ID: id, Name: "HR", ExceptVolumeIDs: []uint64{support}}, true
		}
		return conversation.WorkspaceKnowledge{}, false
	}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	search := chatservice.Message{Role: chatservice.RoleAssistant, ToolCalls: []chatservice.ToolCall{{Name: "kb_search", Args: json.RawMessage(`{"query": "vacation"}`)}}}
	paths := func(events map[conversationservice.SendEventKind][]conversationservice.SendEvent) []string {
		var out []string
		for _, e := range events[conversationservice.EventSources] {
			for _, seg := range e.Segments {
				if seg.SourceID == "library" && !slices.Contains(out, seg.Title) {
					out = append(out, seg.Title)
				}
			}
		}
		slices.Sort(out)
		return out
	}
	send := func(workspace string) map[conversationservice.SendEventKind][]conversationservice.SendEvent {
		t.Helper()
		chat.mu.Lock()
		chat.script = []chatservice.Message{search, {Role: chatservice.RoleAssistant, Content: "done"}}
		chat.mu.Unlock()
		checked = nil
		return collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "vacation", Workspace: workspace,
			// a conversation's own checks from before 16c are ignored
			ContextSettings: map[string]json.RawMessage{conversation.ChronicleID: json.RawMessage(`{"enabled": true, "checked": ["99"]}`)}}))
	}
	excepted := func(want []uint64) {
		t.Helper()
		got := rec.take()
		if len(got) != 2 { // the gather and kb.search
			t.Fatalf("retrievals %+v", got)
		}
		for _, g := range got {
			if !slices.Equal(g.ExceptVolumeIDs, want) {
				t.Errorf("retrieval left out %v, want %v", g.ExceptVolumeIDs, want)
			}
		}
	}

	// Support: HR's passage is never found, entry 7 is checked
	events := send("support")
	if p := paths(events); len(p) != 1 || !strings.Contains(p[0], "refunds") {
		t.Errorf("Support found %v", p)
	}
	excepted([]uint64{hr})
	if len(checked) != 1 || !slices.Equal(checked[0], []string{"7"}) {
		t.Errorf("Support's checked entries %v", checked)
	}

	// HR: the other way round, and nothing checked
	events = send("hr")
	if p := paths(events); len(p) != 1 || !strings.Contains(p[0], "leave") {
		t.Errorf("HR found %v", p)
	}
	excepted([]uint64{support})
	if len(checked) != 1 || len(checked[0]) != 0 {
		t.Errorf("HR's checked entries %v", checked)
	}

	// No workspace named: the caller's Default, not made yet, has the defaults
	events = send("")
	if p := paths(events); len(p) != 2 {
		t.Errorf("Default found %v", p)
	}
	excepted(nil)
	if asked[len(asked)-1] != conversation.DefaultWorkspace {
		t.Errorf("read %v", asked)
	}

	// Someone else's id, or one that does not exist: refused before a round
	other := args
	other.Principal.UserID = 2
	for _, a := range []conversationservice.SendArgs{{ServiceArgs: args, Text: "hi", Workspace: "gone"}, {ServiceArgs: other, Text: "hi", Workspace: "support"}} {
		failed := collect(s.Send(a))[conversationservice.EventFailed]
		if len(failed) != 1 || failed[0].Common.Code != 404 || failed[0].Common.ErrorCode != "workspace_not_found" {
			t.Errorf("%+v sent to %q: %+v", a.Principal, a.Workspace, failed)
		}
	}
	if len(rec.take()) != 0 {
		t.Error("a refused message searched")
	}
}

// A scope on the context outlasts options set after it (forge 16c): a
// model node's Retrieval group replaces the round's options, never the
// workspace's narrowing.
func TestScopeOutlastsOptions(t *testing.T) {
	ctx := retrieval.WithScope(t.Context(), retrieval.Scope{ExceptVolumeIDs: []uint64{hr}})
	ctx = retrieval.WithOptions(ctx, retrieval.Options{VolumeIDs: []uint64{hr, support}})
	if got := retrieval.ScopeFrom(ctx).ExceptVolumeIDs; !slices.Equal(got, []uint64{hr}) {
		t.Errorf("scope %v", got)
	}
}
