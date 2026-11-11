package connectors_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
)

// The test contract and types pass the metadata check.
func TestTestContractPasses(t *testing.T) {
	if p := connectors.CheckContract(connectorstest.Contract()); len(p) > 0 {
		t.Fatalf("test contract: %v", p)
	}
	lookup := func(ref string) (connectors.Contract, bool) {
		return connectorstest.Contract(), ref == connectorstest.ContractRef
	}
	for _, ty := range connectorstest.Types() {
		if p := connectors.CheckType(ty, lookup); len(p) > 0 {
			t.Errorf("%s: %v", ty.ID, p)
		}
	}
}

// has reports whether one of problems is at a path starting with path
// and says want.
func has(problems []connectors.Problem, path, want string) bool {
	for _, p := range problems {
		if strings.HasPrefix(p.Path, path) && strings.Contains(p.Message, want) {
			return true
		}
	}
	return false
}

// The check refuses what a tool cannot be: a name past 64 characters, a
// $ref, args that are not an object, an undescribed operation or field, a
// callback correlating a field neither side has, and an id with _.
func TestCheckRefusesBadMetadata(t *testing.T) {
	cases := []struct {
		name, path, want string
		edit             func(c *connectors.Contract)
	}{
		{"long name", "operations.", "more than a tool name's 64", func(c *connectors.Contract) {
			c.Operations[0].ID = strings.Repeat("a", 70)
		}},
		{"ref", "operations.echo.args", "outside the tool schema subset", func(c *connectors.Contract) {
			c.Operations[0].Args = json.RawMessage(`{"type":"object","properties":{"text":{"$ref":"#/$defs/t","description":"x"}}}`)
			c.Operations[0].Examples = nil
		}},
		{"not object", "operations.echo.args.type", "must be object", func(c *connectors.Contract) {
			c.Operations[0].Args = json.RawMessage(`{"type":"string"}`)
		}},
		{"top oneOf", "operations.echo.result.oneOf", "not allowed at the top", func(c *connectors.Contract) {
			c.Operations[0].Result = json.RawMessage(`{"type":"object","oneOf":[{"type":"object"}]}`)
		}},
		{"undescribed operation", "operations.echo.description", "needs a description", func(c *connectors.Contract) {
			c.Operations[0].Description = ""
		}},
		{"undescribed field", "operations.echo.args.properties.text", "needs a description", func(c *connectors.Contract) {
			c.Operations[0].Args = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
		}},
		{"bad correlation", "operations.start.callbacks[0].correlate.event", "has no field", func(c *connectors.Contract) {
			c.Operations[2].Callbacks[0].Correlate.Event = "nope"
		}},
		{"bad correlation result", "operations.start.callbacks[0].correlate.result", "has no field", func(c *connectors.Contract) {
			c.Operations[2].Callbacks[0].Correlate.Result = "nope"
		}},
		{"underscore", "id", "never _", func(c *connectors.Contract) { c.ID = "test_echo" }},
		{"example", "operations.echo.examples[0].args", "does not fit", func(c *connectors.Contract) {
			c.Operations[0].Examples[0].Args = json.RawMessage(`{"repeat":2}`)
		}},
		{"delivery", "events.finished.delivery", "not watch or callback", func(c *connectors.Contract) {
			c.Events[0].Delivery = []string{"push"}
		}},
		{"key", "operations.start.key", "no field", func(c *connectors.Contract) { c.Operations[2].Key = "nope" }},
	}
	for _, tc := range cases {
		c := connectorstest.Contract()
		tc.edit(&c)
		if p := connectors.CheckContract(c); !has(p, tc.path, tc.want) {
			t.Errorf("%s: want %q at %s, got %v", tc.name, tc.want, tc.path, p)
		}
	}
	ty := connectorstest.Types()[0]
	ty.Contracts = []string{"test.echo"}
	if p := connectors.CheckType(ty, nil); !has(p, "contracts[0]", "version") {
		t.Errorf("a type naming a contract without its version: %v", p)
	}
	ty.Contracts = []string{"test.echo@1", "test.echo@2"}
	if p := connectors.CheckType(ty, nil); !has(p, "contracts[1]", "one version") {
		t.Errorf("a type implementing two versions: %v", p)
	}
}
