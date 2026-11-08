package authz

import (
	"context"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
)

// A context made WithAs decides what its role covers, with its row rules,
// and leaves every other check to the user's grants (forge 06e).
func TestAs(t *testing.T) {
	pol := testPolicy(t)
	Install(pol)
	t.Cleanup(func() { Install(nil) })
	user := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: 1}
	as := As{Name: "Member", Rules: map[string]string{"task": "record.assignee == principal.user"},
		Covers: func(action, resource string) (bool, bool) {
			if !strings.HasPrefix(resource, "tenant:1/volume:") {
				return false, false
			}
			return true, action == "volume.read"
		}}
	ctx := WithAs(context.Background(), as)
	if d := Check(ctx, user, "volume.read", "tenant:1/volume:1"); !d.Allowed || d.Reason != "as the role Member" {
		t.Errorf("a covered action the role gives: %+v", d)
	}
	if d := Check(ctx, user, "volume.write", "tenant:1/volume:1"); d.Allowed {
		t.Errorf("a covered action the role does not give: %+v", d)
	}
	// Not covered: the user's own grants, of which they have none
	if d := Check(ctx, user, "tenant.read", "tenant:1"); d.Allowed {
		t.Errorf("an action the role does not cover: %+v", d)
	}
	if rules, restricted := RowRules(ctx, user, "volume.read", "tenant:1/volume:1", "task"); !restricted || len(rules) != 1 {
		t.Errorf("the role's rule: %v %v", rules, restricted)
	}
	if _, restricted := RowRules(ctx, user, "volume.read", "tenant:1/volume:1", "other"); restricted {
		t.Error("a key the role has no rule for is restricted")
	}
	// A service principal is never previewed
	if d := Check(ctx, features.Principal{Kind: features.PrincipalService, TenantID: 1}, "volume.write", "tenant:1/volume:1"); !d.Allowed {
		t.Errorf("a service principal: %+v", d)
	}
}
