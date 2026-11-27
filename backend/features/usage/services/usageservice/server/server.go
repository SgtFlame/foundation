// Package server serves usageservice.Service's calls from NATS (forge
// 21a-1), handing each to a real implementation.
package server

import (
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
	"github.com/nats-io/nats.go"
)

// Ops are the usage service's operations as a server runs them.
func Ops(svc usageservice.Service) map[string]services.Handler {
	return map[string]services.Handler{
		"Summary": services.Unary(svc.Summary),
		"Records": services.Unary(svc.Records),
		"Capture": services.Unary(svc.Capture),
	}
}

// Serve serves svc's calls on nc.
func Serve(nc *nats.Conn, svc usageservice.Service, opts ...services.ServerOption) (*services.Server, error) {
	return services.Serve(nc, "usage", Ops(svc), opts...)
}
