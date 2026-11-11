package records

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// Row rules (Records.md › Built in slice 21) are expr-lang expressions over
// record and principal that give a bool:
//   - record is the record's values by field id, and its own data (_id,
//     _createdBy, _updatedBy and the rest). A user is named by their id as
//     text, as a relation to user holds it, so _createdBy is text too.
//   - principal is {user, tenant, kind}: the user's and tenant's ids as
//     text, and "user" or "service".
//
// An empty rule allows every record. A rule that fails to run, or gives
// something other than a bool (record values are not typed, so Check
// cannot always tell), allows none.

// ruleEnv is the shape every rule is compiled against.
var ruleEnv = map[string]any{"record": map[string]any{}, "principal": map[string]any{}}

var rules sync.Map // source → *vm.Program

// CompileRule checks a row rule; nil for an empty one.
func CompileRule(src string) (*vm.Program, error) {
	if src == "" {
		return nil, nil
	}
	if p, ok := rules.Load(src); ok {
		return p.(*vm.Program), nil
	}
	p, err := expr.Compile(src, expr.Env(ruleEnv), expr.AsBool())
	if err != nil {
		return nil, err
	}
	rules.Store(src, p)
	return p, nil
}

// RuleEnv is what a rule sees of a record and a principal.
func RuleEnv(record map[string]json.RawMessage, p features.Principal) map[string]any {
	r := make(map[string]any, len(record))
	for k, raw := range record {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			r[k] = v
		}
	}
	for _, k := range []string{"_createdBy", "_updatedBy"} {
		if n, ok := r[k].(float64); ok {
			r[k] = strconv.FormatUint(uint64(n), 10)
		}
	}
	kind := "user"
	switch p.Kind {
	case features.PrincipalService:
		kind = "service"
	case features.PrincipalApp:
		kind = "app" // an installed app's own principal (forge 07a)
	}
	return map[string]any{"record": r, "principal": map[string]any{
		"user": strconv.FormatUint(p.UserID, 10), "tenant": strconv.FormatUint(p.TenantID, 10), "kind": kind, "app": p.App,
	}}
}

// Allows runs a compiled rule over a record for a principal. Service
// principals skip row rules.
func Allows(rule *vm.Program, record map[string]json.RawMessage, p features.Principal) bool {
	if rule == nil || p.Kind == features.PrincipalService {
		return true
	}
	out, err := expr.Run(rule, RuleEnv(record, p))
	ok, _ := out.(bool)
	return err == nil && ok
}

// RuleFor is the rule a principal is held to for an action on the type's
// records: the type's own rule, and the row rules of the app roles that
// give the principal the action (forge 05c question 4), any of which
// allows a record. It returns the rule's source too, for messages.
func (e EntityType) RuleFor(ctx context.Context, p features.Principal, action string) (*vm.Program, string) {
	src := ruleSource(e.Access, action)
	roles, restricted := authz.RowRules(ctx, p, "record."+action, Resource(p, e.ID), e.ID)
	if restricted {
		either := "(" + strings.Join(roles, ") || (") + ")"
		if src == "" {
			src = either
		} else {
			src = "(" + src + ") && (" + either + ")"
		}
	}
	prog, err := CompileRule(src)
	if err != nil {
		prog, _ = CompileRule("false")
	}
	return prog, src
}

func ruleSource(a Access, action string) string {
	return map[string]string{"read": a.Read, "create": a.Create, "update": a.Update, "delete": a.Delete}[action]
}

// Rule is the entity type's rule for an action (read, create, update or
// delete); nil when it has none. A rule that does not compile, which
// Check refuses, allows nothing.
func (e EntityType) Rule(action string) *vm.Program {
	p, err := CompileRule(ruleSource(e.Access, action))
	if err != nil {
		p, _ = CompileRule("false")
	}
	return p
}

// checkAccess is every problem with the row rules.
func checkAccess(a Access) map[string]string {
	out := map[string]string{}
	for action, src := range map[string]string{"read": a.Read, "create": a.Create, "update": a.Update, "delete": a.Delete} {
		if _, err := CompileRule(src); err != nil {
			out["access."+action] = fmt.Sprintf("the %s rule is not a bool expression over record and principal: %v", action, err)
		}
	}
	return out
}
