package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"gorm.io/gorm"
)

// toy contributes a start event, an exec node that appends to its input, a
// pure node that doubles a number (counting its calls), a pure node that
// tests a string, and a failing node.
type toy struct {
	doubled int
}

func (t *toy) contribute(reg *extensions.Registry) {
	cognitionnodes.Contribute(reg)
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.start"}, Title: "Start", Event: true,
		Pins: cognition.Pins{Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("n", "N", "number", ""), cognition.DataPin("s", "S", "string", "")}},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.append"}, Title: "Append",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("s", "S", "string", ""), {ID: "n", Name: "N", Type: "number", Optional: true}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("s", "S", "string", "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			s, _ := cognition.Get[string](in, "s")
			n, _ := cognition.Get[int](in, "n")
			return cognition.Values{"s": s + strings.Repeat("!", n) + "@" + x.Meta.NodeID}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.double"}, Title: "Double",
		Pins: cognition.Pins{In: []cognition.Pin{cognition.DataPin("n", "N", "number", "")}, Out: []cognition.Pin{cognition.DataPin("n", "N", "number", "")}},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			t.doubled++
			n, err := cognition.Get[int](in, "n")
			return cognition.Values{"n": 2 * n, "big": uint64(1) << 60}, "", err
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.flag"}, Title: "Flag",
		Pins: cognition.Pins{In: []cognition.Pin{cognition.DataPin("s", "S", "string", "")}, Out: []cognition.Pin{cognition.DataPin("b", "B", "bool", "")}},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			s, _ := cognition.Get[string](in, "s")
			return cognition.Values{"b": s == "go"}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.fail"}, Title: "Fail",
		Pins: cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, "")}, Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")}},
		Run: func(*cognition.Exec, cognition.Values) (cognition.Values, string, error) {
			return nil, "", errors.New("boom")
		},
	})
}

func edge(from, fromPin, to, toPin string) cognition.Edge {
	return cognition.Edge{From: cognition.PinRef{Node: from, Pin: fromPin}, To: cognition.PinRef{Node: to, Pin: toPin}}
}

func returns(typ string) json.RawMessage {
	return json.RawMessage(`{"outputs": [{"id": "out", "type": "` + typ + `"}]}`)
}

// linear: start → a → b → return, with a pure double node feeding both a and b.
func linear() cognition.Graph {
	return cognition.Graph{Name: "Linear", Kind: cognition.KindFunction, Version: 3,
		Nodes: []cognition.Node{
			{ID: "start", Type: "test.start"}, {ID: "double", Type: "test.double"},
			{ID: "a", Type: "test.append"}, {ID: "b", Type: "test.append"},
			{ID: "ret", Type: "flow.return", Config: returns("string")},
		},
		Edges: []cognition.Edge{
			edge("start", "then", "a", "then"), edge("a", "then", "b", "then"), edge("b", "then", "ret", "then"),
			edge("start", "n", "double", "n"), edge("double", "n", "a", "n"), edge("double", "n", "b", "n"),
			edge("start", "s", "a", "s"), edge("a", "s", "b", "s"), edge("b", "s", "ret", "out"),
		},
	}
}

func newEngine(t *testing.T, graphs map[string]cognition.Graph) (*Engine, *toy, *gorm.DB) {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, cognition.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	tt := &toy{}
	tt.contribute(reg)
	for id, g := range graphs {
		cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: id}, Graph: g})
	}
	return New(gdb, reg, nil), tt, gdb
}

func args() features.ServiceArgs {
	a := features.BackgroundArgs(context.Background())
	a.Principal = features.LocalPrincipal()
	a.Meta.ConversationID = 7
	return a
}

