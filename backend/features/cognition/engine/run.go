package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	values  map[string]cognition.Values           // outputs, by memo key (memo)
	done    map[string]bool                       // exec nodes run, pure nodes evaluated, by memo key
	pulling map[string]bool                       // pure nodes being evaluated, by memo key
	ctx     string                                // the execution context: loop frames, "forEach:n5[2]/loop:n9[1]"
	vars    cognition.Values
	steps   []cognition.Step
	result  cognition.Values
	stubbed []string // External nodes run as stubs (RunArgs.Stub)
}

// memo is a node's key for its outputs: an exec node keeps its latest
// outputs, a pure node one per execution context.
func (r *run) memo(id string) string {
	if r.res.Pins[id].Pure() && r.ctx != "" {
		return r.ctx + "|" + id
	}
	return id
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
	return r.flow(event, out)
}

// flow follows the exec chain leaving node cur by pin to its end.
func (r *run) flow(cur, pin string) error {
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
		v, ok, err := r.input(id, pin)
		if err != nil {
			return nil, err
		}
		if ok {
			in[pin.ID] = v
		}
	}
	return in, nil
}

// input is one data input's value, converted when its edge converts; ok is
// false when nothing feeds it.
func (r *run) input(id string, pin cognition.Pin) (any, bool, error) {
	ref := cognition.PinRef{Node: id, Pin: pin.ID}
	src, ok := r.source[ref]
	if !ok {
		return nil, false, nil
	}
	if r.res.Pins[src.Node].Pure() {
		if err := r.evaluate(src.Node); err != nil {
			return nil, false, err
		}
	} else if !r.done[src.Node] {
		return nil, false, r.fail(id, log.Code("input_not_ready").With("nodeId", id, "pin", pin.ID).
			Errorf("%s has not run yet, so the input %s has no value", r.title(src.Node), pin.Name))
	}
	v := r.values[r.memo(src.Node)][src.Pin]
	if conv, ok := r.res.Conversions[ref]; ok && v != nil {
		out, err := conv.Convert(v)
		if err != nil {
			return nil, false, r.fail(id, log.Code("conversion_failed").With("nodeId", id, "pin", pin.ID).
				Wrapf(err, "cannot convert %s to %s for %s", conv.From, conv.To, pin.Name))
		}
		v = out
	}
	return v, true, nil
}

// evaluate runs a pure node once per execution context, or every time it
// is pulled when it is volatile.
func (r *run) evaluate(id string) error {
	key := r.memo(id)
	if r.done[key] && !r.res.Types[id].Volatile {
		return nil
	}
	if r.pulling[key] {
		return r.fail(id, log.Code("pure_cycle").With("nodeId", id).Errorf("%s pulls its own output", r.title(id)))
	}
	r.pulling[key] = true
	defer delete(r.pulling, key)
	in, err := r.inputs(id)
	if err != nil {
		return err
	}
	_, err = r.call(id, in)
	return err
}

// exec is the Exec a node runs with, its hooks reaching back into the run.
func (r *run) exec(id string, node cognition.Node) *cognition.Exec {
	x := cognition.NewExec(r.args.ServiceArgs, r.record.ID, node, r.result)
	x.Host, x.Vars, x.MaxIterations = r.args.Host, r.vars, r.settings.MaxLoopIterations
	x.GraphID, x.Owner = r.res.Graph.ID, r.res.Graph.Owner
	x.Hooks = cognition.ExecHooks{
		Flow: func(pin, frame string) error {
			outer := r.ctx
			if frame != "" {
				if r.ctx == "" {
					r.ctx = frame
				} else {
					r.ctx += "/" + frame
				}
			}
			defer func() { r.ctx = outer }()
			return r.flow(id, pin)
		},
		Set: func(out cognition.Values) {
			key := r.memo(id)
			v := cognition.Values{}
			for k, val := range r.values[key] {
				v[k] = val
			}
			for k, val := range out {
				v[k] = val
			}
			r.values[key], r.done[key] = v, true
		},
		Pull: func(pin, frame string) (any, error) {
			p, ok := r.res.Pins[id].Find(pin, true)
			if !ok {
				return nil, fmt.Errorf("%s has no input %s", r.title(id), pin)
			}
			outer := r.ctx
			if frame != "" {
				r.ctx = strings.TrimPrefix(outer+"/"+frame, "/")
			}
			defer func() { r.ctx = outer }()
			v, _, err := r.input(id, p)
			return v, err
		},
		Call: func(graphID string, inputs cognition.Values) (cognition.Values, error) {
			return r.engine.call(r, graphID, inputs)
		},
		Setting: func(section, path string) (any, error) {
			return r.engine.setting(r.args.ServiceArgs, section, path)
		},
	}
	return x
}

// call runs one node, within the run's limits, and records it.
func (r *run) call(id string, in cognition.Values) (string, error) {
	if err := r.args.Context().Err(); err != nil {
		return "", r.fail(id, log.Code("run_time_limit").With("nodeId", id).Errorf("the run took longer than its time limit"))
	}
	if len(r.steps) >= r.settings.MaxSteps {
		return "", r.fail(id, log.Code("run_step_limit").With("nodeId", id).Errorf("the run executed more than %d nodes", r.settings.MaxSteps))
	}
	node, _ := r.res.Graph.NodeByID(id)
	t := r.res.Types[id]
	start := time.Now()
	ctx := r.ctx
	var out cognition.Values
	var pin string
	var err error
	switch {
	case r.args.Stub && t.External:
		out, pin = r.stub(id, t)
	case t.Run != nil:
		out, pin, err = t.Run(r.exec(id, node), in)
	}
	if err == nil && r.args.Context().Err() != nil {
		err = log.Code("run_time_limit").Errorf("the run took longer than its time limit")
	}
	r.traceIn(ctx, id, in, out, pin, start, err)
	if err != nil {
		return "", r.fail(id, log.With("nodeId", id).Wrapf(err, "%s failed", r.title(id)))
	}
	key := r.memo(id)
	if prior := r.values[key]; prior != nil && out != nil {
		merged := cognition.Values{}
		for k, v := range prior {
			merged[k] = v
		}
		for k, v := range out {
			merged[k] = v
		}
		out = merged
	} else if out == nil {
		out = r.values[key]
	}
	r.values[key], r.done[key] = out, true
	return pin, nil
}

// stub is what an External node gives in a sandboxed run: its Sample, by
// output pin, leaving by its first exec output.
func (r *run) stub(id string, t cognition.NodeType) (cognition.Values, string) {
	r.stubbed = append(r.stubbed, id)
	sample := t.Sample
	if t.SampleFor != nil {
		if n, ok := r.res.Graph.NodeByID(id); ok {
			sample = t.SampleFor(n.Config)
		}
	}
	out, pin := cognition.Values{}, ""
	for _, p := range r.res.Pins[id].Out {
		switch {
		case p.Exec && pin == "":
			pin = p.ID
		case !p.Exec:
			out[p.ID] = sample[p.ID]
		}
	}
	return out, pin
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

// trace records a node's execution in the current execution context.
func (r *run) trace(id string, in, out cognition.Values, pin string, start time.Time, err error) {
	r.traceIn(r.ctx, id, in, out, pin, start, err)
}

// traceIn records a node's execution in the execution context ctx.
func (r *run) traceIn(ctx, id string, in, out cognition.Values, pin string, start time.Time, err error) {
	limit := r.settings.MaxValueKB << 10
	step := cognition.Step{
		Tenanted: r.record.Tenanted, RunID: r.record.ID, Seq: len(r.steps) + 1, NodeID: id, NodeType: r.res.Types[id].ID, Context: ctx,
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
