// Package proxy is usageservice.Service over NATS (forge 21a-1): each call
// goes to a server of the usage service, wherever it runs.
package proxy

import (
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

// Service is the name of the usage service on the wire.
const Service = "usage"

// Proxy sends each call to a server of the usage service.
type Proxy struct{ C *services.Client }

var _ usageservice.Service = (*Proxy)(nil)

// New is a proxy sending its calls with c.
func New(c *services.Client) *Proxy { return &Proxy{C: c} }

func (p *Proxy) Summary(args usageservice.SummaryArgs) usageservice.SummaryResponse {
	return services.Call[usageservice.SummaryResponse](p.C, Service, "Summary", args, services.Idempotent())
}

func (p *Proxy) Records(args usageservice.RecordsArgs) usageservice.RecordsResponse {
	return services.Call[usageservice.RecordsResponse](p.C, Service, "Records", args, services.Idempotent())
}

func (p *Proxy) Capture(args usageservice.CaptureArgs) usageservice.CaptureResponse {
	return services.Call[usageservice.CaptureResponse](p.C, Service, "Capture", args, services.Idempotent())
}
