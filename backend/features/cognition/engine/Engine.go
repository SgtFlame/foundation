// Package engine runs Cognition graphs (documentation/Cognition.md ›
// Execution). Exec flow is followed from the triggering event node, one
// node at a time; a flow node (Sequence, For each, Loop) runs the chains
// leaving it through ExecHooks.Flow, a loop's iterations each in their own
// execution context. A pure node runs when a downstream node pulls one of
// its outputs, once per execution context. Every node execution is
// recorded as a cognition.Step, the trace the Log Viewer shows; a run with
// TraceFailures keeps its trace only when it fails. Parallel, debugging and
// replay come in later passes.
package engine

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

var log = logging.In("cognition")

// Engine runs the graphs and node types of a registry.
type Engine struct {
	db       *gorm.DB
	registry *extensions.Registry
	config   config.Reader // nil uses cognitionsettings.Defaults
	source   GraphSource   // the tenants' published graphs; nil runs built-ins only

	purgeMu   sync.Mutex
	lastPurge time.Time
	purges    sync.WaitGroup // purges running, which tests wait for

	countMu  sync.Mutex
	untraced map[string]int64 // runs not traced, by tenant and graph
}

// GraphSource gives a tenant's published graphs (cognition/drafts).
type GraphSource interface {
	// Published is the published version of graph id in the principal's
	// tenant; ok is false when it has none.
	Published(args features.ServiceArgs, id string) (g cognition.Graph, ok bool, err error)
}

// DraftSource is a GraphSource that also gives drafts (cognition/drafts),
// for dry runs (forge 04c).
type DraftSource interface {
	Draft(args features.ServiceArgs, id string) (g cognition.Graph, ok bool, err error)
}

// purgeEvery is how often, at most, a finished run purges old traces.
const purgeEvery = 10 * time.Minute

// New makes an engine over the registry's cognition.node and
// cognition.graph contributions, keeping traces in gdb.
func New(gdb *gorm.DB, reg *extensions.Registry, cfg config.Reader) *Engine {
	return &Engine{db: gdb, registry: reg, config: cfg, untraced: map[string]int64{}}
}

// SetSource sets where the tenants' published graphs come from; the drafts
// are made after the engine, since their commands validate against it.
func (e *Engine) SetSource(s GraphSource) { e.source = s }

// DB is the database the traces are kept in.
func (e *Engine) DB() *gorm.DB { return e.db }

// Registry is the registry the engine's graphs, node types and data types
// come from.
func (e *Engine) Registry() *extensions.Registry {
	return e.registry
}

// Graphs are the built-in graphs.
func (e *Engine) Graphs() []cognition.Graph {
	builtIns := cognition.Graphs.All(e.registry)
	out := make([]cognition.Graph, len(builtIns))
	for i, b := range builtIns {
		out[i] = b.Graph
		out[i].ID = b.ID
	}
	return out
}

// BuiltIn is the built-in graph with the given ID.
func (e *Engine) BuiltIn(id string) (cognition.BuiltIn, bool) {
	for _, b := range cognition.Graphs.All(e.registry) {
		if b.ID == id {
			b.Graph.ID = b.ID
			return b, true
		}
	}
	return cognition.BuiltIn{}, false
}

// Graph is the graph with the given ID as the principal's tenant runs it:
// its published version, else the built-in one.
func (e *Engine) Graph(args features.ServiceArgs, id string) (cognition.Graph, bool) {
	if e.source != nil {
		g, ok, err := e.source.Published(args, id)
		if err != nil {
			log.With("graphId", id).Error(args.Context(), err, "cannot read the published graph %s", id)
		}
		if ok {
			return g, true
		}
	}
	b, ok := e.BuiltIn(id)
	return b.Graph, ok
}

// Draft is the graph with the given ID as it is being edited: its draft,
// else what Graph gives. Dry runs use it (forge 04c).
func (e *Engine) Draft(args features.ServiceArgs, id string) (cognition.Graph, bool) {
	if d, ok := e.source.(DraftSource); ok {
		g, ok, err := d.Draft(args, id)
		if err != nil {
			log.With("graphId", id).Error(args.Context(), err, "cannot read the draft of graph %s", id)
		}
		if ok {
			return g, true
		}
	}
	return e.Graph(args, id)
}

// NodeTypes are the registry's node types.
func (e *Engine) NodeTypes() []cognition.NodeType {
	return cognition.Nodes.All(e.registry)
}

// Catalog is the registry's node types, kinds and conversions.
func (e *Engine) Catalog() cognition.Catalog {
	return cognition.CatalogOf(e.registry)
}

// Resolve validates a graph against the registry's catalog.
func (e *Engine) Resolve(g cognition.Graph) cognition.Resolved {
	return e.Catalog().Resolve(g)
}

