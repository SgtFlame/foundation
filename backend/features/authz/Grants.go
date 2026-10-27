package authz

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Grant errors, for authzservice to answer with a status.
var (
	ErrNoRole        = errors.New("there is no such role")
	ErrWrongScope    = errors.New("the role cannot be granted on this resource")
	ErrLastSuperUser = errors.New("one Super Administrator must remain")
)

// GrantArgs names a grant to make. By is the granting user (0: the system).
type GrantArgs struct {
	UserID    uint64
	RoleID    uint64
	Resource  string
	By        uint64
	ExpiresAt *time.Time
}

// Grant gives a role on a resource, inside tx (nil: the policy's database;
// a caller passing its own transaction calls Changed once it commits).
// An app role goes only on "app", a tenant role only on a tenant or below,
// and a custom role only within its own tenant. Granting the same role on
// the same resource again returns the existing grant.
func (pol *Policy) Grant(ctx context.Context, tx *gorm.DB, args GrantArgs) (RoleGrant, error) {
	if tx == nil {
		tx = pol.db
	}
	roles, err := pol.Roles()
	if err != nil {
		return RoleGrant{}, err
	}
	role, ok := roles[args.RoleID]
	if !ok {
		return RoleGrant{}, ErrNoRole
	}
	if !GrantableOn(role, args.Resource) {
		return RoleGrant{}, ErrWrongScope
	}
	resource := Normalize(args.Resource)
	var existing []RoleGrant
	if err := tx.Where("user_id = ? AND role_id = ? AND resource = ?", args.UserID, args.RoleID, resource).Limit(1).Find(&existing).Error; err != nil {
		return RoleGrant{}, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	g := RoleGrant{UserID: args.UserID, RoleID: args.RoleID, Resource: resource, GrantedBy: args.By, ExpiresAt: args.ExpiresAt}
	if err := tx.Create(&g).Error; err != nil {
		return RoleGrant{}, err
	}
	pol.Changed()
	pol.auditIn(ctx, tx, AccessAudit{Kind: AuditGrant, UserID: args.UserID, ActorID: args.By, TenantID: tenantOf(resource), Action: role.Name, Resource: resource})
	return g, nil
}

// Revoke removes a grant. The last Super Administrator grant cannot go.
func (pol *Policy) Revoke(ctx context.Context, tx *gorm.DB, grantID, by uint64) (RoleGrant, error) {
	if tx == nil {
		tx = pol.db
	}
	var g RoleGrant
	if err := tx.First(&g, grantID).Error; err != nil {
		return RoleGrant{}, err
	}
	if g.RoleID == RoleSuperAdmin {
		n, err := pol.superAdmins(tx, g.UserID)
		if err != nil {
			return RoleGrant{}, err
		}
		if n == 0 {
			return RoleGrant{}, ErrLastSuperUser
		}
	}
	if err := tx.Delete(&g).Error; err != nil {
		return RoleGrant{}, err
	}
	pol.Changed()
	roles, _ := pol.Roles()
	pol.auditIn(ctx, tx, AccessAudit{Kind: AuditRevoke, UserID: g.UserID, ActorID: by, TenantID: tenantOf(g.Resource), Action: roles[g.RoleID].Name, Resource: g.Resource})
	return g, nil
}

// RevokeUnder removes a user's grants on resource and everything under it,
// as when they leave a tenant; user 0 removes everyone's, as when a tenant
// is deleted.
func (pol *Policy) RevokeUnder(ctx context.Context, tx *gorm.DB, user uint64, resource string, by uint64) error {
	if tx == nil {
		tx = pol.db
	}
	resource = Normalize(resource)
	q := tx.Where("(resource = ? OR resource LIKE ? ESCAPE '\\')", resource, escapeLike(resource)+"/%")
	if user != 0 {
		q = q.Where("user_id = ?", user)
	}
	var rows []RoleGrant
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	for _, g := range rows {
		if err := tx.Delete(&g).Error; err != nil {
			return err
		}
		pol.auditIn(ctx, tx, AccessAudit{Kind: AuditRevoke, UserID: g.UserID, ActorID: by, TenantID: tenantOf(g.Resource), Resource: g.Resource})
	}
	if len(rows) > 0 {
		pol.Changed()
	}
	return nil
}

// SuperAdmins is the users holding Super Administrator on the application.
func (pol *Policy) SuperAdmins(tx *gorm.DB) ([]uint64, error) {
	if tx == nil {
		tx = pol.db
	}
	var ids []uint64
	err := tx.Model(&RoleGrant{}).Where("role_id = ? AND resource = ?", RoleSuperAdmin, OnApp).Distinct().Pluck("user_id", &ids).Error
	return ids, err
}

// superAdmins counts the Super Administrators other than user.
func (pol *Policy) superAdmins(tx *gorm.DB, user uint64) (int, error) {
	ids, err := pol.SuperAdmins(tx)
	n := 0
	for _, id := range ids {
		if id != user {
			n++
		}
	}
	return n, err
}

// GrantsOn is the grants on resource exactly, or with under, on everything
// below it too.
func (pol *Policy) GrantsOn(tx *gorm.DB, resource string, under bool) ([]RoleGrant, error) {
	if tx == nil {
		tx = pol.db
	}
	resource = Normalize(resource)
	q := tx.Where("resource = ?", resource)
	if under {
		q = tx.Where("(resource = ? OR resource LIKE ? ESCAPE '\\')", resource, escapeLike(resource)+"/%")
	}
	var rows []RoleGrant
	err := q.Order("id").Find(&rows).Error
	return rows, err
}

// GrantableOn tells whether role may be granted on resource.
func GrantableOn(role Role, resource string) bool {
	path := Split(resource)
	switch {
	case role.Scope == OnApp:
		return len(path) == 0
	case len(path) == 0 || !strings.HasPrefix(path[0], "tenant:"):
		return false
	case role.TenantID != 0:
		return path[0] == Resource("tenant", role.TenantID)
	}
	return true
}

// Normalize is the stored form of a resource path: "app", or the segments
// below it.
func Normalize(resource string) string {
	path := Split(resource)
	if len(path) == 0 {
		return OnApp
	}
	return strings.Join(path, "/")
}

// tenantOf is the tenant a path is in, 0 for the application.
func tenantOf(resource string) uint64 {
	path := Split(resource)
	if len(path) == 0 {
		return 0
	}
	var id uint64
	if rest, ok := strings.CutPrefix(path[0], "tenant:"); ok {
		for _, c := range rest {
			if c < '0' || c > '9' {
				return 0
			}
			id = id*10 + uint64(c-'0')
		}
	}
	return id
}

// TenantOf is the tenant a resource path is in, 0 for the application.
func TenantOf(resource string) uint64 { return tenantOf(resource) }

func escapeLike(s string) string {
	return strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(s)
}
