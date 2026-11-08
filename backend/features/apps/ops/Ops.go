// Package ops is the operation log (forge 06a, documentation/Apps.md ›
// Built in slice 38): every definition command is an op, whose first
// event carries its arguments, its request, its actor and its inverse. The
// ops.log projection folds them into one entry per op, keyed by the app it
// belongs to and its sequence; ops.list reads them by app, request and
// principal, grouped by request; Replay sends an app's ops into another
// tenant.
package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

var log = apps.Log.Tags("ops")

// Projector is the operation log's projection.
const Projector = "ops.log"

// NoApp is the key part of ops on parts that belong to no app.
const NoApp = "_"

// Entry is one op in the log.
type Entry struct {
	Seq        uint64          `json:"seq"`     // the sequence it committed at: its last event's
	Decided    uint64          `json:"decided"` // its target's sequence when it was decided
	Command    string          `json:"command"`
	TargetType string          `json:"targetType"`
	Target     string          `json:"target"`
	Args       json.RawMessage `json:"args,omitempty"`
	App        string          `json:"app,omitempty"`
	Request    string          `json:"request"`
	CommandID  string          `json:"commandId"`
	Principal  uint64          `json:"principal,string"`
	Actor      string          `json:"actor,omitempty"`
	// Inverse is the ops that undo it, in order; Irreversible says why
	// there are none.
	Inverse      []live.Op `json:"inverse"`
	Irreversible string    `json:"irreversible,omitempty"`
	Time         time.Time `json:"time"`
}

// Request is one request's ops, oldest first.
type Request struct {
	ID        string    `json:"id"`
	App       string    `json:"app,omitempty"`
	Principal uint64    `json:"principal,string"`
	Actor     string    `json:"actor,omitempty"`
	Seq       uint64    `json:"seq"`  // its last op's
	Time      time.Time `json:"time"` // its last op's
	Ops       []Entry   `json:"ops"`
}

// Log is the ops.list query's result: requests, newest first.
type Log struct {
	Requests []Request `json:"requests"`
}

// keyOf is an op's key: its app and the sequence of its first event.
func keyOf(e live.Event) string {
	if e.Op == nil {
		return ""
	}
	app := e.Op.App
	if app == "" {
		app = NoApp
	}
	return fmt.Sprintf("%s.%012d", app, e.Seq)
}

func fold(f live.Fold) (json.RawMessage, error) {
	e := f.Event
	seq := e.Seq
	if e.Batch != nil {
		_, seq = e.Batch.Range(e.Seq)
	}
	return json.Marshal(Entry{
		Seq: seq, Decided: e.Op.Decided, Command: e.Op.Command, TargetType: e.Target.Type, Target: e.Op.Target, Args: e.Op.Args,
		App: e.Op.App, Request: e.RequestID, CommandID: e.CommandID, Principal: e.UserID, Actor: e.Actor,
		Inverse: e.Op.Inverse, Irreversible: e.Op.Irreversible, Time: e.Time,
	})
}

// Params are ops.list's parameters.
type Params struct {
	App       string `json:"app,omitempty"`
	Request   string `json:"request,omitempty"`
	Principal uint64 `json:"principal,string,omitempty"`
	After     uint64 `json:"after,omitempty"` // only ops committed after this sequence
}

func (p Params) keeps(e Entry) bool {
	return (p.Request == "" || e.Request == p.Request) && (p.Principal == 0 || e.Principal == p.Principal) && e.Seq > p.After
}

func (p Params) pattern() string {
	if p.App != "" {
		return p.App + ".*"
	}
	return ">"
}

// Contribute registers the ops.log projector and the ops.list query.
func Contribute(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: "apps"}, Title: "The operation log",
		Description: "Every definition command as an op, keyed <app>.<sequence> (_ for none): {seq, decided, command, targetType, target, args, app, request, commandId, principal, actor, inverse, irreversible, time}",
		Area:        live.AreaDefinitions, Fold: fold, KeyOf: keyOf,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "ops.list", Source: "apps"}, Title: "The operation log",
		Description: "The ops that changed definitions (graphs, prefabs, entity types, apps), grouped by request, newest request first: {requests: [{id, app, principal, actor, seq, time, ops: [{seq, command, targetType, target, args, app, request, principal, actor, inverse, irreversible, time}]}]}. app keeps one app's ops (its own parts' and those made while building it), request one request's, principal one user's, after those committed after a sequence. Each new op arrives as a change.",
		Params:      json.RawMessage(`{"type":"object","properties":{"app":{"type":"string"},"request":{"type":"string"},"principal":{"type":"string","description":"A user's id"},"after":{"type":"integer","description":"A stream sequence"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"app":"tasks"}`)},
		Action:      "app.read",
		Resource: func(p features.Principal, params json.RawMessage) string {
			var q Params
			_ = json.Unmarshal(params, &q)
			return apps.Resource(p, q.App)
		},
		Run: run,
	})
}

func run(f live.Feed) error {
	var q Params
	if err := json.Unmarshal(f.Params(), &q); err != nil {
		return log.Code("invalid_params").Wrap(err)
	}
	if q.App != "" && !live.ValidToken(q.App) {
		return log.Code("invalid_params").Errorf("%q is not an app id", q.App)
	}
	entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, Projector, q.pattern())
	if err != nil {
		return err
	}
	may := readable(f.Args())
	var all []Entry
	l := Log{Requests: []Request{}}
	pos := live.Position{}
	started := false
	for e := range entries {
		if e == nil {
			started = true
			l = group(all)
			f.Snapshot(l, pos)
			continue
		}
		pos = pos.Merge(e.Value.Position())
		var x Entry
		if e.Deleted || json.Unmarshal(e.Value.Value, &x) != nil || !q.keeps(x) || !may(x) {
			if started {
				f.Change(live.Reached, pos)
			}
			continue
		}
		if !started {
			all = append(all, x)
			continue
		}
		var delta live.Delta
		l, delta = add(l, x)
		f.Change(delta, pos)
	}
	return nil
}

