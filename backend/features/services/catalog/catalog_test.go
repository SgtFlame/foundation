package catalog

import (
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/services"
)

// TestEveryOperationTravels fails for an operation whose args or response
// hold something that cannot be sent (forge 21a-2), unless it is marked
// //svc:local or //svc:stream.
func TestEveryOperationTravels(t *testing.T) {
	var bad []string
	for _, s := range Services {
		bad = append(bad, services.CannotTravel(s)...)
	}
	if len(bad) > 0 {
		t.Fatalf("operations that cannot travel:\n  %s", strings.Join(bad, "\n  "))
	}
}
