package identitydto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

type UpdateAccountRequest struct {
	DisplayName     *string `json:"displayName,omitempty"`
	Email           *string `json:"email,omitempty"`
	CurrentPassword string  `json:"currentPassword,omitempty"`
	NewPassword     string  `json:"newPassword,omitempty"`
}

func (r UpdateAccountRequest) ToArgs(args features.ServiceArgs) identityservice.UpdateAccountArgs {
	return identityservice.UpdateAccountArgs{ServiceArgs: args, DisplayName: r.DisplayName, Email: r.Email, CurrentPassword: r.CurrentPassword, NewPassword: r.NewPassword}
}
