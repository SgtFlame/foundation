package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// Lifetimes of a session's two tokens (MultiTenant.md question 2).
const (
	AccessLifetime  = 15 * time.Minute
	RefreshLifetime = 30 * 24 * time.Hour
	// RefreshGrace is how long a rotated refresh token still gives a new
	// access token, without revoking its family: two browser tabs refreshing
	// at once both send the old one.
	RefreshGrace = 30 * time.Second
)

const issuer = "foundation"

// Claims are an access token's: the user (subject), the active tenant and
// the user's policy version. Grants are never in the
// token (RBAC.md › Evaluation).
type Claims struct {
	jwt.RegisteredClaims
	TenantID      string `json:"tid"`
	PolicyVersion int    `json:"pv"`
}

// Signer signs and verifies access tokens with HMAC-SHA256.
type Signer struct {
	key []byte
	now func() time.Time
}

// NewSigner signs with key.
func NewSigner(key []byte) *Signer {
	return &Signer{key: key, now: time.Now}
}

// LoadSigner reads the signing key from the application database, sealed
// by cipher, making one on first use.
func LoadSigner(gdb *gorm.DB, cipher *config.Cipher) (*Signer, error) {
	var row SigningKey
	err := gdb.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		sealed, err := cipher.Encrypt("identity", "signingKey", base64.StdEncoding.EncodeToString(key))
		if err != nil {
			return nil, err
		}
		if err := gdb.Create(&SigningKey{Sealed: sealed}).Error; err != nil {
			return nil, err
		}
		return NewSigner(key), nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := cipher.Decrypt("identity", "signingKey", row.Sealed)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(plain)
	if err != nil {
		return nil, err
	}
	return NewSigner(key), nil
}

// Issue makes an access token for u in tenant, and its expiry.
func (s *Signer) Issue(u User, tenant uint64) (string, time.Time, error) {
	now := s.now()
	expires := now.Add(AccessLifetime)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatUint(u.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
		TenantID:      strconv.FormatUint(tenant, 10),
		PolicyVersion: u.PolicyVersion,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	return token, expires, err
}

// ErrExpired is Verify's error for a token that was valid but has expired,
// which rest.RequireUser answers with session_expired.
var ErrExpired = rest.ErrExpired

// Verify checks an access token and returns its principal.
func (s *Signer) Verify(token string) (features.Principal, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return s.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(), jwt.WithTimeFunc(s.now))
	if errors.Is(err, jwt.ErrTokenExpired) {
		return features.Principal{}, ErrExpired
	}
	if err != nil {
		return features.Principal{}, err
	}
	user, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil {
		return features.Principal{}, err
	}
	tenant, err := strconv.ParseUint(claims.TenantID, 10, 64)
	if err != nil {
		return features.Principal{}, err
	}
	return features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: tenant}, nil
}

// NewRefreshToken makes a random refresh token: what the cookie holds, and
// the hash stored in its place.
func NewRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is the stored form of a refresh token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
