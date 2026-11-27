// Package infranodes holds the infrastructure blueprint nodes (forge 22e):
// each ensures one thing by its resource id, so running it twice changes
// nothing and running it after a crash continues. Hosts are made through
// infra.compute; everything else is the Control Plane's desired state, as
// records of the infrastructure app whose ids derive from resource ids,
// which the daemons reconcile toward.
package infranodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

const source = "infra"

// Deps are the nodes' run-time dependencies.
type Deps struct {
	Bus     infra.Executor
	Data    data.Deps
	Compute infra.Compute
}

// RecordID is the desired-state record id of a resource id: its words
// joined by _, which no configuration id holds, so ids never collide.
func RecordID(resource string) string {
	r := strings.NewReplacer(":", "_", "/", "_", ".", "_")
	id := r.Replace(resource)
	if len(id) > 120 {
		sum := sha256.Sum256([]byte(resource))
		id = id[:100] + "_" + hex.EncodeToString(sum[:8])
	}
	return id
}

// kinds the nodes run in: functions, server actions and triggers (and the
// jobs 22f enqueues, which run function graphs). Never commands: their
// effects are outside the decision.
var kinds = []string{cognition.KindFunction, livenodes.KindAction, livenodes.KindTrigger}

// settle waits until the records projection has folded every committed
// event, so a read after a write sees it.
func (d Deps) settle(args features.ServiceArgs) {
	if s, ok := any(d.Data.Projections).(interface {
		Settle(ctx context.Context, tenant uint64, projector string) error
	}); ok {
		_ = s.Settle(args.Context(), args.Principal.TenantID, data.Projector)
	}
}

// Ensure makes a record hold values, creating it under id when missing;
// it changes only what differs, so a second run sends nothing. key makes
// the commands' ids, so a retried step is dropped by Nats-Msg-Id.
func (d Deps) Ensure(args features.ServiceArgs, entity, id string, values map[string]any, key string) (bool, error) {
	if d.Bus == nil || d.Data.Projections == nil {
		return false, fmt.Errorf("the infrastructure nodes are not available")
	}
	d.settle(args)
	cur, _, err := data.Get(args, d.Data, entity, id)
	if err != nil {
		return false, err
	}
	if cur == nil {
		raw, _ := json.Marshal(map[string]any{"entity": entity, "values": values})
		r := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: key + "-create", Type: "record.create", Target: id, Args: raw, Actor: "infra"})
		if r.Common.Code < 300 {
			return true, nil
		}
		if cur, _, err = data.Get(args, d.Data, entity, id); err != nil || cur == nil {
			return false, fmt.Errorf("%s %s: %s", entity, id, r.Common.Message)
		}
	}
	set := map[string]any{}
	for _, k := range sortedKeys(values) {
		want, _ := json.Marshal(values[k])
		if !sameJSON(cur[k], want) {
			set[k] = values[k]
		}
	}
	if len(set) == 0 {
		return false, nil
	}
	raw, _ := json.Marshal(map[string]any{"entity": entity, "set": set})
	sum := sha256.Sum256(raw)
	r := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: key + "-update-" + hex.EncodeToString(sum[:6]), Type: "record.update", Target: id, Args: raw, Actor: "infra"})
	if r.Common.Code >= 300 {
		return false, fmt.Errorf("%s %s: %s", entity, id, r.Common.Message)
	}
	return true, nil
}

// Remove deletes a record if it exists.
func (d Deps) Remove(args features.ServiceArgs, entity, id, key string) (bool, error) {
	d.settle(args)
	cur, _, err := data.Get(args, d.Data, entity, id)
	if err != nil || cur == nil {
		return false, err
	}
	raw, _ := json.Marshal(map[string]any{"entity": entity})
	r := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: key + "-delete", Type: "record.delete", Target: id, Args: raw, Actor: "infra"})
	if r.Common.Code >= 300 {
		return false, fmt.Errorf("%s %s: %s", entity, id, r.Common.Message)
	}
	return true, nil
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil {
		x = nil
	}
	if json.Unmarshal(b, &y) != nil {
		y = nil
	}
	ra, _ := json.Marshal(x)
	rb, _ := json.Marshal(y)
	return string(ra) == string(rb)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stepOf reads a plan step (or a resource) from a pin.
func stepOf(in cognition.Values, pin string) (infra.Step, error) {
	raw, _ := json.Marshal(in[pin])
	var s struct {
		infra.Step
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.ID == "" {
		return infra.Step{}, fmt.Errorf("%s is not a plan step: %s", pin, raw)
	}
	if s.Desired == nil {
		s.Desired = s.Props
	}
	if s.Desired == nil {
		s.Desired = s.Before
	}
	if s.Desired == nil {
		s.Desired = map[string]any{}
	}
	return s.Step, nil
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

const stepPin = "A plan step from infra.plan ({id, kind, site, account, desired}), or a resource ({id, kind, site, account, props})"

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Source, t.Order, t.Category = source, order, "Infrastructure"
	t.External, t.Effects = true, true
	t.Kinds = kinds
	t.Tags = append(t.Tags, "infrastructure")
	cognition.Nodes.Register(reg, t)
}
