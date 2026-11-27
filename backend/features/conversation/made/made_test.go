package made_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/SgtFlame/foundation/backend/features/cognition/traces"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/made"
	conversationnodes "github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	conversationsettings "github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
	toolsnodes "github.com/SgtFlame/foundation/backend/features/tools/nodes"
)

type sections map[string]string

func (s sections) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	if v, ok := s[section]; ok {
		return json.RawMessage(v), nil
	}
	return json.RawMessage(`{}`), nil
}

type fixture struct {
	t      *testing.T
	reg    *extensions.Registry
	p      *project.Projections
	bus    *bus.Bus
	engine *engine.Engine
	deps   made.Deps
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	conversationnodes.Contribute(reg)
	for _, p := range []string{"workbench", "rag", "activity"} {
		conversation.ContextProviders.Register(reg, conversation.ContextProvider{Descriptor: extensions.Descriptor{ID: p}, Title: p,
			Gather: func(conversation.GatherArgs) ([]conversation.ContextItem, error) { return nil, nil }})
	}
	conversation.ContextProviders.Register(reg, conversation.ContextProvider{Descriptor: extensions.Descriptor{ID: "tools"}, Title: "Tools"})
	conversation.Modes.Register(reg, conversation.ModeEntry{Descriptor: extensions.Descriptor{ID: "appBuilder"}, Label: "App Builder", Icon: "🛠️", GraphRef: made.TemplateID,
		Focus: &conversation.Focus{Kinds: []string{"prefab", "blueprint"}}})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "kb.search"}, Title: "Search", Description: "Searches the Volumes", Effect: tools.EffectRead})
	tools.Tools.Register(reg, tools.Tool{Descriptor: extensions.Descriptor{ID: "mail.label"}, Title: "Label", Description: "Labels a thread", Effect: tools.EffectWrite})
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "t.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(cognition.Models(), tools.Models()...)...); err != nil {
		t.Fatal(err)
	}
	cfg := sections{
		"chat":       `{"systemPrompt": "Be helpful."}`,
		"chat.modes": `{"modes": [{"id": "personal", "label": "Personal", "icon": "🧠", "graphRef": "chatModeTemplate", "default": true}]}`,
	}
	toolsnodes.Contribute(reg, func() toolsnodes.Deps {
		return toolsnodes.Deps{Calls: conversation.CallsIn(gdb), Registry: reg, Config: cfg}
	})
	f := &fixture{t: t, reg: reg}
	var draftDeps drafts.Deps
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	made.Contribute(reg, func() made.Deps { return f.deps })
	ops.Contribute(reg)
	srv := natsd.StartTest(t)
	f.p = project.New(srv.JS(), reg)
	t.Cleanup(f.p.Close)
	f.engine = engine.New(traces.In(gdb), reg, cfg)
	f.engine.SetSource(drafts.Source{Store: f.p})
	draftDeps = drafts.Deps{Engine: f.engine, Projections: f.p}
	f.bus = bus.New(f.p)
	f.deps = made.Deps{Store: f.p, Bus: f.bus, Engine: f.engine, Config: cfg,
		Apps: func(features.ServiceArgs) []made.App {
			return []made.App{{ID: "mail", Title: "Mail", Kinds: []string{"app:mail:main", "app:mail:thread"}}}
		}}
	conversationsettings.SetMadeModes(made.Source{Deps: func() made.Deps { return f.deps }})
	t.Cleanup(func() { conversationsettings.SetMadeModes(nil) })
	return f
}

func as(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1},
		Meta: features.Meta{RoundID: 31}}
}

var ana, ben = as(7), as(8)

func (f *fixture) tool(args features.ServiceArgs, name string, a any) (tools.Result, error) {
	f.t.Helper()
	for _, t := range tools.Available(f.reg, args) {
		if t.ID == name {
			raw, _ := json.Marshal(a)
			return t.Run(tools.Call{ServiceArgs: args, Name: name, Args: raw})
		}
	}
	f.t.Fatalf("no tool %s", name)
	return tools.Result{}, nil
}

// modes is what args' principal sees, once the projection holds the
// commit at seq.
func (f *fixture) modes(args features.ServiceArgs, seq uint64) conversationsettings.Modes {
	f.t.Helper()
	if seq > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, p := range []string{made.Projector, drafts.PublishedProjector} {
			if err := f.p.Settle(ctx, 1, p); err != nil {
				f.t.Fatal(err)
			}
		}
	}
	m, err := conversationsettings.ReadModes(f.deps.Config, args)
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func labels(m conversationsettings.Modes) string {
	var out []string
	for _, x := range m.Modes {
		out = append(out, x.Label)
	}
	return strings.Join(out, ",")
}

var support = map[string]any{
	"label": "Support answers", "icon": "🎧", "purpose": "Answers customers' support questions briefly from the Support Volume.",
	"instructions": "Answer support questions in two or three sentences, from the passages given. Say {{so}} when they do not answer it.",
	"context":      []string{"rag"}, "tools": []string{"kb.search"},
	"focus": map[string]any{"kinds": []string{"app:mail:*"}},
}

