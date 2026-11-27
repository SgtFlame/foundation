package servicesdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/services/services/servicesservice"
)

type StatusResponse struct {
	rest.Status
	Processes []Process       `json:"processes"`
	Services  []ServiceStatus `json:"services"`
}

type Process struct {
	Process   string    `json:"process"`
	Host      string    `json:"host"`
	Version   string    `json:"version"`
	Set       string    `json:"set"`
	Services  []string  `json:"services"`
	Calls     []string  `json:"calls"`
	Started   time.Time `json:"started"`
	Heartbeat time.Time `json:"heartbeat"`
	Draining  bool      `json:"draining"`
}

type ServiceStatus struct {
	Name      string   `json:"name"`
	Processes []string `json:"processes"`
}

// StatusResponseFrom converts the service's StatusResponse; its code is
// carried by the HTTP status rather than the body.
func StatusResponseFrom(resp servicesservice.StatusResponse) StatusResponse {
	out := StatusResponse{Status: rest.StatusFrom(resp.Common), Processes: []Process{}, Services: []ServiceStatus{}}
	for _, p := range resp.Processes {
		out.Processes = append(out.Processes, Process(p))
	}
	for _, s := range resp.Services {
		ps := s.Processes
		if ps == nil {
			ps = []string{}
		}
		out.Services = append(out.Services, ServiceStatus{Name: s.Name, Processes: ps})
	}
	return out
}
