package identity

import (
	"context"
	"errors"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"gorm.io/gorm"
)

// Session is what signing in, refreshing or switching tenants gives the
// browser: the access token and, unless a refresh within the grace period
// reused the current one, a new refresh token. Handlers put them in cookies.
type Session struct {
	Access         string
	AccessExpires  time.Time
	Refresh        string // empty: keep the cookie the browser has
	RefreshExpires time.Time
	User           User
	TenantID       uint64
}

// Sessions starts, rotates and revokes sessions in the application
// database.
type Sessions struct {
	DB     *gorm.DB
	Signer *Signer
	Now    func() time.Time // time.Now when nil
}

func (s Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// In is s writing through tx, inside a caller's transaction.
func (s Sessions) In(tx *gorm.DB) Sessions {
	s.DB = tx
	return s
}

// Start begins a new session for u in tenant.
func (s Sessions) Start(ctx context.Context, u User, tenant uint64) (Session, error) {
	return s.issue(s.DB.WithContext(ctx), u, tenant, features.NewTSID())
}

func (s Sessions) issue(tx *gorm.DB, u User, tenant, family uint64) (Session, error) {
	access, expires, err := s.Signer.Issue(u, tenant)
	if err != nil {
		return Session{}, err
	}
	refresh, hash, err := NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	row := RefreshToken{Hash: hash, Family: family, UserID: u.ID, TenantID: tenant, ExpiresAt: s.now().Add(RefreshLifetime)}
	if err := tx.Create(&row).Error; err != nil {
		return Session{}, err
	}
	return Session{Access: access, AccessExpires: expires, Refresh: refresh, RefreshExpires: row.ExpiresAt, User: u, TenantID: tenant}, nil
}

// Refresh errors: the token is unknown, expired or revoked, or its user may
// no longer use its tenant. Each one means signing in again.
var (
	ErrBadRefresh = errors.New("identity: the refresh token is not valid")
	ErrReused     = errors.New("identity: a replaced refresh token was used again")
)

// Rotate replaces the refresh token with a new one and issues an access
// token with the user's current flags. A token replaced less than
// RefreshGrace ago only gets a new access token; one replaced earlier is
// taken as stolen, and its whole family is revoked.
func (s Sessions) Rotate(ctx context.Context, token string) (Session, error) {
	var out Session
	var reused bool
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row RefreshToken
		err := tx.Where("hash = ?", HashToken(token)).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBadRefresh
		}
		if err != nil {
			return err
		}
		now := s.now()
		inGrace := row.ReplacedAt != nil && now.Sub(*row.ReplacedAt) < RefreshGrace
		if inGrace {
			// Only while its replacement is alive: not after signing out
			var alive int64
			if err := tx.Model(&RefreshToken{}).Where("family = ? AND revoked_at IS NULL", row.Family).Count(&alive).Error; err != nil {
				return err
			}
			inGrace = alive > 0
		}
		switch {
		case row.RevokedAt != nil && !inGrace:
			if row.ReplacedAt != nil {
				reused = true
			}
			return ErrBadRefresh
		case now.After(row.ExpiresAt):
			return ErrBadRefresh
		}
		u, err := usable(tx, row.UserID, row.TenantID)
		if err != nil {
			return err
		}
		if inGrace {
			access, expires, err := s.Signer.Issue(u, row.TenantID)
			out = Session{Access: access, AccessExpires: expires, User: u, TenantID: row.TenantID}
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"revoked_at": now, "replaced_at": now}).Error; err != nil {
			return err
		}
		out, err = s.issue(tx, u, row.TenantID, row.Family)
		return err
	})
	if reused {
		if rerr := s.revokeFamilyOf(ctx, token); rerr != nil {
			return Session{}, rerr
		}
		return Session{}, ErrReused
	}
	return out, err
}

// usable is the user, if they are enabled and a member of tenant.
func usable(tx *gorm.DB, userID, tenant uint64) (User, error) {
	var u User
	if err := tx.First(&u, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return User{}, ErrBadRefresh
		}
		return User{}, err
	}
	if u.Disabled {
		return User{}, ErrBadRefresh
	}
	var n int64
	if err := tx.Model(&Membership{}).Where("tenant_id = ? AND user_id = ?", tenant, userID).Count(&n).Error; err != nil {
		return User{}, err
	}
	if n == 0 {
		return User{}, ErrBadRefresh
	}
	return u, nil
}

// End revokes the session the refresh token belongs to (signing out).
func (s Sessions) End(ctx context.Context, token string) error {
	return s.revokeFamilyOf(ctx, token)
}

func (s Sessions) revokeFamilyOf(ctx context.Context, token string) error {
	var row RefreshToken
	err := s.DB.WithContext(ctx).Where("hash = ?", HashToken(token)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Model(&RefreshToken{}).Where("family = ? AND revoked_at IS NULL", row.Family).
		Update("revoked_at", s.now()).Error
}

// RevokeUser revokes the user's sessions: in one tenant, or in all of them
// when tenant is 0. Their access tokens still work until they expire.
func (s Sessions) RevokeUser(tx *gorm.DB, userID, tenant uint64) error {
	q := tx.Model(&RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", userID)
	if tenant != 0 {
		q = q.Where("tenant_id = ?", tenant)
	}
	return q.Update("revoked_at", s.now()).Error
}

// TenantFor is where u's session starts: want if u is a member there, else
// the tenant they last used, else their first. ok is false when u belongs
// to no tenant.
func TenantFor(tx *gorm.DB, u User, want uint64) (uint64, bool, error) {
	var tenants []uint64
	if err := tx.Model(&Membership{}).Where("user_id = ?", u.ID).Order("tenant_id").Pluck("tenant_id", &tenants).Error; err != nil {
		return 0, false, err
	}
	if len(tenants) == 0 {
		return 0, false, nil
	}
	for _, pick := range []uint64{want, u.LastTenantID} {
		for _, t := range tenants {
			if pick != 0 && t == pick {
				return t, true, nil
			}
		}
	}
	return tenants[0], true, nil
}
