package authzdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/authz"
)

type Action struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Class string `json:"class"`
	On    string `json:"on"`
}

type ResourceType struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Source  string   `json:"source"`
	Parent  string   `json:"parent"`
	Owned   bool     `json:"owned,omitempty"`
	Actions []Action `json:"actions"`
}

func ResourceTypeFrom(t authz.ResourceType) ResourceType {
	out := ResourceType{ID: t.ID, Title: t.Title, Source: t.Source, Parent: t.Parent, Owned: t.Owned, Actions: make([]Action, len(t.Actions))}
	for i, a := range t.Actions {
		out.Actions[i] = Action{Name: a.Name, Title: a.Title, Class: string(a.Class), On: a.On}
	}
	return out
}

type Role struct {
	ID          uint64   `json:"id,string"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Scope       string   `json:"scope"`
	BuiltIn     bool     `json:"builtIn,omitempty"`
	Permissions []string `json:"permissions"`
	Revision    int      `json:"revision,omitempty"`
}

func RoleFrom(r authz.Role) Role {
	return Role{ID: r.ID, Name: r.Name, Description: r.Description, Scope: r.Scope, BuiltIn: r.BuiltIn, Permissions: r.Permissions, Revision: r.Revision}
}

type Grant struct {
	ID        uint64     `json:"id,string"`
	UserID    uint64     `json:"userId,string"`
	RoleID    uint64     `json:"roleId,string"`
	Resource  string     `json:"resource"`
	GrantedBy uint64     `json:"grantedBy,string"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func GrantFrom(g authz.RoleGrant) Grant {
	return Grant{ID: g.ID, UserID: g.UserID, RoleID: g.RoleID, Resource: g.Resource, GrantedBy: g.GrantedBy, CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt}
}

func GrantsFrom(gs []authz.RoleGrant) []Grant {
	out := make([]Grant, len(gs))
	for i, g := range gs {
		out[i] = GrantFrom(g)
	}
	return out
}

// Held is one of the principal's grants with its role's permissions.
type Held struct {
	Grant       Grant    `json:"grant"`
	RoleName    string   `json:"roleName"`
	Permissions []string `json:"permissions"`
}

type Effective struct {
	All        bool     `json:"all,omitempty"`
	Grants     []Held   `json:"grants"`
	Self       []string `json:"self"`
	OwnedTypes []string `json:"ownedTypes"`
}

func EffectiveFrom(e authz.Effective) Effective {
	out := Effective{All: e.All, Grants: make([]Held, len(e.Grants)), Self: e.Self, OwnedTypes: e.OwnedTypes}
	for i, h := range e.Grants {
		out.Grants[i] = Held{Grant: GrantFrom(h.Grant), RoleName: h.Role.Name, Permissions: h.Permissions}
	}
	if out.Self == nil {
		out.Self = []string{}
	}
	if out.OwnedTypes == nil {
		out.OwnedTypes = []string{}
	}
	return out
}

type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	GrantID uint64 `json:"grantId,omitempty,string"`
	RoleID  uint64 `json:"roleId,omitempty,string"`
}

func DecisionFrom(d authz.Decision) Decision {
	return Decision{Allowed: d.Allowed, Reason: d.Reason, GrantID: d.GrantID, RoleID: d.RoleID}
}