// Settings are the cognition section, or its defaults when it cannot be read.
func (e *Engine) Settings(args features.ServiceArgs) cognitionsettings.Cognition {
	s := cognitionsettings.Defaults
	if e.config != nil {
		if read, err := config.Read[cognitionsettings.Cognition](e.config, args, cognitionsettings.ID); err == nil {
			s = read
		}
	}
	d := cognitionsettings.Defaults
	for _, f := range []struct{ v, def *int }{{&s.MaxSteps, &d.MaxSteps}, {&s.MaxWallSeconds, &d.MaxWallSeconds}, {&s.MaxValueKB, &d.MaxValueKB},
		{&s.MaxLoopIterations, &d.MaxLoopIterations}, {&s.MaxCallDepth, &d.MaxCallDepth}} {
		if *f.v <= 0 {
			*f.v = *f.def
		}
	}
	return s
}

// Trace modes (Blueprints question 4).
const (
	TraceAll      = ""         // every run is traced
	TraceFailures = "failures" // only failed runs are; the others are counted
)

// RunArgs start a run.
type RunArgs struct {
	features.ServiceArgs
	GraphID string
	// Graph, when set, is run in place of the graph GraphID names: a
	// draft under test.
	Graph   *cognition.Graph
	Event   string           // the node type of the event that starts it, e.g. "event.userMessage"
	Inputs  cognition.Values // the event node's outputs
	Trigger string           // what started it, e.g. "chat"
	Host    any              // what its nodes act on (cognition.Exec.Host)
	Trace   string           // TraceAll or TraceFailures
	Limits  *Limits          // tighter limits than the cognition section's, for a kind
	// Stub runs External nodes as stubs that give their Sample (forge 06f:
	// a sandboxed run), in the graphs it calls too.
	Stub bool
	// Steps keeps the run's steps in its Result, for a caller that reports
	// them whether or not the trace is stored.
	Steps bool
	depth int // how deep in Call graph
}

// Limits narrow a run's limits; zero keeps the section's.
type Limits struct {
	MaxSteps  int
	MaxWallMs int
	MaxLoop   int
}

// Result is a finished run.
type Result struct {
	RunID      uint64
	Traced     bool             // whether the run's trace was kept
	Outputs    cognition.Values // what Return nodes returned; empty when none ran
	FailedNode string           // the node a failed run failed on
	Steps      []cognition.Step // with RunArgs.Steps: what each node was given and gave
	Stubbed    []string         // with RunArgs.Stub: the External nodes stubbed, by node id
}

// Untraced is how many runs of a graph in the principal's tenant were not
// traced since the server started.
func (e *Engine) Untraced(args features.ServiceArgs, graphID string) int64 {
	e.countMu.Lock()
	defer e.countMu.Unlock()
	return e.untraced[untracedKey(args, graphID)]
}

func untracedKey(args features.ServiceArgs, graphID string) string {
	return strconv.FormatUint(args.Principal.TenantID, 10) + "/" + graphID
}

// Run runs the graph from the event, synchronously, and records its trace.
// A run that fails returns the error of the node it failed on, with the
// run's ID in Result so it can be found.
func (e *Engine) Run(args RunArgs) (Result, error) {
	var g cognition.Graph
	if args.Graph != nil {
		g = *args.Graph
	} else {
		var ok bool
		if g, ok = e.Graph(args.ServiceArgs, args.GraphID); !ok {
			return Result{}, log.Code("graph_not_found").With("graphId", args.GraphID).Errorf("there is no graph %q", args.GraphID)
		}
	}
	res := e.Resolve(g)
	if res.Errors() {
		for _, p := range res.Problems {
			if p.Severity == cognition.SeverityError {
				return Result{}, log.Code("invalid_graph").With("graphId", g.ID, "nodeId", p.NodeID).Errorf("graph %s is not valid: %s", g.ID, p.Message)
			}
		}
	}
	event := ""
	for _, n := range g.Nodes {
		if t := res.Types[n.ID]; t.Event && t.ID == args.Event {
			event = n.ID
		}
	}
	if event == "" {
		return Result{}, log.Code("no_event").With("graphId", g.ID, "event", args.Event).Errorf("graph %s has no %q event", g.ID, args.Event)
	}

	settings := e.Settings(args.ServiceArgs)
	if l := args.Limits; l != nil {
		if l.MaxSteps > 0 {
			settings.MaxSteps = min(settings.MaxSteps, l.MaxSteps)
		}
		if l.MaxLoop > 0 {
			settings.MaxLoopIterations = min(settings.MaxLoopIterations, l.MaxLoop)
		}
	}
	wall := time.Duration(settings.MaxWallSeconds) * time.Second
	if l := args.Limits; l != nil && l.MaxWallMs > 0 {
		wall = min(wall, time.Duration(l.MaxWallMs)*time.Millisecond)
	}
	r := newRun(e, args, res, settings)
	r.record.Event = event
	r.record.ID = features.NewTSID()
	persist := context.WithoutCancel(args.Context())
	traceAll := args.Trace == TraceAll
	if traceAll {
		if err := e.db.WithContext(persist).Create(&r.record).Error; err != nil {
			return Result{}, log.Wrap(err)
		}
	}

	ctx, cancel := context.WithTimeout(args.Context(), wall)
	defer cancel()
	r.args.Ctx = ctx
	err := r.from(event)
	r.finish(err)

	kept := func(res Result) Result {
		res.Stubbed = r.stubbed
		if args.Steps {
			res.Steps = r.steps
		}
		return res
	}
	if !traceAll && err == nil {
		e.countMu.Lock()
		e.untraced[untracedKey(args.ServiceArgs, g.ID)]++
		e.countMu.Unlock()
		return kept(Result{RunID: r.record.ID, Outputs: r.result}), nil
	}
	if dbErr := e.db.WithContext(persist).Transaction(func(tx *gorm.DB) error {
		if len(r.steps) > 0 {
			if err := tx.CreateInBatches(r.steps, 100).Error; err != nil {
				return err
			}
		}
		return tx.Save(&r.record).Error
	}); dbErr != nil {
		log.With("runId", r.record.ID).Error(persist, dbErr, "cannot store the trace of run %d", r.record.ID)
	}
	e.purges.Go(func() { e.purge(persist, settings) })
	return kept(Result{RunID: r.record.ID, Traced: true, Outputs: r.result, FailedNode: r.record.FailedNode}), err
}

