package identitydto

import (
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

// Account is the signed-in user, the active tenant and the tenants they may
// switch to.
type Account struct {
	User     User     `json:"user"`
	TenantID uint64   `json:"tenantId,string"`
	Tenants  []Tenant `json:"tenants"`
}

// User is a user as their own account and the User editor show them; it
// never carries the password hash.
type User struct {
	ID          uint64 `json:"id,string"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	SuperAdmin  bool   `json:"superAdmin"` // holds Super Administrator on the application
	Disabled    bool   `json:"disabled,omitempty"`
}

type Tenant struct {
	ID   uint64 `json:"id,string"`
	Name string `json:"name"`
}

// UserFrom converts a user, leaving out the password hash.
func UserFrom(u identity.User) User {
	return User{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, SuperAdmin: u.SuperAdmin, Disabled: u.Disabled}
}

func TenantFrom(t identity.Tenant) Tenant {
	return Tenant{ID: t.ID, Name: t.Name}
}

func AccountFrom(a identityservice.Account) Account {
	out := Account{User: UserFrom(a.User), TenantID: a.TenantID, Tenants: make([]Tenant, len(a.Tenants))}
	for i, t := range a.Tenants {
		out.Tenants[i] = TenantFrom(t)
	}
	return out
}
