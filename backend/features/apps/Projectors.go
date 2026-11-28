package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// KindProjector is the graph kind of an app projector's fold (forge 26d-1).
const KindProjector = "projector"

// Projector is a projection the app declares (forge 26d-1, iteration 26
// question 4): its projector graph folds each event of its entity types'
// records into the value at the key its Key expression gives, kept current
// by the live core while the app is installed, in
// PRJ_<tenant>_app_<appId>_<name>_<generation>, and read by the app's
// queries with Read projection as app.<appId>.<name>.
type Projector struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Entities are the app's own entity types whose record events it folds.
	Entities []string `json:"entities"`
	// Key is an expression over event ({type, entity, id, data, user,
	// time}) and record (the record as projected at or after the event; nil
	// once deleted) giving the key the event folds into; "" skips the
	// event. The record's id when left out.
	Key string `json:"key,omitempty"`
	// Graph is the app's own projector graph: On fold gives state, event,
	// record and key, and its Return's value is the key's new value (null
	// deletes it).
	Graph string `json:"graph"`
}

// ProjectorID is the id an app's queries read a projector by:
// app.<appId>.<name>.
func ProjectorID(app, name string) string { return "app." + app + "." + name }

// ParseProjectorID splits an app projector's id into the app and the name.
func ParseProjectorID(id string) (app, name string, ok bool) {
	rest, ok := strings.CutPrefix(id, "app.")
	if !ok {
		return "", "", false
	}
	app, name, ok = strings.Cut(rest, ".")
	return app, name, ok && app != "" && name != "" && !strings.Contains(name, ".")
}

// Generation names a projector's fold as published: a new graph, key or
// set of entity types is a new generation, rebuilt from the stream into a
// bucket of its own while the old one is left behind.
func (p Projector) Generation(g cognition.Graph) string {
	raw, _ := json.Marshal(struct {
		Entities []string        `json:"entities"`
		Key      string          `json:"key"`
		Graph    cognition.Graph `json:"graph"`
	}{p.Entities, p.Key, g})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:10]
}

// ProjectorGraphs are the graphs a manifest's projectors fold with.
func (m Manifest) ProjectorGraphs() []string {
	var out []string
	for _, p := range m.Projectors {
		if p.Graph != "" && !slices.Contains(out, p.Graph) {
			out = append(out, p.Graph)
		}
	}
	return out
}

// Projector is one of the version's projectors with its pinned graph.
func (v Version) Projector(name string) (Projector, cognition.Graph, bool) {
	i := slices.IndexFunc(v.Manifest.Projectors, func(p Projector) bool { return p.Name == name })
	if i < 0 {
		return Projector{}, cognition.Graph{}, false
	}
	p := v.Manifest.Projectors[i]
	g, ok := v.Graphs[p.Graph]
	return p, g, ok
}

// KeyEnv is what a projector's key expression reads.
var KeyEnv = map[string]any{"event": map[string]any{}, "record": map[string]any{}}

// CompileKey compiles a projector's key expression.
func CompileKey(src string) (*vm.Program, error) {
	if src == "" {
		src = "event.id"
	}
	return expr.Compile(src, expr.Env(KeyEnv), expr.AllowUndefinedVariables())
}

func (c *check) projectors() {
	seen := map[string]bool{}
	for _, p := range c.m.Projectors {
		path := "projectors[" + p.Name + "]"
		switch {
		case !toolName.MatchString(p.Name):
			c.add(path+".name", fmt.Sprintf("%q is not a projector name", p.Name), "use one word of letters and digits, such as unread")
		case seen[p.Name]:
			c.add(path+".name", fmt.Sprintf("the projector %s is declared twice", p.Name), "")
		}
		seen[p.Name] = true
		if len(p.Entities) == 0 {
			c.add(path+".entities", "a projector folds the records of at least one entity type", "name one of the app's own entity types")
		}
		for i, e := range p.Entities {
			if !slices.Contains(c.m.Entities, e) {
				c.add(fmt.Sprintf("%s.entities[%d]", path, i), fmt.Sprintf("the app does not use the entity type %q", e), "add it with app.addEntity, or name one of the app's entity types")
			}
		}
		if _, err := CompileKey(p.Key); err != nil {
			c.add(path+".key", fmt.Sprintf("the key is not an expression: %v", err), "write an expression over event and record, such as record.channel")
		}
		var g cognition.Graph
		ok := false
		if c.env.Graph != nil && live.ValidToken(p.Graph) {
			g, ok = c.env.Graph(p.Graph)
		}
		switch {
		case !ok:
			c.add(path+".graph", fmt.Sprintf("there is no graph %q", p.Graph), "make a projector graph with bp.create first: On fold gives state and event, its Return's value is the new state")
		case g.Kind != KindProjector:
			c.add(path+".graph", fmt.Sprintf("the graph %s is a %s graph, not a projector graph", p.Graph, g.Kind), "name a projector graph")
		case g.Owner != Owner(c.m.ID):
			c.add(path+".graph", fmt.Sprintf("the graph %s is not this app's own", p.Graph), "make it with owner app:"+c.m.ID)
		}
	}
}

// Ephemeral is a topic of signals that are never stored (forge 26d-1):
// typing, a cursor. A signal is published on the core NATS subject
// eph.<tenant>.<appId>.<topic>.<key>, where key is a record of the entity
// type the actions name, and reaches only the subscribers that may Read
// that record; publishing needs Write on it.
type Ephemeral struct {
	Topic       string `json:"topic"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Read and Write are entity actions (entity:<id>.<action>) on one of
	// the app's own entity types; Write is Read when left out.
	Read  string `json:"read"`
	Write string `json:"write,omitempty"`
}

// Entity is the entity type whose records key the topic, and the actions
// reading and writing it need.
func (e Ephemeral) Entity() (entity, read, write string, ok bool) {
	entity, ra, ok := EntityAction(e.Read)
	if !ok || len(ra) != 1 {
		return "", "", "", false
	}
	read, write = ra[0], ra[0]
	if e.Write != "" {
		we, wa, ok := EntityAction(e.Write)
		if !ok || we != entity || len(wa) != 1 {
			return "", "", "", false
		}
		write = wa[0]
	}
	return entity, read, write, true
}

// EphemeralTopic is one of an install's ephemeral topics.
func (m Manifest) EphemeralTopic(topic string) (Ephemeral, bool) {
	i := slices.IndexFunc(m.Ephemeral, func(e Ephemeral) bool { return e.Topic == topic })
	if i < 0 {
		return Ephemeral{}, false
	}
	return m.Ephemeral[i], true
}

func (c *check) ephemeral() {
	seen := map[string]bool{}
	for _, e := range c.m.Ephemeral {
		path := "ephemeral[" + e.Topic + "]"
		switch {
		case !toolName.MatchString(e.Topic):
			c.add(path+".topic", fmt.Sprintf("%q is not a topic", e.Topic), "use one word of letters and digits, such as typing")
		case seen[e.Topic]:
			c.add(path+".topic", fmt.Sprintf("the topic %s is declared twice", e.Topic), "")
		}
		seen[e.Topic] = true
		entity, _, _, ok := e.Entity()
		switch {
		case !ok:
			c.add(path+".read", "read and write must be single entity actions on one entity type, such as entity:channel.read", "")
		case !slices.Contains(c.m.Entities, entity):
			c.add(path+".read", fmt.Sprintf("the app does not use the entity type %q", entity), "add it with app.addEntity")
		}
	}
}
