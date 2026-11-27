package impl

import (
	"context"
	"net/http"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/services/services/servicesservice"
)

func TestStatus(t *testing.T) {
	s := New(WithKnown([]string{"record", "ui", "chat"}), WithProcesses(func(context.Context) ([]servicesservice.Process, error) {
		return []servicesservice.Process{
			{Process: "web-1", Services: []string{"ui"}},
			{Process: "data-1", Services: []string{"record"}},
			{Process: "data-2", Services: []string{"record"}, Draining: true},
		}, nil
	}))
	resp := s.Status(servicesservice.StatusArgs{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}})
	if resp.Common.Code != http.StatusOK || len(resp.Processes) != 3 {
		t.Fatalf("status %+v", resp)
	}
	got := map[string][]string{}
	for _, x := range resp.Services {
		got[x.Name] = x.Processes
	}
	if len(got["record"]) != 1 || got["record"][0] != "data-1" || len(got["chat"]) != 0 || got["ui"][0] != "web-1" {
		t.Fatalf("services %+v", resp.Services)
	}
	if code := s.Status(servicesservice.StatusArgs{ServiceArgs: features.ServiceArgs{Principal: features.AnonymousPrincipal()}}).Common.Code; code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", code)
	}
}