func TestLinearRunPullsPureNodesOnceAndRecordsTheTrace(t *testing.T) {
	e, tt, gdb := newEngine(t, map[string]cognition.Graph{"linear": linear()})
	res, err := e.Run(RunArgs{ServiceArgs: args(), GraphID: "linear", Event: "test.start", Inputs: cognition.Values{"n": 1, "s": "x"}, Trigger: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Outputs["out"]; got != "x!!@a!!@b" {
		t.Errorf("out = %v", got)
	}
	if tt.doubled != 1 {
		t.Errorf("the pure node ran %d times, want once per run", tt.doubled)
	}

	var run cognition.Run
	if err := gdb.First(&run, res.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != cognition.StatusOK || run.GraphVersion != 3 || run.Event != "start" || run.ConversationID != 7 || run.Steps != 5 {
		t.Errorf("run = %+v", run)
	}
	var steps []cognition.Step
	gdb.Where("run_id = ?", run.ID).Order("seq").Find(&steps)
	var order []string
	for _, s := range steps {
		order = append(order, s.NodeID+":"+s.ExecPin)
	}
	if got := strings.Join(order, " "); got != "start:then double: a:then b:then ret:" {
		t.Errorf("steps = %s", got)
	}
	if !strings.Contains(steps[1].Outputs, `"big":"1152921504606846976"`) {
		t.Errorf("a uint64 beyond 2^53 is not captured as a string: %s", steps[1].Outputs)
	}
}

func TestBranchFollowsTheCondition(t *testing.T) {
	g := cognition.Graph{
		Nodes: []cognition.Node{
			{ID: "start", Type: "test.start"}, {ID: "cond", Type: "test.flag"}, {ID: "if", Type: "flow.branch"},
			{ID: "yes", Type: "test.append"}, {ID: "ret", Type: "flow.return", Config: returns("string")},
		},
		Edges: []cognition.Edge{
			edge("start", "then", "if", "then"), edge("if", "true", "yes", "then"), edge("yes", "then", "ret", "then"),
			edge("start", "s", "cond", "s"), edge("cond", "b", "if", "condition"),
			edge("start", "s", "yes", "s"), edge("yes", "s", "ret", "out"),
		},
	}
	e, _, _ := newEngine(t, map[string]cognition.Graph{"g": g})
	res, err := e.Run(RunArgs{ServiceArgs: args(), GraphID: "g", Event: "test.start", Inputs: cognition.Values{"s": "go"}})
	if err != nil || res.Outputs["out"] != "go@yes" {
		t.Errorf("true: %v, %v", res.Outputs, err)
	}
	res, err = e.Run(RunArgs{ServiceArgs: args(), GraphID: "g", Event: "test.start", Inputs: cognition.Values{"s": "stop"}})
	if err != nil || len(res.Outputs) != 0 {
		t.Errorf("false ends the run with no outputs: %v, %v", res.Outputs, err)
	}
}

func TestFailuresAreRecordedOnTheirNode(t *testing.T) {
	g := linear()
	g.Nodes = append(g.Nodes, cognition.Node{ID: "boom", Type: "test.fail"})
	g.Edges[1] = edge("a", "then", "boom", "then")
	g.Edges = append(g.Edges, edge("boom", "then", "b", "then"))
	e, _, gdb := newEngine(t, map[string]cognition.Graph{"g": g})
	res, err := e.Run(RunArgs{ServiceArgs: args(), GraphID: "g", Event: "test.start", Inputs: cognition.Values{"n": 0, "s": ""}})
	if err == nil || !strings.Contains(err.Error(), "Fail failed: boom") {
		t.Fatalf("err = %v", err)
	}
	var run cognition.Run
	gdb.First(&run, res.RunID)
	if run.Status != cognition.StatusFailed || run.FailedNode != "boom" || !strings.Contains(run.Error, "boom") {
		t.Errorf("run = %+v", run)
	}
	var step cognition.Step
	gdb.Where("run_id = ? AND node_id = ?", run.ID, "boom").First(&step)
	if step.Error == "" {
		t.Error("the failed step has no error")
	}
}

func TestStepLimit(t *testing.T) {
	e, _, gdb := newEngine(t, map[string]cognition.Graph{"linear": linear()})
	e.config = sections{`{"maxSteps": 3}`}
	_, err := e.Run(RunArgs{ServiceArgs: args(), GraphID: "linear", Event: "test.start", Inputs: cognition.Values{"n": 1}})
	if err == nil || !strings.Contains(err.Error(), "more than 3 nodes") {
		t.Fatalf("err = %v", err)
	}
	var run cognition.Run
	gdb.Last(&run)
	if run.FailedNode != "b" {
		t.Errorf("failed on %q, want b", run.FailedNode)
	}
}

func TestUnknownGraphAndEvent(t *testing.T) {
	e, _, _ := newEngine(t, map[string]cognition.Graph{"linear": linear()})
	if _, err := e.Run(RunArgs{ServiceArgs: args(), GraphID: "nope", Event: "test.start"}); err == nil {
		t.Error("an unknown graph ran")
	}
	if _, err := e.Run(RunArgs{ServiceArgs: args(), GraphID: "linear", Event: "event.reply"}); err == nil {
		t.Error("an event the graph does not have ran")
	}
}

func TestPurgeKeepsTheLastRunsOrTheRecentOnes(t *testing.T) {
	e, _, gdb := newEngine(t, map[string]cognition.Graph{"linear": linear()})
	p := features.LocalPrincipal()
	old := time.Now().AddDate(0, 0, -30)
	for i := range 5 {
		run := cognition.Run{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), GraphID: "linear", StartedAt: old.Add(time.Duration(i) * time.Hour)}
		gdb.Create(&run)
		gdb.Create(&cognition.Step{Tenanted: run.Tenanted, RunID: run.ID, Seq: 1})
	}
	recent := cognition.Run{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), GraphID: "other", StartedAt: time.Now()}
	gdb.Create(&recent)
	if err := e.Purge(context.Background(), 2, 7); err != nil {
		t.Fatal(err)
	}
	var runs, steps int64
	gdb.Model(&cognition.Run{}).Count(&runs)
	gdb.Model(&cognition.Step{}).Count(&steps)
	if runs != 3 || steps != 2 {
		t.Errorf("kept %d runs and %d steps, want the last 2 old runs with their steps and the recent one", runs, steps)
	}
}

type sections struct{ cognition string }

func (s sections) Resolved(features.ServiceArgs, string) (json.RawMessage, error) {
	return json.RawMessage(s.cognition), nil
}