// chat.mode.create makes a published mode graph from the template and its
// entry in one request: Ana sees it, Ben does not until it is shared, it
// undoes as one request, and a round in it runs its graph with its tools.
func TestCreateMakesAModeInOneRequest(t *testing.T) {
	f := setup(t)
	res, err := f.tool(ana, "chat.mode.create", support)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.Content["mode"].(string)
	if !strings.HasPrefix(id, "mode-") || res.Content["graph"] != id || res.Summary != "Made 🎧 Support answers, now in your mode menu" {
		t.Fatalf("result = %+v", res)
	}
	seq, _ := res.Content["seq"].(uint64)
	if seq == 0 {
		seq = 1
	}
	mine := f.modes(ana, seq)
	if labels(mine) != "Personal,Support answers" {
		t.Fatalf("Ana sees %s", labels(mine))
	}
	m, _ := mine.Find(id)
	if !m.Made || !m.Mine || m.Shared || m.GraphRef != id || m.Focus == nil || m.Focus.Kinds[0] != "app:mail:*" {
		t.Fatalf("mode = %+v", m)
	}
	if labels(f.modes(ben, 0)) != "Personal" {
		t.Fatalf("Ben sees %s before it is shared", labels(f.modes(ben, 0)))
	}

	// The graph is published, with the instructions, context and tools set
	g, ok := f.engine.Graph(ana, id)
	if !ok || g.Version != 1 || g.Name != "Support answers" || g.Kind != cognition.KindMode {
		t.Fatalf("graph %+v %v", g, ok)
	}
	config := func(node string) string {
		n, _ := g.NodeByID(node)
		return string(n.Config)
	}
	if !strings.Contains(config("gather"), `["rag"]`) || !strings.Contains(config("toolList"), `[\"kb.search\"]`) ||
		!strings.Contains(config("instructions"), `{{\"{{\"}}so}}`) || !slices.Equal(cognition.ModeSpecOf(g).Settings, []string{"rag", "tools", "debug"}) {
		t.Fatalf("graph nodes: %s %s %s %s", config("gather"), config("toolList"), config("instructions"), g.Spec)
	}

	// A round in it runs its graph: its instructions after the system
	// prompt, its context, and kb.search alone
	h := &host{server: tools.Available(f.reg, ana)}
	if r, err := f.engine.Run(engine.RunArgs{ServiceArgs: ana, GraphID: id, Event: conversationnodes.EventUserMessage, Host: h,
		Inputs: cognition.Values{conversationnodes.PinText: "How do I reset my password?", conversationnodes.PinTitle: "Named"}}); err != nil {
		t.Fatalf("the mode failed on %s: %v", r.FailedNode, err)
	}
	if !slices.Equal(h.gathered, []string{"rag"}) || len(h.replied.Tools) != 1 || h.replied.Tools[0].ID != "kb.search" ||
		h.replied.System != "Be helpful.\n\n# Mode: Support answers\nAnswer support questions in two or three sentences, from the passages given. Say {{so}} when they do not answer it." {
		t.Fatalf("round: gathered %v, tools %+v, system %q", h.gathered, h.replied.Tools, h.replied.System)
	}

	// Shared, Ben sees it too
	share := f.bus.Execute(bus.Request{ServiceArgs: ana, ID: "share-1", Type: "chat.mode.share", Target: id, Args: json.RawMessage(`{"shared":true}`)})
	if share.Common.Code >= 300 {
		t.Fatalf("share: %s %+v", share.Common.Message, share.Problems)
	}
	if b := f.modes(ben, share.Seq); labels(b) != "Personal,Support answers" || b.Modes[1].Mine || !b.Modes[1].Shared {
		t.Fatalf("Ben sees %+v once shared", b.Modes)
	}
	// Ben may not change Ana's mode (a policy-less test allows config.write,
	// so a shared mode is anyone's to change here; an unshared one is not)
	unshare := f.bus.Execute(bus.Request{ServiceArgs: ana, ID: "share-2", Type: "chat.mode.share", Target: id, Args: json.RawMessage(`{"shared":false}`)})
	f.modes(ana, unshare.Seq)
	if r := f.bus.Execute(bus.Request{ServiceArgs: ben, ID: "ben-1", Type: "chat.mode.update", Target: id, Args: json.RawMessage(`{"label":"Mine now"}`)}); r.Common.Code < 300 {
		t.Fatal("Ben changed Ana's mode")
	}

	// Undoing the round's request removes the mode and its graph together
	u, fail := ops.Undo(ana, ops.Deps{Bus: f.bus, Store: f.p}, ops.UndoArgs{Request: "round-31"}, false, "chat")
	if fail != nil {
		t.Fatal(fail.Message)
	}
	if labels(f.modes(ana, u.Seq)) != "Personal" {
		t.Fatalf("after undo (%s, %s) Ana sees %s", u.Status, u.Summary, labels(f.modes(ana, 0)))
	}
	if _, ok := f.engine.Graph(ana, id); ok {
		t.Fatalf("the graph %s is still there after undo: %+v", id, u)
	}
}

