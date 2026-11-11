package connectors

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Problem is one way metadata fails the check, at a path such as
// operations.fetch.args.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (p Problem) String() string { return p.Path + ": " + p.Message }

// MaxToolName is the longest a tool name may be: Foundation's tools, MCP
// and the function calling of Gemini, Claude and OpenAI all accept it.
const MaxToolName = 64

// ids are dotted words of letters, digits and -, never _ (subject tokens
// map . to _).
var idPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)*$`)

// ValidID reports whether id may name a contract, type, operation or event.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ToolName is an operation's tool name, <contract>.<operation>.
func ToolName(contract, op string) string { return contract + "." + op }

// WireName is a tool name as models and MCP see it: . as _.
func WireName(name string) string { return strings.ReplaceAll(name, ".", "_") }

type checker struct{ out []Problem }

func (c *checker) add(path, format string, args ...any) {
	c.out = append(c.out, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (c *checker) meta(path string, m Meta) {
	if strings.TrimSpace(m.Title) == "" {
		c.add(path+".title", "needs a title")
	}
	if strings.TrimSpace(m.Description) == "" {
		c.add(path+".description", "needs a description for a model")
	}
	if len(m.Tags) == 0 {
		c.add(path+".tags", "needs tags")
	}
}

// CheckContract checks a contract's metadata: titles, descriptions and
// examples everywhere, and every operation in the tool shape (a name of at
// most 64 characters, object args and result in the schema subset
// tools.CheckArgs checks), events with payloads in the same subset, ids
// and correlations that name existing fields.
func CheckContract(ct Contract) []Problem {
	c := &checker{}
	if !ValidID(ct.ID) {
		c.add("id", "%q is not a contract id: dotted words of letters, digits and -, never _", ct.ID)
	}
	if ct.Version < 1 {
		c.add("version", "needs a version from 1")
	}
	c.meta("meta", ct.Meta)
	if len(ct.Operations) == 0 && len(ct.Events) == 0 {
		c.add("operations", "a contract needs an operation or an event")
	}
	ops := map[string]bool{}
	events := map[string]EventDef{}
	for _, e := range ct.Events {
		events[e.ID] = e
	}
	for _, op := range ct.Operations {
		path := "operations." + op.ID
		if !ValidID(op.ID) {
			c.add(path+".id", "%q is not an operation id: letters, digits, . and -, never _", op.ID)
		}
		if ops[op.ID] {
			c.add(path, "is declared twice")
		}
		ops[op.ID] = true
		if n := len(ToolName(ct.ID, op.ID)); n > MaxToolName {
			c.add(path+".id", "%s.%s is %d characters, more than a tool name's %d", ct.ID, op.ID, n, MaxToolName)
		}
		if strings.TrimSpace(op.Title) == "" {
			c.add(path+".title", "needs a title")
		}
		if strings.TrimSpace(op.Description) == "" {
			c.add(path+".description", "needs a description for a model")
		}
		if !slices.Contains([]string{EffectRead, EffectWrite, EffectSend}, op.Effect) {
			c.add(path+".effect", "must be read, write or send, not %q", op.Effect)
		}
		args := c.schema(path+".args", op.Args, true)
		result := c.schema(path+".result", op.Result, true)
		if op.Key != "" && args != nil {
			if _, ok := args.Properties[op.Key]; !ok {
				c.add(path+".key", "names %q, which args has no field for", op.Key)
			}
		}
		for _, code := range op.Errors {
			if !ValidCode(code) {
				c.add(path+".errors", "%q is not an error code: use one of %s", code, strings.Join(Codes, ", "))
			}
		}
		if len(op.Examples) == 0 {
			c.add(path+".examples", "needs an example, args and their result")
		}
		for i, ex := range op.Examples {
			if err := tools.CheckArgs(op.Args, ex.Args); err != nil {
				c.add(fmt.Sprintf("%s.examples[%d].args", path, i), "does not fit args: %v", err)
			}
			if err := tools.CheckArgs(op.Result, ex.Result); err != nil {
				c.add(fmt.Sprintf("%s.examples[%d].result", path, i), "does not fit result: %v", err)
			}
		}
		for i, cb := range op.Callbacks {
			cp := fmt.Sprintf("%s.callbacks[%d]", path, i)
			e, ok := events[cb.Event]
			if !ok {
				c.add(cp+".event", "names %q, which the contract does not declare", cb.Event)
				continue
			}
			if cb.Correlate.Result == "" || cb.Correlate.Event == "" {
				c.add(cp+".correlate", "needs the result's field and the event's field that hold the same key")
				continue
			}
			if result != nil && !result.has(cb.Correlate.Result) {
				c.add(cp+".correlate.result", "names %q, which the operation's result has no field for", cb.Correlate.Result)
			}
			if p, err := parseSchema(e.Payload); err == nil && !p.has(cb.Correlate.Event) {
				c.add(cp+".correlate.event", "names %q, which %s's payload has no field for", cb.Correlate.Event, e.ID)
			}
		}
	}
	seen := map[string]bool{}
	for _, e := range ct.Events {
		path := "events." + e.ID
		if !ValidID(e.ID) {
			c.add(path+".id", "%q is not an event id", e.ID)
		}
		if seen[e.ID] {
			c.add(path, "is declared twice")
		}
		seen[e.ID] = true
		if strings.TrimSpace(e.Title) == "" {
			c.add(path+".title", "needs a title")
		}
		if strings.TrimSpace(e.Description) == "" {
			c.add(path+".description", "needs a description for a model")
		}
		payload := c.schema(path+".payload", e.Payload, true)
		if len(e.Example) == 0 {
			c.add(path+".example", "needs an example payload")
		} else if err := tools.CheckArgs(e.Payload, e.Example); err != nil {
			c.add(path+".example", "does not fit the payload: %v", err)
		}
		if len(e.IDFields) == 0 {
			c.add(path+".id", "needs the payload fields that make an event unique")
		}
		for _, f := range e.IDFields {
			if payload != nil && !payload.has(f) {
				c.add(path+".id", "names %q, which the payload has no field for", f)
			}
		}
		if len(e.Delivery) == 0 {
			c.add(path+".delivery", "needs watch, callback or both")
		}
		for _, d := range e.Delivery {
			if d != DeliveryWatch && d != DeliveryCallback {
				c.add(path+".delivery", "%q is not watch or callback", d)
			}
		}
	}
	return c.out
}

// CheckType checks a type's metadata; contract, when not nil, finds the
// contracts it names, which must exist.
func CheckType(t Type, contract func(ref string) (Contract, bool)) []Problem {
	c := &checker{}
	if !ValidID(t.ID) || t.ID == "surface" {
		c.add("id", "%q is not a type id: dotted words of letters, digits and -, never _", t.ID)
	}
	c.meta("meta", t.Meta)
	if t.Source != SourceBuiltin && t.Source != SourceScripted {
		c.add("source", "must be builtin or scripted, not %q", t.Source)
	}
	if len(t.Contracts) == 0 && !t.Discovers {
		c.add("contracts", "names no contract, and does not discover one")
	}
	ids := map[string]bool{}
	for i, ref := range t.Contracts {
		path := fmt.Sprintf("contracts[%d]", i)
		id, v, err := ParseRef(ref)
		if err != nil || v == 0 {
			c.add(path, "%q must name a contract and its version, such as mail.mailbox@1", ref)
			continue
		}
		if ids[id] {
			c.add(path, "implements two versions of %s: a type implements one version of each contract", id)
		}
		ids[id] = true
		if contract != nil {
			if _, ok := contract(ref); !ok {
				c.add(path, "names %s, which is not a known contract", ref)
			}
		}
	}
	if len(t.Settings) > 0 {
		if _, err := config.ParseSchema(t.Settings); err != nil {
			c.add("settings", "is not a settings schema: %v", err)
		}
	}
	if t.Receives != "" && !slices.Contains(Receivers, t.Receives) {
		c.add("receives", "%q is not a callback scheme: use one of %s", t.Receives, strings.Join(Receivers, ", "))
	}
	return c.out
}

// CheckSurface checks a surface's metadata.
func CheckSurface(s SurfaceInfo) []Problem {
	c := &checker{}
	if !ValidID(s.ID) {
		c.add("id", "%q is not a surface id", s.ID)
	}
	c.meta("meta", s.Meta)
	return c.out
}

// schemaNode is the part of a JSON Schema the tool shape allows.
type schemaNode struct {
	Type       any                    `json:"type"`
	Properties map[string]*schemaNode `json:"properties"`
	Items      *schemaNode            `json:"items"`
	raw        map[string]json.RawMessage
}

func parseSchema(raw json.RawMessage) (*schemaNode, error) {
	var n schemaNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &n.raw)
	return &n, nil
}

// has reports whether a dotted path names a field of the schema.
func (n *schemaNode) has(path string) bool {
	cur := n
	for _, part := range strings.Split(path, ".") {
		if cur == nil || cur.Properties == nil {
			return false
		}
		next, ok := cur.Properties[part]
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

// forbidden are keywords outside the tool subset, anywhere in a schema.
var forbidden = []string{"$ref", "$defs", "definitions", "patternProperties", "$dynamicRef"}

// forbiddenTop are keywords not allowed at a schema's top level.
var forbiddenTop = []string{"oneOf", "anyOf", "allOf", "not"}

// schema checks one args, result or payload schema: an object at the top,
// in the subset every tool consumer accepts, its top-level fields
// described.
func (c *checker) schema(path string, raw json.RawMessage, object bool) *schemaNode {
	if len(raw) == 0 {
		c.add(path, "needs a JSON Schema")
		return nil
	}
	n, err := parseSchema(raw)
	if err != nil {
		c.add(path, "is not a JSON Schema: %v", err)
		return nil
	}
	if object && n.Type != "object" {
		c.add(path+".type", "must be object at the top, so it can be a tool's input or MCP structuredContent")
	}
	for _, k := range forbiddenTop {
		if _, ok := n.raw[k]; ok {
			c.add(path+"."+k, "is not allowed at the top of a tool schema")
		}
	}
	var walk func(p string, raw json.RawMessage)
	walk = func(p string, raw json.RawMessage) {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		for _, k := range forbidden {
			if _, ok := m[k]; ok {
				c.add(p+"."+k, "is outside the tool schema subset: inline it")
			}
		}
		if props, ok := m["properties"]; ok {
			var ps map[string]json.RawMessage
			_ = json.Unmarshal(props, &ps)
			for k, v := range ps {
				walk(p+".properties."+k, v)
			}
		}
		if items, ok := m["items"]; ok {
			walk(p+".items", items)
		}
	}
	walk(path, raw)
	var props map[string]json.RawMessage
	_ = json.Unmarshal(n.raw["properties"], &props)
	for k := range n.Properties {
		var d struct{ Description string }
		_ = json.Unmarshal(props[k], &d)
		if strings.TrimSpace(d.Description) == "" {
			c.add(path+".properties."+k, "needs a description")
		}
	}
	return n
}
