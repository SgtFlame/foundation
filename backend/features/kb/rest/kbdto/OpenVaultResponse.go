package kbdto

import "github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"

type OpenVaultResponse struct {
	Message string `json:"message,omitempty"`
	VaultID uint64 `json:"vaultId,string,omitempty"`
}

// OpenVaultResponseFrom converts the service's OpenVaultResponse; its code is
// carried by the HTTP status rather than the body.
func OpenVaultResponseFrom(resp kbservice.OpenVaultResponse) OpenVaultResponse {
	return OpenVaultResponse{
		Message: resp.Common.Message,
		VaultID: resp.VaultID,
	}
}
