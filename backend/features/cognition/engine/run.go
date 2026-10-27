package engine

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/settings"
)

// run is one run's state.
type run struct {
	engine   *Engine
	args     RunArgs
	res      cognition.Resolved
	settings cognitionsettings.Cognition
	record   cognition.Run

	next    map[cognition.PinRef]string           // exec output → the node it leads to
	source  map[cognition.PinRef]cognition.PinRef // data input → the output feeding it
	values  map[string]cognition.Values           // each run node's outputs
	done    map[string]bool                       // exec nodes run, pure nodes evaluated
	pulling map[string]bool                       // pure nodes being evaluated
	steps   []cognition.Step
	result  cognition.Values
}

// from follows exec flow from the event node until a node leaves by an exec
// pin that leads nowhere.
func (r *run) from(event string) error {
	r.values[event], r.done[event] = r.args.Inputs, true
	out := ""
	for _, p := range r.res.Pins[event].Out {
		if p.Exec {
			out = p.ID
			break
		}
	}
	r.trace(event, nil, r.args.Inputs, out, time.Now(), nil)
	cur, pin := event, out
	for pin != "" {
		node, ok := r.next[cognition.PinRef{Node: cur, Pin: pin}]
		if !ok {
			return nil
		}
		in, err := r.inputs(node)
		if err != nil {
			return err
		}
		if pin, err = r.call(node, in); err != nil {
			return err
		}
		cur = node
	}
	return nil
}

// inputs gathers a node's data inputs: from pure nodes, evaluated now if
// they have not been, or from exec nodes that have already run.
func (r *run) inputs(id string) (cognition.Values, error) {
	in := cognition.Values{}
	for _, pin := range r.res.Pins[id].In {
		if pin.Exec {
			continue
		}
		src, ok := r.source[cognition.PinRef{Node: id, Pin: pin.ID}]
		if !ok {
			continue
		}
		if r.res.Pins[src.Node].Pure() {
			if err := r.evaluate(src.Node); err != nil {
				return nil, err
			}
		} else if !r.done[src.Node] {
			return nil, r.fail(id, log.Code("input_not_ready").With("nodeId", id, "pin", pin.ID).
				Errorf("%s has not run yet, so the input %s has no value", r.title(src.Node), pin.Name))
		}
		in[pin.ID] = r.values[src.Node][src.Pin]
	}
	return in, nil
}

// evaluate runs a pure node once per run.
func (r *run) evaluate(id string) error {
	if r.done[id] {
		return nil
	}
	if r.pulling[id] {
		return r.fail(id, log.Code("pure_cycle").With("nodeId", id).Errorf("%s pulls its own output", r.title(id)))
	}
	r.pulling[id] = true
	defer delete(r.pulling, id)
	in, err := r.inputs(id)
	if err != nil {
		return err
	}
	_, err = r.call(id, in)
	return err
}

// call runs one node, within the run's limits, and records it.
func (r *run) call(id string, in cognition.Values) (string, error) {
	if err := r.args.Context().Err(); err != nil {
		return "", r.fail(id, log.Code("run_time_limit").With("nodeId", id).Errorf("the run took longer than %d seconds", r.settings.MaxWallSeconds))
	}
	if len(r.steps) >= r.settings.MaxSteps {
		return "", r.fail(id, log.Code("run_step_limit").With("nodeId", id).Errorf("the run executed more than %d nodes", r.settings.MaxSteps))
	}
	node, _ := r.res.Graph.NodeByID(id)
	t := r.res.Types[id]
	start := time.Now()
	var out cognition.Values
	var pin string
	var err error
	if t.Run != nil {
		out, pin, err = t.Run(cognition.NewExec(r.args.ServiceArgs, r.record.ID, node, r.result), in)
	}
	if err == nil && r.args.Context().Err() != nil {
		err = log.Code("run_time_limit").Errorf("the run took longer than %d seconds", r.settings.MaxWallSeconds)
	}
	r.trace(id, in, out, pin, start, err)
	if err != nil {
		return "", r.fail(id, log.With("nodeId", id).Wrapf(err, "%s failed", r.title(id)))
	}
	r.values[id], r.done[id] = out, true
	return pin, nil
}

// fail marks the run failed on a node, once, and returns err.
func (r *run) fail(id string, err error) error {
	if r.record.FailedNode == "" {
		r.record.FailedNode = id
	}
	return err
}

// finish completes the run's record.
func (r *run) finish(err error) {
	r.record.DurationMs = time.Since(r.record.StartedAt).Milliseconds()
	r.record.Steps = len(r.steps)
	r.record.Status = cognition.StatusOK
	if err != nil {
		r.record.Status, r.record.Error = cognition.StatusFailed, err.Error()
	}
	if len(r.result) > 0 {
		r.record.Outputs = Capture(r.result, r.settings.MaxValueKB<<10)
	}
}

// trace records a node's execution.
func (r *run) trace(id string, in, out cognition.Values, pin string, start time.Time, err error) {
	limit := r.settings.MaxValueKB << 10
	step := cognition.Step{
		Tenanted: r.record.Tenanted, RunID: r.record.ID, Seq: len(r.steps) + 1, NodeID: id, NodeType: r.res.Types[id].ID,
		Inputs: Capture(in, limit), Outputs: Capture(out, limit), ExecPin: pin, StartedAt: start, DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		step.Error = err.Error()
	}
	r.steps = append(r.steps, step)
}

// title is a node's title for messages.
func (r *run) title(id string) string {
	if n, ok := r.res.Graph.NodeByID(id); ok && n.Title != "" {
		return n.Title
	}
	if t, ok := r.res.Types[id]; ok {
		return t.Title
	}
	return id
}

// Capture is values as JSON for the trace, one object by pin: each value
// is cut to a preview when it is larger than limit bytes, and integers too
// large for a JavaScript number become strings, so IDs survive the browser.
func Capture(values map[string]any, limit int) string {
	if len(values) == 0 {
		return ""
	}
	out := make(map[string]json.RawMessage, len(values))
	for k, v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			b, _ = json.Marshal(map[string]string{"$error": "cannot capture: " + err.Error()})
		} else if len(b) > limit {
			b, _ = json.Marshal(map[string]any{"$truncated": len(b), "preview": string(b[:min(len(b), 4096)])})
		} else {
			b = safeInts(b)
		}
		out[k] = b
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// safeInts turns the integers of a JSON document that a float64 cannot hold
// exactly into strings.
func safeInts(b []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return b
	}
	changed := false
	var walk func(v any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x)
			}
		case json.Number:
			if n, err := t.Int64(); err == nil {
				if n > 1<<53 || n < -(1<<53) {
					changed = true
					return t.String()
				}
			} else if _, err := strconv.ParseUint(t.String(), 10, 64); err == nil {
				changed = true
				return t.String()
			}
		}
		return v
	}
	v = walk(v)
	if !changed {
		return b
	}
	out, err := json.Marshal(v)
	if err != nil {
		return b
	}
	return out
}
