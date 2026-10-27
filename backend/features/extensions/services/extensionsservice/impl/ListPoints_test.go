package impl

import (
	"net/http"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/extensions/services/extensionsservice"
)

func TestListPoints(t *testing.T) {
	reg := extensions.NewRegistry()
	reg.Declare(config.Sections)
	config.Sections.Register(reg, config.Section{Descriptor: extensions.Descriptor{ID: "ui.theme", Source: "settings"}})

	resp := newService(WithRegistry(reg)).ListPoints(extensionsservice.ListPointsArgs{
		ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()},
	})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("code = %d %s", resp.Common.Code, resp.Common.Message)
	}
	if len(resp.Points) != 1 || resp.Points[0].ID != "config.section" || len(resp.Points[0].Contributions) != 1 {
		t.Errorf("points = %+v", resp.Points)
	}
}
