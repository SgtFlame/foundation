// Package engine runs Cognition graphs (documentation/plans/Cognition.md ›
// Execution). Slice 7 builds linear exec flow and pure nodes: exec flow is
// followed from the triggering event node, one node at a time, and a pure
// node runs when a downstream node pulls one of its outputs, once per run.
// Every node execution is recorded as a cognition.Step, the trace the Log
// Viewer shows. Loops, Parallel, sub-graphs, debugging and replay come in
// later passes.
package engine

import (
	"context"
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

	purgeMu   sync.Mutex
	lastPurge time.Time
}

// purgeEvery is how often, at most, a finished run purges old traces.
const purgeEvery = 10 * time.Minute

// New makes an engine over the registry's cognition.node and
// cognition.graph contributions, keeping traces in gdb.
func New(gdb *gorm.DB, reg *extensions.Registry, cfg config.Reader) *Engine {
	return &Engine{db: gdb, registry: reg, config: cfg}
}

// Registry is the registry the engine's graphs, node types and data types
// come from.
func (e *Engine) Registry() *extensions.Registry {
	return e.registry
}

// Graphs are the graphs the engine can run: the built-in ones, for now.
func (e *Engine) Graphs() []cognition.Graph {
	builtIns := cognition.Graphs.All(e.registry)
	out := make([]cognition.Graph, len(builtIns))
	for i, b := range builtIns {
		out[i] = b.Graph
		out[i].ID = b.ID
	}
	return out
}

// Graph is the graph with the given ID.
func (e *Engine) Graph(id string) (cognition.Graph, bool) {
	for _, g := range e.Graphs() {
		if g.ID == id {
			return g, true
		}
	}
	return cognition.Graph{}, false
}

// NodeTypes are the registry's node types.
func (e *Engine) NodeTypes() []cognition.NodeType {
	return cognition.Nodes.All(e.registry)
}

// Resolve validates a graph against the registry's node types.
func (e *Engine) Resolve(g cognition.Graph) cognition.Resolved {
	return cognition.Resolve(g, e.NodeTypes())
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
	if s.MaxSteps <= 0 {
		s.MaxSteps = d.MaxSteps
	}
	if s.MaxWallSeconds <= 0 {
		s.MaxWallSeconds = d.MaxWallSeconds
	}
	if s.MaxValueKB <= 0 {
		s.MaxValueKB = d.MaxValueKB
	}
	return s
}

// RunArgs start a run.
type RunArgs struct {
	features.ServiceArgs
	GraphID string
	Event   string           // the node type of the event that starts it, e.g. "event.userMessage"
	Inputs  cognition.Values // the event node's outputs
	Trigger string           // what started it, e.g. "chat"
}

// Result is a finished run.
type Result struct {
	RunID   uint64
	Outputs cognition.Values // what Return nodes returned; empty when none ran
}

// Run runs the graph from the event, synchronously, and records its trace.
// A run that fails returns the error of the node it failed on, with the
// run's ID in Result so it can be found.
func (e *Engine) Run(args RunArgs) (Result, error) {
	g, ok := e.Graph(args.GraphID)
	if !ok {
		return Result{}, log.Code("graph_not_found").With("graphId", args.GraphID).Errorf("there is no graph %q", args.GraphID)
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
	r := newRun(e, args, res, settings)
	r.record.Event = event
	persist := context.WithoutCancel(args.Context())
	if err := e.db.WithContext(persist).Create(&r.record).Error; err != nil {
		return Result{}, log.Wrap(err)
	}

	ctx, cancel := context.WithTimeout(args.Context(), time.Duration(settings.MaxWallSeconds)*time.Second)
	defer cancel()
	r.args.Ctx = ctx
	err := r.from(event)
	r.finish(err)

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
	go e.purge(persist, settings)
	return Result{RunID: r.record.ID, Outputs: r.result}, err
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
		result: cognition.Values{},
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
