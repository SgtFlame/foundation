package apps

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Scripted connectors (forge 07d): the graph kind and node types the
// manifest check reads. The kind and nodes are registered by
// connectors/scripted.
const (
	// KindConnectorOp is the kind of a scripted connector's operation and
	// watch graphs.
	KindConnectorOp = "connector.op"
	// NodeOperation is a connector.op graph's entry, Operation called.
	NodeOperation = "connector.operation"
	// NodeReturnError ends a connector.op graph with a typed error.
	NodeReturnError = "connector.returnError"
	// NodeReturn is the Return node, whose declared outputs are the result.
	NodeReturn = "flow.return"
)

// connectorName is a connector's name: one type-id word, letters, digits
// and -, never _ or . (the type id <appId>.<name> is a subject token).
var connectorName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// OperationConfig is Operation called's config: the contract and the
// operation it answers.
type OperationConfig struct {
	Contract  string `json:"contract"`
	Operation string `json:"operation"`
	// Watch marks the entry of a watch graph: the cursor in, no args.
	Watch bool `json:"watch,omitempty"`
}

// ConnectorType is a connector's metadata as the registry lists it.
func ConnectorType(app string, c Connector) connectors.Type {
	title := c.Title
	if title == "" {
		title = c.Name
	}
	return connectors.Type{ID: c.TypeID(app), Contracts: c.Contracts, Settings: c.Settings, Source: connectors.SourceScripted,
		Meta: connectors.Meta{Title: title, Description: c.Description, Icon: c.Icon, Tags: []string{"scripted", app}}}
}

// BuiltinContracts finds the registry's builtin contracts by reference,
// which scripted connectors implement (forge 07d question 1).
func BuiltinContracts(reg *extensions.Registry) func(ref string) (connectors.Contract, bool) {
	return func(ref string) (connectors.Contract, bool) {
		if reg == nil {
			return connectors.Contract{}, false
		}
		for _, c := range connectors.Contracts.All(reg) {
			if c.Contract.Ref() == ref {
				return c.Contract, true
			}
		}
		return connectors.Contract{}, false
	}
}

// Operations are the operations a connector's contracts declare, each
// with the first contract declaring it, in order.
func (c Connector) operationsOf(contract func(string) (connectors.Contract, bool)) ([]string, map[string]connectors.Contract) {
	var ids []string
	by := map[string]connectors.Contract{}
	for _, ref := range c.Contracts {
		ct, ok := contract(ref)
		if !ok {
			continue
		}
		for _, op := range ct.Operations {
			if _, seen := by[op.ID]; !seen {
				ids = append(ids, op.ID)
				by[op.ID] = ct
			}
		}
	}
	return ids, by
}

func (c *check) connectors() {
	seen := map[string]bool{}
	contract := c.env.Contract
	if contract == nil {
		contract = func(string) (connectors.Contract, bool) { return connectors.Contract{}, false }
	}
	for _, k := range c.m.Connectors {
		path := "connectors[" + k.Name + "]"
		switch {
		case !connectorName.MatchString(k.Name):
			c.add(path+".name", fmt.Sprintf("%q is not a connector name", k.Name), "use letters, digits and -, never _ or .")
		case seen[k.Name]:
			c.add(path+".name", fmt.Sprintf("the connector %s is declared twice", k.Name), "")
		case !connectors.ValidID(k.TypeID(c.m.ID)):
			c.add(path+".name", fmt.Sprintf("the type id %s cannot be a connector type's: the app's id holds _", k.TypeID(c.m.ID)), "make the app again with an id of letters, digits and -")
		}
		seen[k.Name] = true
		// The registry refuses a type that fails its own check
		for _, p := range connectors.CheckType(ConnectorType(c.m.ID, k), contract) {
			field := p.Path
			switch {
			case field == "id":
				continue // named above
			case field == "meta.tags":
				continue // made from the app
			case strings.HasPrefix(field, "meta."):
				field = strings.TrimPrefix(field, "meta.")
			}
			fix := ""
			switch field {
			case "description":
				fix = "describe what the connector does, for a model choosing a connection"
			case "title":
				fix = "give it a title people see in the Connection form"
			}
			if strings.HasPrefix(field, "contracts") {
				fix = "name a contract connectors.contracts lists, as id@version"
			}
			c.add(path+"."+field, p.Message, fix)
		}
		ops, by := k.operationsOf(contract)
		for _, op := range slices.Sorted(maps.Keys(k.Operations)) {
			at := path + ".operations." + op
			ct, ok := by[op]
			if !ok {
				c.add(at, fmt.Sprintf("no contract the connector names has an operation %q", op), "use one of "+strings.Join(ops, ", "))
				continue
			}
			c.opGraph(at, k.Operations[op], ct, op, false)
		}
		if k.Watch != "" {
			c.opGraph(path+".watch", k.Watch, connectors.Contract{}, "", true)
		}
	}
}

