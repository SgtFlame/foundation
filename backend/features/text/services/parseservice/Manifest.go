package parseservice

import "github.com/SgtFlame/foundation/backend/features/services"

// Manifest is the service's declaration (forge 21f-1): it owns nothing and
// reads uploaded files through the files service.
var Manifest = services.Manifest{
	Calls: []string{"files"},
}