// call runs another graph from its On call event, for Call graph.
func (e *Engine) call(parent *run, graphID string, inputs cognition.Values) (cognition.Values, error) {
	if parent.args.depth+1 >= parent.settings.MaxCallDepth {
		return nil, log.Code("call_depth").Errorf("graphs called graphs more than %d deep", parent.settings.MaxCallDepth)
	}
	args := RunArgs{ServiceArgs: parent.args.ServiceArgs, GraphID: graphID, Event: cognition.EventCall, Inputs: inputs,
		Trigger: "graph:" + strconv.FormatUint(parent.record.ID, 10), Host: parent.args.Host, Trace: parent.args.Trace, Stub: parent.args.Stub, depth: parent.args.depth + 1}
	res, err := e.Run(args)
	return res.Outputs, err
}

// purge deletes the traces that are neither among their graph's last
// KeepRuns runs (per tenant) nor younger than KeepDays, at most once every
// purgeEvery.
func (e *Engine) purge(ctx context.Context, s cognitionsettings.Cognition) {
	e.purgeMu.Lock()
	if time.Since(e.lastPurge) < purgeEvery {
		e.purgeMu.Unlock()
		return
	}
	e.lastPurge = time.Now()
	e.purgeMu.Unlock()
	if err := e.Purge(ctx, s.KeepRuns, s.KeepDays); err != nil {
		log.Error(ctx, err, "cannot purge old Cognition traces")
	}
}

// Purge deletes the traces that are neither among their graph's last
// keepRuns runs (per tenant) nor younger than keepDays.
func (e *Engine) Purge(ctx context.Context, keepRuns, keepDays int) error {
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM cognition_runs WHERE started_at < ? AND id NOT IN (
			SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY tenant_id, graph_id ORDER BY started_at DESC, id DESC) AS n FROM cognition_runs) WHERE n <= ?)`,
			cutoff, keepRuns).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM cognition_run_steps WHERE run_id NOT IN (SELECT id FROM cognition_runs)`).Error
	})
}

// newRun prepares a run's state: the exec successors and data sources by pin.
func newRun(e *Engine, args RunArgs, res cognition.Resolved, s cognitionsettings.Cognition) *run {
	r := &run{
		engine: e, args: args, res: res, settings: s,
		next: map[cognition.PinRef]string{}, source: map[cognition.PinRef]cognition.PinRef{},
		values: map[string]cognition.Values{}, done: map[string]bool{}, pulling: map[string]bool{},
		vars: cognition.Values{}, result: cognition.Values{},
	}
	for _, v := range res.Graph.Variables {
		r.vars[v.ID] = v.Default
	}
	for _, edge := range res.Graph.Edges {
		if pin, ok := res.Pins[edge.From.Node].Find(edge.From.Pin, false); ok && pin.Exec {
			r.next[edge.From] = edge.To.Node
		} else {
			r.source[edge.To] = edge.From
		}
	}
	r.record = cognition.Run{
		Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal),
		GraphID: res.Graph.ID, GraphVersion: res.Graph.Version, Trigger: args.Trigger, Status: cognition.StatusRunning,
		ConversationID: args.Meta.ConversationID, RoundID: args.Meta.RoundID, StartedAt: time.Now(),
	}
	return r
}
