package kbservice

import (
	"github.com/SgtFlame/foundation/backend/features"
)

type Service interface {
	OpenVaultAsync(args OpenVaultArgs) *OpenVaultRequest
	OpenVault(args OpenVaultArgs) OpenVaultResponse
}

type OpenVaultArgs struct {
	features.ServiceArgs
	Path VolumePath
}

type OpenVaultRequest struct {
	Response chan *OpenVaultResponse
}

type OpenVaultResponse struct {
	Common  features.ServiceResponse
	VaultID uint64
}
