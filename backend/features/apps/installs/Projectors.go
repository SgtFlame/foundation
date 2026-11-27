package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// foldLimits keep a fold quick: it runs once per event.
var foldLimits = &engine.Limits{MaxSteps: 200, MaxWallMs: 2000, MaxLoop: 1000}

// validKey is what a KV key may hold.
var validKey = regexp.MustCompile(`^[-/_=.a-zA-Z0-9]+$`)

// ProjectorResolver resolves an installed app's projectors (forge 26d-1)
// for project.Projections.SetResolver: app.<appId>.<name> stands for the
// projector of the install's current generation,
// app.<appId>.<name>.<generation>, which folds the record events of its
// entity types with the version's copy of its graph, as the app's
// principal. A new generation (an upgrade changing the graph, key or
// entity types) is a new bucket and consumer, rebuilt from the stream's
// start, so every process folds each event once into it whichever saw the
// upgrade first.
func ProjectorResolver(deps func() Deps) project.Resolver {
	return func(ctx context.Context, tenant uint64, id string) (live.Projector, bool, error) {
		app, name, ok := apps.ParseProjectorID(id)
		if !ok {
			return live.Projector{}, false, nil
		}
		d := deps()
		in, ok := Running(ctx, d.Projections, tenant, app)
		if !ok {
			return live.Projector{}, false, nil
		}
		p, g, ok := in.Version.Projector(name)
		if !ok {
			return live.Projector{}, false, nil
		}
		key, err := apps.CompileKey(p.Key)
		if err != nil {
			return live.Projector{}, false, log.Code("projector_key").Wrapf(err, "the key of %s", id)
		}
		f := &folder{deps: deps, tenant: tenant, app: app, id: id, graph: g, key: key}
		return live.Projector{
			Descriptor:  extensions.Descriptor{ID: id + "." + p.Generation(g)},
			Title:       p.Title,
			Description: p.Description,
			Area:        live.AreaRecords,
			Types:       p.Entities,
			KeyOf:       f.keyOf,
			Fold:        f.fold,
		}, true, nil
	}
}

// folder folds one app projector's events. A consumer folds one event at
// a time, so the record keyOf read is kept for the fold that follows.
type folder struct {
	deps   func() Deps
	tenant uint64
	app    string
	id     string
	graph  cognition.Graph
	key    *vm.Program

	mu     sync.Mutex
	seq    uint64
	record any
}

// event is what the graph and the key read of an event.
func eventValue(e live.Event) map[string]any {
	var d any
	if len(e.Data) > 0 {
		_ = json.Unmarshal(e.Data, &d)
	}
	user := ""
	if e.UserID != 0 {
		user = fmt.Sprint(e.UserID)
	}
	return map[string]any{"type": e.Type, "entity": e.Target.Type, "id": e.Target.ID, "data": d, "user": user,
		"time": e.Time.UTC().Format(time.RFC3339Nano), "seq": e.Seq}
}

// recordOf is the event's record as projected at or after it.
func (f *folder) recordOf(e live.Event) any {
	f.mu.Lock()
	if f.seq == e.Seq && e.Seq != 0 {
		r := f.record
		f.mu.Unlock()
		return r
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var r any
	if v, err := f.deps().Projections.WaitFor(ctx, f.tenant, data.Projector, data.Key(e.Target.Type, e.Target.ID), e.Seq, 3*time.Second); err == nil && v.Value != nil {
		_ = json.Unmarshal(v.Value, &r)
	}
	f.mu.Lock()
	f.seq, f.record = e.Seq, r
	f.mu.Unlock()
	return r
}

func (f *folder) keyOf(e live.Event) string {
	out, err := expr.Run(f.key, map[string]any{"event": eventValue(e), "record": f.recordOf(e)})
	if err != nil || out == nil {
		if err != nil {
			log.With("projector", f.id).Warn(context.Background(), "the key of %s failed on event %d: %v; skipped", f.id, e.Seq, err)
		}
		return ""
	}
	k := fmt.Sprint(out)
	if k == "" {
		return ""
	}
	if !validKey.MatchString(k) {
		log.With("projector", f.id).Warn(context.Background(), "the key %q of %s is not a key; event %d skipped", k, f.id, e.Seq)
		return ""
	}
	return k
}

func (f *folder) fold(in live.Fold) (json.RawMessage, error) {
	d := f.deps()
	if d.Nodes.Engine == nil {
		return nil, fmt.Errorf("there is no engine to fold %s", f.id)
	}
	var state any
	if len(in.State) > 0 {
		_ = json.Unmarshal(in.State, &state)
	}
	g := f.graph
	res, err := d.Nodes.Engine.Run(engine.RunArgs{
		ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(f.tenant, f.app)},
		Graph:       &g, Event: livenodes.OnFold, Trigger: "projector", Trace: engine.TraceFailures, Limits: foldLimits,
		Inputs: cognition.Values{"state": state, "event": eventValue(in.Event), "record": f.recordOf(in.Event), "key": in.Key},
	})
	if err != nil {
		return nil, err
	}
	v, ok := res.Outputs["value"]
	if !ok || v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
