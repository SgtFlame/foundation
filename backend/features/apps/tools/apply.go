package apptools

import (
	"cmp"
	"encoding/json"
	"fmt"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"maps"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Change is an op another author applied, as forge.apply and
// forge.changes report it (forge 06e).
type Change struct {
	Seq       uint64 `json:"seq"`
	Request   string `json:"request"`
	Command   string `json:"command"`
	Target    string `json:"target"`
	Summary   string `json:"summary"`
	Principal string `json:"principal"`       // the user's id
	Actor     string `json:"actor,omitempty"` // chat or agent, when not by hand
	You       bool   `json:"you,omitempty"`   // the caller's own, made outside this request
}

// OpResult is one op of a batch forge.apply committed.
type OpResult struct {
	Index   int    `json:"index"`
	Command string `json:"command"`
	Target  string `json:"target"` // made by the command when the op had none
	Events  int    `json:"events"` // 0: it changed nothing
}

// asks reports whether a batch needs the user's approval (forge 06
// question 2): drafts change without asking, since they are undone by
// request and only published versions run; publishing, installing and
// any write outside the definitions (records) ask.
func asks(b *bus.Bus, args json.RawMessage) bool {
	var a struct{ Ops []live.Op }
	if json.Unmarshal(args, &a) != nil || b == nil {
		return true
	}
	for _, op := range a.Ops {
		family, name, _ := strings.Cut(op.Command, ".")
		if name == "publish" || (family == "app" && slices.Contains([]string{"install", "upgrade", "uninstall"}, name)) {
			return true
		}
		if cmd, ok := b.Command(op.Command); ok && cmd.Area != live.AreaDefinitions {
			return true
		}
	}
	return false
}

// ContributeApply registers the forge.* tools that change and check an
// app (forge 06e): forge.apply, forge.changes, forge.check, forge.preview
// and forge.undo.
func ContributeApply(reg *extensions.Registry, deps func() ForgeDeps) {
	builder := newBuilderPresence(func() *presence.Presence { return deps().Presence })
	conversation.RoundEnds.Register(reg, conversation.RoundEnd{
		Descriptor: extensions.Descriptor{ID: "forge.presence", Source: "apps"},
		End:        func(args features.ServiceArgs, round conversation.Round) { builder.End(args, round.ID) },
	})
	mayRead := func(call tools.Call, app string) error {
		res := apps.Resource(call.Principal, app)
		return authz.Check(call.Context(), call.Principal, "app.read", res).Err("app.read", res)
	}
	// settle waits until every definition projection has folded the stream,
	// so what was just committed reads back
	settle := func(call tools.Call) error {
		store := deps().Store
		for _, pr := range live.Projectors.All(store.Registry()) {
			if pr.Area == live.AreaDefinitions {
				if err := store.Settle(call.Context(), call.Principal.TenantID, pr.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// changes are the app's ops after a sequence that are not this request's
	changes := func(call tools.Call, app string, after uint64, request string) ([]Change, error) {
		if err := settle(call); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(map[string]any{"app": app, "after": after})
		data, _, fail := deps().Subscriptions.Once(call.ServiceArgs, "ops.list", raw)
		if fail != nil {
			return nil, fmt.Errorf("%s", fail.Message)
		}
		b, _ := json.Marshal(data)
		var l ops.Log
		_ = json.Unmarshal(b, &l)
		out := []Change{}
		for _, r := range l.Requests {
			if r.ID == request {
				continue
			}
			for _, e := range r.Ops {
				out = append(out, Change{Seq: e.Seq, Request: e.Request, Command: e.Command, Target: e.TargetType + ":" + e.Target, Summary: e.Summary,
					Principal: fmt.Sprint(e.Principal), Actor: e.Actor, You: e.Principal == call.Principal.UserID})
			}
		}
		slices.SortFunc(out, func(a, b Change) int { return cmp.Compare(a.Seq, b.Seq) })
		return out, nil
	}

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.apply", Source: "apps"}, Title: "Change an app", Effect: tools.EffectWrite,
		Asks:        func(args json.RawMessage) bool { return asks(deps().Bus, args) },
		Description: "Applies a batch of ops (commands such as entity.create, entity.addField, prefab.insertNode, app.addPage, app.setNav) to an app's drafts as one atomic change: each op is decided against the state the ops before it left, and either all commit or none. An op with no target, of a command that makes one, gets a new id (returned in results). A refused op names its index (ops.<i>) with a fix, and nothing is committed: correct it and apply the batch again. Gives the sequence, each op's result, the app's problems after it, and, given after (the sequence you last read, from forge.app's position or a previous apply), the ops other authors applied since, so you do not work from a stale view. Drafts change without asking the user; a batch that publishes, installs or writes records asks first. Every op must be in one area: records go in a batch of their own. One batch holds at most 1000 events: split a larger change.",
		Input:       json.RawMessage(`{"type":"object","properties":{"appId":{"type":"string","description":"The app the batch is for (app.list)"},"ops":{"type":"array","description":"The ops, in order","items":{"type":"object","properties":{"command":{"type":"string","description":"A command type, e.g. entity.addField"},"target":{"type":"string","description":"The id of what it changes; leave it out for a command that makes its target"},"args":{"type":"object","description":"The command's arguments, as its own tool takes them in args"}},"required":["command"]}},"after":{"type":"integer","description":"The definitions stream's sequence you last read: other authors' ops after it are listed"}},"required":["appId","ops"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				AppID string
				Ops   []live.Op
				After uint64
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			if !live.ValidToken(a.AppID) {
				return tools.Result{}, fmt.Errorf("%q is not an app id", a.AppID)
			}
			if len(a.Ops) == 0 {
				return tools.Result{}, fmt.Errorf("the batch has no ops")
			}
			request := livetools.RequestOf(call)
			t := deps().Bus.Transact(bus.Request{ServiceArgs: call.ServiceArgs, ID: fmt.Sprintf("tool-%d", features.NewTSID()), RequestID: request, App: a.AppID,
				Actor: livetools.ActorOf(call)}, a.Ops, false)
			if t.Common.Code >= 300 {
				if len(t.Problems) == 0 {
					return tools.Result{}, fmt.Errorf("%s", t.Common.Message)
				}
				p := t.Problems[0]
				what := "the batch"
				var i int
				if _, err := fmt.Sscanf(p.Path, "ops.%d", &i); err == nil && i < len(a.Ops) {
					what = fmt.Sprintf("ops.%d (%s, the op at index %d)", i, a.Ops[i].Command, i)
				}
				msg := fmt.Sprintf("%s was refused: %s. Nothing was committed", what, p.Message)
				if p.Fix != "" {
					msg += "; fix: " + p.Fix
				}
				return tools.Result{Content: map[string]any{"status": "refused", "committed": false, "problems": t.Problems}}, fmt.Errorf("%s", msg)
			}
			builder.Touch(call.ServiceArgs, a.AppID, a.Ops)
			results := []OpResult{}
			for i, d := range t.Ops {
				results = append(results, OpResult{Index: i, Command: d.Op.Command, Target: d.Op.Target, Events: d.Events})
			}
			content := map[string]any{"status": "applied", "request": t.RequestID, "seq": t.Seq, "results": results}
			if err := settle(call); err != nil {
				return tools.Result{}, err
			}
			problems := []live.Problem{}
			store := deps().Store
			if d, ok := manifests.GetDraft(call.Context(), store, call.Principal.TenantID, a.AppID); ok {
				problems = append(problems, apps.CheckManifest(d.Manifest, manifests.Env(call.Context(), store, call.Principal.TenantID, reg))...)
			}
			content["problems"] = problems
			summary := fmt.Sprintf("Applied %d ops to %s", len(results), a.AppID)
			if a.After > 0 {
				others, err := changes(call, a.AppID, a.After, request)
				if err != nil {
					return tools.Result{}, err
				}
				content["changes"] = others
				if len(others) > 0 {
					summary += fmt.Sprintf("; %d changes by others since %d", len(others), a.After)
				}
			}
			if len(problems) > 0 {
				summary += fmt.Sprintf("; %d problems", len(problems))
			}
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.changes", Source: "apps"}, Title: "Others' changes to an app", Effect: tools.EffectRead,
		Description: "Lists the ops other authors (people, or another chat) applied to an app after a sequence, oldest first, each with its summary, who made it and its request; your own ops in this round are left out. Use it to catch up during a long build, before changing what someone else may have changed.",
		Input:       json.RawMessage(`{"type":"object","properties":{"appId":{"type":"string"},"after":{"type":"integer","description":"A definitions stream sequence, from forge.app's position or forge.apply's seq"}},"required":["appId","after"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				AppID string
				After uint64
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			if err := mayRead(call, a.AppID); err != nil {
				return tools.Result{}, err
			}
			out, err := changes(call, a.AppID, a.After, livetools.RequestOf(call))
			if err != nil {
				return tools.Result{}, err
			}
			return tools.Result{Content: map[string]any{"changes": out}, Summary: fmt.Sprintf("%d changes since %d", len(out), a.After)}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.check", Source: "apps"}, Title: "Check an app", Effect: tools.EffectRead,
		Description: "Checks an app's draft as it would be published and exported. errors stop it publishing or running: validation of the manifest and every part, and the blueprints-only check (every reference a primitive or one of the app's, the tenant's or a required app's parts). warnings do not: pages nothing reaches (not home, in the nav or an editor kind, and no part navigates to them), parts of the app nothing uses, and parts and pages with no description. Each has a path and a fix.",
		Input:       json.RawMessage(`{"type":"object","properties":{"appId":{"type":"string"}},"required":["appId"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ AppID string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			if err := mayRead(call, a.AppID); err != nil {
				return tools.Result{}, err
			}
			if err := settle(call); err != nil {
				return tools.Result{}, err
			}
			errs, warnings, ok := manifests.CheckDraft(call.Context(), deps().Store, call.Principal.TenantID, reg, a.AppID)
			if !ok {
				return tools.Result{}, fmt.Errorf("there is no app %q; list them with app.list", a.AppID)
			}
			return tools.Result{Content: map[string]any{"errors": errs, "warnings": warnings},
				Summary: fmt.Sprintf("%d errors, %d warnings", len(errs), len(warnings))}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.preview", Source: "apps"}, Title: "Preview a page", Effect: tools.EffectRead,
		Description: "Renders a page of an app's draft at a route (with its params, e.g. /task/42) as the app's draft run shows it: every prefab in its draft, its queries read once on the app's records. as names one of the manifest's roles: the page is read with that role's actions and row rules in place of yours, as a user holding only that role would see it (your own user id stays principal.user). When none of the app's entity types is published yet, the page prefab is rendered with its preview data instead. Gives the tree summarized (types, keys, texts and data, events, bindings; no styling) and the problems each at its key path. No screenshot.",
		Input:       json.RawMessage(`{"type":"object","properties":{"appId":{"type":"string"},"route":{"type":"string","description":"A route of the app, its params filled in, e.g. /mine or /task/42"},"as":{"type":"string","description":"The id of one of the manifest's roles, e.g. member"}},"required":["appId","route"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ AppID, Route, As string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			if err := settle(call); err != nil {
				return tools.Result{}, err
			}
			out, err := preview(call, reg, deps(), a.AppID, a.Route, a.As)
			if err != nil {
				return tools.Result{}, err
			}
			summary := fmt.Sprintf("Previewed %s %s", a.AppID, a.Route)
			if a.As != "" {
				summary += " as " + a.As
			}
			return tools.Result{Content: out, Summary: summary}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.undo", Source: "apps"}, Title: "Undo a chat's change", Effect: tools.EffectWrite,
		Asks:        func(json.RawMessage) bool { return false },
		Description: "Undoes a request's changes to an app's drafts as one new request: the last request a chat made for you on the app that is not undone yet, or the one named by request (from forge.changes or a result's request). Another author's later change to the same item is a conflict: nothing is undone and they are named; ask the user, then call ops.undo with force to undo the rest. Publishing and installs are not undone.",
		Input:       json.RawMessage(`{"type":"object","properties":{"appId":{"type":"string"},"request":{"type":"string","description":"A request id, e.g. round-42; the chat's last when left out"}},"required":["appId"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ AppID, Request string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			if !live.ValidToken(a.AppID) {
				return tools.Result{}, fmt.Errorf("%q is not an app id", a.AppID)
			}
			d := deps()
			request := a.Request
			if request == "" {
				if err := settle(call); err != nil {
					return tools.Result{}, err
				}
				raw, _ := json.Marshal(map[string]any{"scope": "app:" + a.AppID})
				data, _, fail := d.Subscriptions.Once(call.ServiceArgs, "ops.stack", raw)
				if fail != nil {
					return tools.Result{}, fmt.Errorf("%s", fail.Message)
				}
				b, _ := json.Marshal(data)
				var s ops.Stack
				_ = json.Unmarshal(b, &s)
				if i := slices.IndexFunc(s.Undo, func(it ops.StackItem) bool { return it.Actor == "chat" }); i >= 0 {
					request = s.Undo[i].Request
				}
				if request == "" {
					return tools.Result{Content: map[string]any{"status": "nothing"}, Summary: "Nothing a chat did to undo"}, nil
				}
			}
			res, fail := ops.Undo(call.ServiceArgs, ops.Deps{Bus: d.Bus, Store: d.Store}, ops.UndoArgs{Request: request}, false, livetools.ActorOf(call))
			if fail != nil {
				return tools.Result{}, fmt.Errorf("%s", fail.Message)
			}
			raw, _ := json.Marshal(res)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: "Undo: " + res.Status + ", " + res.Summary}, nil
		},
	})
}

// preview renders a page of an app's draft for forge.preview (forge 06e
// question 3): the draft run at route, as a manifest role when as is
// given, its tree summarized.
func preview(call tools.Call, reg *extensions.Registry, d ForgeDeps, app, route, as string) (map[string]any, error) {
	pv, err := PreviewPage(call, reg, d, app, route, as)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"route": route, "page": pv.Page.Route, "prefab": pv.Page.Prefab, "data": pv.Data, "tree": Summarize(pv.Tree), "problems": pv.Problems}
	if len(pv.Params) > 0 {
		out["params"] = pv.Params
	}
	if as != "" {
		out["as"] = as
	}
	return out, nil
}

// Previewed is a page of an app's draft as forge.preview renders it,
// before its tree is summarized.
type Previewed struct {
	Page     apps.Page
	Params   map[string]string
	Data     string // "records", or "preview data" when no entity type of the app is published
	Tree     *ui.Node
	Problems []ui.Problem
}

// PreviewPage renders a page of an app's draft at route, as a manifest
// role when as is given: forge.preview's page with its whole tree, styling
// included, for tests that assert on it (forge 06h).
func PreviewPage(call tools.Call, reg *extensions.Registry, d ForgeDeps, app, route, as string) (Previewed, error) {
	res := apps.Resource(call.Principal, app)
	if err := authz.Check(call.Context(), call.Principal, "app.write", res).Err("app.write", res); err != nil {
		return Previewed{}, err
	}
	tenant := call.Principal.TenantID
	dr, ok := manifests.GetDraft(call.Context(), d.Store, tenant, app)
	if !ok {
		return Previewed{}, fmt.Errorf("there is no app %q; list them with app.list", app)
	}
	m := dr.Manifest
	pg, params, ok := m.FindPage(route)
	if !ok {
		var routes []string
		for _, p := range m.Pages {
			routes = append(routes, p.Route)
		}
		return Previewed{}, fmt.Errorf("%s has no page at %s: its routes are %s", m.Name, route, strings.Join(routes, ", "))
	}
	args, err := roleArgs(call.ServiceArgs, reg, d, m, app, as)
	if err != nil {
		return Previewed{}, err
	}
	// Records need a published entity type: with none, the page's preview data
	published := false
	for _, id := range m.Entities {
		if _, ok, _ := d.Store.Get(call.Context(), tenant, entities.PublishedProjector, entities.TargetType+"."+id); ok {
			published = true
		}
	}
	o := ui.Options{Catalog: ui.Catalog(reg), Subs: d.Subscriptions}
	var tree *ui.Node
	var problems []ui.Problem
	data := "records"
	if !published {
		data = "preview data"
		t, p, fail := ui.Preview(args, o, pg.Prefab, "", nil, true)
		if fail != nil {
			return Previewed{}, fmt.Errorf("%s", fail.Message)
		}
		tree, problems = t, p
	} else {
		var page ui.Page
		for _, src := range ui.AppSources.All(reg) {
			if p, mine, err := src.Page(args, app+"/draft"); err == nil && mine {
				page, ok = p, true
				break
			}
		}
		if !ok {
			return Previewed{}, fmt.Errorf("the draft of %s cannot be run here", app)
		}
		var root *ui.Node
		o.Send = func(f any) {
			if t, ok := f.(ui.TreeFrame); ok {
				root = t.Tree
			}
		}
		raw, _ := json.Marshal(map[string]any{"route": route})
		s, fail := ui.Open(args, fmt.Sprintf("preview-%d", features.NewTSID()), page, raw, nil, o)
		if fail != nil {
			return Previewed{}, fmt.Errorf("%s", fail.Message)
		}
		s.Close(false)
		// The page, without the app's frame around it: the draft run keys
		// it page_<route>
		tree = pageOf(root)
		problems = ui.TreeProblems(tree)
	}
	return Previewed{Page: pg, Params: params, Data: data, Tree: tree, Problems: problems}, nil
}

// roleArgs are args that decide as one of an app draft's manifest roles
// in place of the user (forge 06e question 3); args as given when as is
// empty.
func roleArgs(args features.ServiceArgs, reg *extensions.Registry, d ForgeDeps, m apps.Manifest, app, as string) (features.ServiceArgs, error) {
	if as == "" {
		return args, nil
	}
	i := slices.IndexFunc(m.Roles, func(r apps.Role) bool { return r.ID == as })
	if i < 0 {
		var ids []string
		for _, r := range m.Roles {
			ids = append(ids, r.ID)
		}
		return args, fmt.Errorf("%s has no role %q: its roles are %s", m.Name, as, strings.Join(ids, ", "))
	}
	env := manifests.Env(args.Context(), d.Store, args.Principal.TenantID, reg)
	args.Ctx = authz.WithAs(args.Context(), AsRole(m, m.Roles[i], func(id string) bool {
		e, ok := env.Entity(id)
		return ok && e.Owner == apps.Owner(app)
	}))
	return args, nil
}

// AsRole is a manifest role as authz decides a preview by it (forge 06e
// question 3): record actions on the app's own entity types (owned) are
// what the role gives, under its row rules; every other check is the
// user's own.
func AsRole(m apps.Manifest, r apps.Role, owned func(entity string) bool) authz.As {
	gives := map[string]bool{} // <entity>.<action>
	for _, a := range r.Actions {
		if entity, acts, ok := apps.EntityAction(a); ok {
			for _, act := range acts {
				gives[entity+"."+act] = true
			}
		}
	}
	return authz.As{Name: cmp.Or(r.Title, r.ID), Rules: r.RowRules, Covers: func(action, resource string) (bool, bool) {
		act, ok := strings.CutPrefix(action, "record.")
		if !ok {
			return false, false
		}
		i := strings.LastIndex(resource, "/entity:")
		if i < 0 {
			return false, false
		}
		entity := resource[i+len("/entity:"):]
		if !slices.Contains(m.Entities, entity) || !owned(entity) {
			return false, false
		}
		return true, gives[entity+"."+act]
	}}
}

// pageOf is the page inside an evaluated app frame: the first node keyed
// page_<route>; the root when there is no frame.
func pageOf(root *ui.Node) *ui.Node {
	if root == nil {
		return nil
	}
	if strings.HasPrefix(root.Key, "page_") {
		return root
	}
	for _, c := range root.Children {
		if p := pageOf(c); p != root && p != nil && strings.HasPrefix(p.Key, "page_") {
			return p
		}
	}
	for _, name := range slices.Sorted(maps.Keys(root.Slots)) {
		for _, c := range root.Slots[name] {
			if p := pageOf(c); p != nil && strings.HasPrefix(p.Key, "page_") {
				return p
			}
		}
	}
	return root
}

// plain is a prop's value without a record's bookkeeping (_seq,
// _createdAt…): only _id and _title are kept.
func plain(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = plain(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if strings.HasPrefix(k, "_") && k != "_id" && k != "_title" {
				continue
			}
			out[k] = plain(item)
		}
		return out
	}
	return v
}

// noise are the props a summarized tree leaves out: how it looks, not
// what it says.
var noise = map[string]bool{"sx": true, "style": true, "spacing": true, "direction": true, "alignItems": true, "justifyContent": true, "variant": true,
	"size": true, "color": true, "elevation": true, "wrap": true, "grow": true, "fill": true, "dense": true, "gap": true, "width": true, "height": true,
	"align": true, "divider": true, "icon": true, "fullWidth": true, "maxWidth": true, "minWidth": true, "square": true, "debounce": true, "disableGutters": true}

// maxItems is how many items of a list prop a summarized tree keeps.
const maxItems = 20

// Summarize is an evaluated tree for a model (forge 06e): each node's
// type and key, its props but for styling and records' bookkeeping, the events it sends, its
// bindings left for the browser, and its children and slots; lists cut
// to maxItems.
func Summarize(n *ui.Node) map[string]any {
	if n == nil {
		return nil
	}
	out := map[string]any{"type": n.Type, "key": n.Key}
	props := map[string]any{}
	for k, v := range n.Props {
		if noise[k] || v == nil || v == "" {
			continue
		}
		if list, ok := v.([]any); ok && len(list) > maxItems {
			v = append(slices.Clone(list[:maxItems]), fmt.Sprintf("… %d more", len(list)-maxItems))
		}
		props[k] = plain(v)
	}
	if len(props) > 0 {
		out["props"] = props
	}
	if len(n.On) > 0 {
		out["on"] = slices.Sorted(maps.Keys(n.On))
	}
	if len(n.Bind) > 0 {
		out["bind"] = n.Bind
	}
	if len(n.Children) > 0 {
		var children []map[string]any
		for _, c := range n.Children {
			children = append(children, Summarize(c))
		}
		out["children"] = children
	}
	if len(n.Slots) > 0 {
		slots := map[string]any{}
		for name, nodes := range n.Slots {
			var list []map[string]any
			for _, c := range nodes {
				list = append(list, Summarize(c))
			}
			slots[name] = list
		}
		out["slots"] = slots
	}
	return out
}
