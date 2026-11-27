package usageservice

import "github.com/SgtFlame/foundation/backend/features/services"

// Manifest is the service's declaration (forge 21f-1): the storage it
// owns, from documentation/DataServices.md's inventory.
var Manifest = services.Manifest{
	Owns: []string{"tenant DB: usage records and captures"},
	// usage.Events over NATS (forge 21f-2)
	Publishes: []string{"bus.usage.records"},
}