// group is entries grouped by request, newest request first.
func group(entries []Entry) Log {
	slices.SortFunc(entries, func(a, b Entry) int { return cmp.Compare(a.Seq, b.Seq) })
	l := Log{Requests: []Request{}}
	for _, e := range entries {
		l, _ = add(l, e)
	}
	return l
}

// add puts an entry into its request, which becomes the newest, and gives
// the change that does it.
func add(l Log, e Entry) (Log, live.Delta) {
	zero := 0
	i := slices.IndexFunc(l.Requests, func(r Request) bool { return r.ID == e.Request })
	var r Request
	if i >= 0 {
		r = l.Requests[i]
		r.Ops = slices.Clone(r.Ops)
		l.Requests = slices.Delete(slices.Clone(l.Requests), i, i+1)
	} else {
		r = Request{ID: e.Request, App: e.App, Principal: e.Principal, Actor: e.Actor}
	}
	if j := slices.IndexFunc(r.Ops, func(o Entry) bool { return o.Seq == e.Seq }); j >= 0 {
		r.Ops[j] = e
	} else {
		r.Ops = append(r.Ops, e)
	}
	if e.Seq >= r.Seq {
		r.Seq, r.Time = e.Seq, e.Time
	}
	l.Requests = slices.Insert(l.Requests, 0, r)
	raw, _ := json.Marshal(r)
	if i >= 0 {
		return l, live.Delta{Op: "move", List: "requests", Key: r.ID, From: &i, Index: &zero, Value: raw}
	}
	return l, live.Delta{Op: "insert", List: "requests", Key: r.ID, Index: &zero, Value: raw}
}

// readable is whether the principal may see an op: an app's when they may
// read the app (app.read), a library part's when they may read the part.
func readable(args features.ServiceArgs) func(Entry) bool {
	p := args.Principal
	seen := map[string]bool{}
	return func(e Entry) bool {
		action, resource := "app.read", apps.Resource(p, e.App)
		if e.App == "" {
			switch e.TargetType {
			case drafts.TargetType:
				action, resource = "cognition.read", drafts.Resource(p, e.Target)
			case "prefab":
				action, resource = "prefabs.read", ui.PrefabResource(p, e.Target)
			case "entity":
				action, resource = "records.read", records.Resource(p, e.Target)
			default:
				resource = apps.Resource(p, "")
			}
		}
		key := action + " " + resource
		ok, known := seen[key]
		if !known {
			ok = authz.Check(args.Context(), p, action, resource).Allowed
			seen[key] = ok
		}
		return ok
	}
}

// Store is what Replay reads the log from (live/project.Projections).
type Store interface {
	live.Store
	Settle(ctx context.Context, tenant uint64, projector string) error
	Registry() *extensions.Registry
}

// Entries are an app's ops in from's tenant, in order, once the log has
// folded every definition event.
func Entries(from features.ServiceArgs, store Store, app string) ([]Entry, error) {
	ctx, cancel := context.WithCancel(from.Context())
	defer cancel()
	tenant := from.Principal.TenantID
	if err := store.Settle(ctx, tenant, Projector); err != nil {
		return nil, err
	}
	ch, err := store.Watch(ctx, tenant, Projector, Params{App: app}.pattern())
	if err != nil {
		return nil, err
	}
	var out []Entry
	for e := range ch {
		if e == nil {
			break
		}
		var x Entry
		if !e.Deleted && json.Unmarshal(e.Value.Value, &x) == nil {
			out = append(out, x)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return cmp.Compare(a.Seq, b.Seq) })
	return out, nil
}

// Replay sends an app's ops, in order, as into's principal in its tenant:
// the check that an app's ops are complete (forge 06a). It is for tests,
// not a tool. It stops at the first op refused, naming it.
func Replay(from features.ServiceArgs, store Store, app string, into features.ServiceArgs, b *bus.Bus) (int, error) {
	entries, err := Entries(from, store, app)
	if err != nil {
		return 0, err
	}
	for i, e := range entries {
		if reads(e.Command) {
			// It reads other parts' published versions, which their own
			// ops changed just before: wait until every definition is folded
			if err := settle(into, store); err != nil {
				return i, err
			}
		}
		res := b.Execute(bus.Request{ServiceArgs: into, ID: fmt.Sprintf("replay-%d", features.NewTSID()), Type: e.Command, Target: e.Target,
			RequestID: "replay-" + e.Request, App: e.App, Actor: e.Actor, Args: e.Args})
		if res.Common.Code >= 300 {
			return i, log.Code("replay_refused").Errorf("op %d of %d, %s on %s, was refused: %s %v", i+1, len(entries), e.Command, e.Target, res.Common.Message, res.Problems)
		}
	}
	return len(entries), nil
}

// reads reports whether a command reads other parts' published versions:
// publishing an app pins its parts', installing takes a version.
func reads(command string) bool {
	return command == "app.publish" || command == "app.install" || command == "app.upgrade"
}

// settle waits until every definition projection has folded the stream.
func settle(args features.ServiceArgs, store Store) error {
	for _, pr := range live.Projectors.All(store.Registry()) {
		if pr.Area == live.AreaDefinitions {
			if err := store.Settle(args.Context(), args.Principal.TenantID, pr.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
