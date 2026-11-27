package configservice

import "github.com/SgtFlame/foundation/backend/features/services"

// Manifest is the service's declaration (forge 21f-1): the storage it
// owns, from documentation/DataServices.md's inventory.
var Manifest = services.Manifest{
	Owns: []string{"tenant DB: settings"},
	// config.Changes over NATS (forge 21f-2): every change, and the other
	// processes' changes dropping this one's cached values
	Publishes: []string{"bus.config.changes"},
	Follows:   []string{"bus.config.changes"},
}
