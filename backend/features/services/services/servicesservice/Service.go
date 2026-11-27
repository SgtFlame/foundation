package servicesservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
)

// Service tells which processes run which services (forge 21g-1), for
// the Services view and the forge services tool.
type Service interface {
	Status(args StatusArgs) StatusResponse
}

type StatusArgs struct {
	features.ServiceArgs
}

type StatusResponse struct {
	Common features.ServiceResponse
	// Processes announced now, by name.
	Processes []Process
	// Services are every service with the processes serving it; one no
	// process serves has none.
	Services []ServiceStatus
}

// Process is one process's announcement.
type Process struct {
	Process   string
	Host      string
	Version   string
	Set       string
	Services  []string
	Calls     []string
	Started   time.Time
	Heartbeat time.Time
	Draining  bool
}

// ServiceStatus is a service and who serves it.
type ServiceStatus struct {
	Name      string
	Processes []string
}
