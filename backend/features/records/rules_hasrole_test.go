package records

import (
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
)

// principal.hasRole compiles and runs in a row rule (forge 07j).
func TestRuleHasRole(t *testing.T) {
	prog, err := CompileRule("principal.hasRole('mailSender', record.account) || principal.kind == 'agent' && principal.agent == '9'")
	if err != nil {
		t.Fatal(err)
	}
	rec := map[string]json.RawMessage{"account": json.RawMessage(`"7"`)}
	if Allows(prog, rec, features.AgentPrincipal(1, 8, 2)) {
		t.Error("allowed with no policy grant")
	}
	if !Allows(prog, rec, features.AgentPrincipal(1, 9, 2)) {
		t.Error("principal.agent")
	}
}