// chat.mode.create refuses a label another mode has, a place that does
// not exist and a tool the person may not use, and makes nothing.
func TestCreateRefuses(t *testing.T) {
	f := setup(t)
	with := func(k string, v any) map[string]any {
		a := map[string]any{}
		for key, val := range support {
			a[key] = val
		}
		a[k] = v
		return a
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{with("label", "personal!"), `already a mode called "Personal"`},
		{with("focus", map[string]any{"kinds": []string{"app:nothing:*"}}), `no place "app:nothing:*"`},
		{with("tools", []string{"kb.search", "admin.everything"}), `"admin.everything" is not one you may give`},
		{with("context", []string{"chronicle"}), `no context "chronicle"`},
		{with("from", "nothing"), `no mode graph "nothing"`},
	} {
		if _, err := f.tool(ana, "chat.mode.create", c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.args, err, c.want)
		}
	}
	if labels(f.modes(ana, 0)) != "Personal" {
		t.Fatalf("something was made: %s", labels(f.modes(ana, 0)))
	}
	// The places the App Builder is offered on are places too
	ok := with("focus", map[string]any{"kinds": []string{"prefab"}})
	ok["tools"] = []string{}
	res, err := f.tool(ana, "chat.mode.create", ok)
	if err != nil {
		t.Fatal(err)
	}
	f.modes(ana, 1)
	g, _ := f.engine.Graph(ana, res.Content["graph"].(string))
	if n, _ := g.NodeByID("toolList"); !strings.Contains(string(n.Config), `"expr":"[]"`) || slices.Contains(cognition.ModeSpecOf(g).Settings, "tools") {
		t.Fatalf("no tools: %s, %s", n.Config, g.Spec)
	}
}

// chat.mode.options gives the modes, the context, the places and the
// tools grouped by app.
func TestOptions(t *testing.T) {
	f := setup(t)
	res, err := f.tool(ana, "chat.mode.options", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.Content)
	for _, want := range []string{`"label":"Personal"`, `"id":"rag"`, `"label":"on Mail's tabs","focus":{"kinds":["app:mail:*"]}`,
		`"label":"where 🛠️ App Builder is offered"`, `"group":"kb"`, `"chat.askForm: chat.askForm"`, `"graph":"chatModeTemplate"`, `"mayShare":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("options lack %s", want)
		}
	}
	if strings.Contains(string(raw), `"id":"tools"`) {
		t.Error("the tools setting is not a context")
	}
	// It fits a tool result whatever the number of tools: names and titles
	// only, a group's descriptions on request
	for i := range 400 {
		tools.Tools.Register(f.reg, tools.Tool{Descriptor: extensions.Descriptor{ID: fmt.Sprintf("app%d.tool%d", i%40, i)}, Title: "A tool", Description: strings.Repeat("Does something useful. ", 30), Effect: tools.EffectWrite})
	}
	res, err = f.tool(ana, "chat.mode.options", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ = json.Marshal(res.Content); len(raw) > 24<<10 || strings.Contains(string(raw), "Does something useful") {
		t.Errorf("the options are %d bytes, with descriptions", len(raw))
	}
	res, err = f.tool(ana, "chat.mode.options", map[string]any{"group": "kb"})
	if raw, _ = json.Marshal(res.Content); err != nil || !strings.Contains(string(raw), `"description":"Searches the Volumes"`) || strings.Contains(string(raw), "mail.label") {
		t.Errorf("the kb group: %s %v", raw, err)
	}
}

// host is a chat round.
type host struct {
	server   []tools.Tool
	gathered []string
	replied  conversation.ReplyArgs
}

func (h *host) Turn() conversation.Turn {
	return conversation.Turn{Conversation: conversation.Conversation{Title: "Named"}, Round: conversation.Round{Text: "How do I reset my password?"}}
}
func (h *host) Settings(string) json.RawMessage { return json.RawMessage(`{}`) }
func (h *host) Gather(p []string) []conversation.ContextItem {
	h.gathered = p
	return nil
}
func (h *host) ServerTools() []tools.Tool              { return h.server }
func (h *host) ClientTools() []tools.Tool              { return nil }
func (h *host) Say(string) error                       { return nil }
func (h *host) SetTitle(string)                        {}
func (h *host) Followed(string, conversation.Followed) {}
func (h *host) AddContext(system string, _ []conversation.ContextItem, _, _ string) (string, []retrieval.RoundSegment) {
	return system, nil
}
func (h *host) Reply(args conversation.ReplyArgs) (conversation.ReplyOutcome, error) {
	h.replied = args
	return conversation.ReplyOutcome{OK: true, Round: conversation.Round{Reply: "Use the reset link."}}, nil
}
