package userdto

import (
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// User is a user in the User editor: their account fields and tenants.
type User struct {
	identitydto.User
	TenantIDs []string `json:"tenantIds"`
}

func UserFrom(s userservice.Summary) User {
	out := User{User: identitydto.UserFrom(s.User), TenantIDs: make([]string, len(s.TenantIDs))}
	for i, t := range s.TenantIDs {
		out.TenantIDs[i] = strconv.FormatUint(t, 10)
	}
	return out
}

type ListResponse struct {
	rest.Status
	Users []User `json:"users"`
}

func ListResponseFrom(resp userservice.ListResponse) ListResponse {
	out := ListResponse{Status: rest.StatusFrom(resp.Common), Users: make([]User, len(resp.Users))}
	for i, u := range resp.Users {
		out.Users[i] = UserFrom(u)
	}
	return out
}

// UserResponse is the response of Create and Update.
type UserResponse struct {
	rest.Status
	User *User `json:"user,omitempty"`
}

func UserResponseFrom(common features.ServiceResponse, u userservice.Summary) UserResponse {
	out := UserResponse{Status: rest.StatusFrom(common)}
	if common.Code < 300 {
		user := UserFrom(u)
		out.User = &user
	}
	return out
}

type CreateRequest struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Password    string   `json:"password"`
	SuperAdmin  bool     `json:"superAdmin"`
	TenantIDs   []string `json:"tenantIds"`
}

// ToArgs converts the request; a tenant ID that does not parse becomes 0,
// which names no tenant.
func (r CreateRequest) ToArgs(args features.ServiceArgs) userservice.CreateArgs {
	tenants := make([]uint64, len(r.TenantIDs))
	for i, t := range r.TenantIDs {
		tenants[i], _ = strconv.ParseUint(t, 10, 64)
	}
	return userservice.CreateArgs{ServiceArgs: args, Username: r.Username, DisplayName: r.DisplayName, Email: r.Email,
		Password: r.Password, SuperAdmin: r.SuperAdmin, TenantIDs: tenants}
}

type UpdateRequest struct {
	UserID      uint64  `json:"userId,string"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	SuperAdmin  *bool   `json:"superAdmin,omitempty"`
	Disabled    *bool   `json:"disabled,omitempty"`
	Password    string  `json:"password,omitempty"`
}

func (r UpdateRequest) ToArgs(args features.ServiceArgs) userservice.UpdateArgs {
	return userservice.UpdateArgs{ServiceArgs: args, UserID: r.UserID, DisplayName: r.DisplayName, Email: r.Email,
		SuperAdmin: r.SuperAdmin, Disabled: r.Disabled, Password: r.Password}
}