// opGraph checks a connector.op graph: the app's own, its entry naming the
// operation, and its Return nodes giving the result's fields.
func (c *check) opGraph(path, id string, ct connectors.Contract, op string, watch bool) {
	var g cognition.Graph
	ok := false
	if c.env.Graph != nil && live.ValidToken(id) {
		g, ok = c.env.Graph(id)
	}
	switch {
	case !ok:
		c.add(path, fmt.Sprintf("there is no graph %q", id), "make a connector.op graph with bp.create first, or let the Connectors tab make it")
		return
	case g.Kind != KindConnectorOp:
		c.add(path, fmt.Sprintf("the graph %s is a %s graph, not a connector.op graph", id, g.Kind), "name a connector.op graph")
		return
	case g.Owner != Owner(c.m.ID):
		c.add(path, fmt.Sprintf("the graph %s is not this app's own", id), "make it with owner app:"+c.m.ID)
		return
	}
	var entry *OperationConfig
	for _, n := range g.Nodes {
		if n.Type != NodeOperation {
			continue
		}
		var oc OperationConfig
		if len(n.Config) > 0 && json.Unmarshal(n.Config, &oc) != nil {
			continue
		}
		entry = &oc
	}
	if entry == nil {
		c.add(path, fmt.Sprintf("the graph %s has no Operation called node", id), "start it with Operation called")
		return
	}
	if watch {
		if !entry.Watch {
			c.add(path, fmt.Sprintf("the graph %s answers %s, not the event source", id, entry.Operation), "set its Operation called to watch")
		}
		return
	}
	if entry.Watch || entry.Operation != op {
		c.add(path, fmt.Sprintf("the graph %s answers %q, not %s", id, entry.Operation, op), "set its Operation called to "+op)
		return
	}
	if entry.Contract != ct.Ref() && !slices.Contains(c.connector(path).Contracts, entry.Contract) {
		c.add(path, fmt.Sprintf("the graph %s answers %s of %s, which the connector does not name", id, op, entry.Contract), "set its Operation called to "+ct.Ref())
		return
	}
	operation, _ := ct.Operation(op)
	var result struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	_ = json.Unmarshal(operation.Result, &result)
	for i, n := range g.Nodes {
		if n.Type != NodeReturn {
			continue
		}
		pins, err := cognition.DeclaredPins(n.Config)
		if err != nil {
			continue // the graph's own check names it
		}
		have := map[string]bool{}
		for _, p := range pins {
			have[p.ID] = true
			if _, ok := result.Properties[p.ID]; !ok {
				c.add(fmt.Sprintf("%s.nodes.%d", path, i), fmt.Sprintf("the graph %s returns %s, which the result of %s has not", id, p.ID, op),
					"return only the result's fields: "+strings.Join(slices.Sorted(maps.Keys(result.Properties)), ", "))
			}
		}
		for _, r := range result.Required {
			if !have[r] {
				c.add(fmt.Sprintf("%s.nodes.%d", path, i), fmt.Sprintf("the graph %s returns no %s, which the result of %s needs", id, r, op), "add the output "+r+" to its Return")
			}
		}
	}
}

// connector is the connector a path is under.
func (c *check) connector(path string) Connector {
	name, _, _ := strings.Cut(strings.TrimPrefix(path, "connectors["), "]")
	for _, k := range c.m.Connectors {
		if k.Name == name {
			return k
		}
	}
	return Connector{}
}

// ConnectorWarnings are what a manifest's connectors leave out without it
// stopping the app: operations of their contracts with no graph, which
// answer notSupported.
func ConnectorWarnings(m Manifest, contract func(ref string) (connectors.Contract, bool)) []live.Problem {
	out := []live.Problem{}
	if contract == nil {
		return out
	}
	for _, k := range m.Connectors {
		ops, by := k.operationsOf(contract)
		for _, op := range ops {
			if k.Operations[op] == "" {
				out = append(out, live.Problem{Code: "warning", Path: "manifest.connectors[" + k.Name + "].operations." + op,
					Message: fmt.Sprintf("the connector %s has no graph for %s of %s, so calling it answers notSupported", k.Name, op, by[op].Ref()),
					Fix:     fmt.Sprintf("add its graph with app.updateConnector {name: %q, operations: {%q: <graph>}}", k.Name, op)})
			}
		}
	}
	return out
}

// Connector is one of the version's connectors.
func (v Version) Connector(name string) (Connector, bool) {
	i := slices.IndexFunc(v.Manifest.Connectors, func(c Connector) bool { return c.Name == name })
	if i < 0 {
		return Connector{}, false
	}
	return v.Manifest.Connectors[i], true
}

// ConnectorGraph is the pinned graph of one of the version's connectors'
// operations ("" for none; ok false when it has no graph).
func (v Version) ConnectorGraph(name, op string) (cognition.Graph, bool) {
	c, ok := v.Connector(name)
	if !ok {
		return cognition.Graph{}, false
	}
	id := c.Operations[op]
	if id == "" {
		return cognition.Graph{}, false
	}
	g, ok := v.Graphs[id]
	return g, ok
}

// WatchGraph is the pinned watch graph of one of the version's connectors.
func (v Version) WatchGraph(name string) (cognition.Graph, bool) {
	c, ok := v.Connector(name)
	if !ok || c.Watch == "" {
		return cognition.Graph{}, false
	}
	g, ok := v.Graphs[c.Watch]
	return g, ok
}
