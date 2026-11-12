package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

// EditorKinds lists the installed apps and the kinds of top-level tab
// their pages open as, the Go apps' and the tenant's installed ones; the
// browser contributes a workbench.editor for each.
func (s *service) EditorKinds(args uiservice.EditorKindsArgs) uiservice.EditorKindsResponse {
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, "ui.read", resource).Err("ui.read", resource); err != nil {
		return uiservice.EditorKindsResponse{Common: *args.Fail(authz.Status, err)}
	}
	resp := uiservice.EditorKindsResponse{Common: features.OK(), Apps: ui.Apps.All(s.reg), Kinds: ui.EditorKinds.All(s.reg)}
	// And the tenant's installed apps (forge 05d)
	for _, src := range ui.AppSources.All(s.reg) {
		if src.Apps != nil {
			a, k := src.Apps(args.ServiceArgs)
			resp.Apps, resp.Kinds = append(resp.Apps, a...), append(resp.Kinds, k...)
		}
	}
	return resp
}
