package kbdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

type OpenVaultRequest struct {
	Tenant uint64     `json:"tenant,string"`
	Path   VolumePath `json:"path"`
}

// ToArgs converts the request to the service's OpenVaultArgs.
func (r OpenVaultRequest) ToArgs() kbservice.OpenVaultArgs {
	return kbservice.OpenVaultArgs{
		ServiceArgs: features.ServiceArgs{Tenant: r.Tenant},
		Path:        r.Path.ToVolumePath(),
	}
}
