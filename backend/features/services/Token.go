package services

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
)

// HeaderToken carries the call token (forge 21b).
const HeaderToken = "Svc-Token"

// TokenTTL is how long a call token is good for.
const TokenTTL = 5 * time.Minute

// Error codes of call identity.
const (
	CodeExpired          = "call_expired"           // 401: the token's time passed
	CodePrincipalChanged = "call_principal_changed" // 403: a process without the key asked for another principal
	CodeAsLocal          = "call_as_local"          // 403: a check decided by a role (authz.WithAs) cannot travel
)

// Token is a signed statement of who a call is for, made where the call
// entered the system and forwarded unchanged with every call made on its
// behalf (forge 21b). A server rebuilds the principal from it and then
// runs the operation's own access check.
type Token struct {
	Principal features.PrincipalRef `json:"p"`
	Tenant    uint64                `json:"t,string,omitempty"`
	Issued    int64                 `json:"iat"`
	Expires   int64                 `json:"exp"`
	// Parent is the hash of the token this one narrows (an agent's run
	// started under a person's call), empty for a token made at entry.
	Parent string `json:"parent,omitempty"`
	raw    string
}

// PrincipalOf is the principal the token names.
func (t Token) PrincipalOf() features.Principal { return t.Principal.In(t.Tenant) }

// Raw is the token as it travels.
func (t Token) Raw() string { return t.raw }

// Hash names the token (for a child's Parent).
func (t Token) Hash() string {
	h := sha256.Sum256([]byte(t.raw))
	return hex.EncodeToString(h[:8])
}

// Names reports whether the token is for p.
func (t Token) Names(p features.Principal) bool {
	return t.Tenant == p.TenantID && t.Principal == p.Ref()
}

// Keys sign and verify call tokens. A process that may make tokens (one
// that runs an entry point: the web service, the job runner, triggers)
// holds Current; every server holds the public keys only, and accepts
// Previous until a rotation drops it.
type Keys struct {
	Current  ed25519.PrivateKey
	Previous []ed25519.PublicKey
}

// Public are the keys a server accepts.
func (k *Keys) Public() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	if k.Current != nil {
		out = append(out, k.Current.Public().(ed25519.PublicKey))
	}
	return append(out, k.Previous...)
}

// NewKeys makes a fresh key.
func NewKeys() (*Keys, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Keys{Current: priv}, nil
}

// Mint signs a token for p, narrowing parent when given.
func (k *Keys) Mint(p features.Principal, parent *Token) (Token, error) {
	if k == nil || k.Current == nil {
		return Token{}, errors.New("this process holds no call key")
	}
	now := time.Now()
	t := Token{Principal: p.Ref(), Tenant: p.TenantID, Issued: now.Unix(), Expires: now.Add(TokenTTL).Unix()}
	if parent != nil {
		t.Parent = parent.Hash()
	}
	body, err := json.Marshal(t)
	if err != nil {
		return Token{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := ed25519.Sign(k.Current, []byte(payload))
	t.raw = payload + "." + base64.RawURLEncoding.EncodeToString(sig)
	return t, nil
}

// ErrExpired is a token whose time passed.
var ErrExpired = errors.New("the call token expired")

// Verify checks raw's signature against public and its expiry.
func Verify(raw string, public []ed25519.PublicKey, now time.Time) (Token, error) {
	payload, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return Token{}, errors.New("the call token is malformed")
	}
	s, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Token{}, errors.New("the call token is malformed")
	}
	good := false
	for _, k := range public {
		if ed25519.Verify(k, []byte(payload), s) {
			good = true
			break
		}
	}
	if !good {
		return Token{}, errors.New("the call token's signature fails")
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Token{}, errors.New("the call token is malformed")
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil {
		return Token{}, errors.New("the call token is malformed")
	}
	t.raw = raw
	if now.Unix() >= t.Expires {
		return t, ErrExpired
	}
	return t, nil
}

type tokenKey struct{}

// WithToken is ctx carrying t, which calls made with it forward.
func WithToken(ctx context.Context, t Token) context.Context {
	return context.WithValue(ctx, tokenKey{}, t)
}

// TokenFrom is the token ctx carries.
func TokenFrom(ctx context.Context) (Token, bool) {
	t, ok := ctx.Value(tokenKey{}).(Token)
	return t, ok
}

// keyFile is how a key file holds the keys: the current private key and
// the previous public keys, base64.
type keyFile struct {
	Current  string   `json:"current"`
	Previous []string `json:"previous,omitempty"`
}

// LoadOrCreateKeys reads the call keys from path (<data-dir>/call.key),
// making a new key there when it does not exist.
func LoadOrCreateKeys(path string) (*Keys, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		k, err := NewKeys()
		if err != nil {
			return nil, err
		}
		return k, SaveKeys(path, k)
	}
	if err != nil {
		return nil, err
	}
	return ParseKeys(b)
}

// ParseKeys reads keys as SaveKeys writes them (a key file, or Vault's
// server/call-key).
func ParseKeys(b []byte) (*Keys, error) {
	var f keyFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("the call key cannot be read: %v", err)
	}
	priv, err := base64.StdEncoding.DecodeString(f.Current)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("the call key cannot be read")
	}
	k := &Keys{Current: ed25519.PrivateKey(priv)}
	for _, p := range f.Previous {
		pub, err := base64.StdEncoding.DecodeString(p)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, errors.New("a previous call key cannot be read")
		}
		k.Previous = append(k.Previous, ed25519.PublicKey(pub))
	}
	return k, nil
}

// Marshal is the keys as SaveKeys writes them.
func (k *Keys) Marshal() []byte {
	f := keyFile{Current: base64.StdEncoding.EncodeToString(k.Current)}
	for _, p := range k.Previous {
		f.Previous = append(f.Previous, base64.StdEncoding.EncodeToString(p))
	}
	b, _ := json.Marshal(f)
	return b
}

// SaveKeys writes k to path, readable only by its owner.
func SaveKeys(path string, k *Keys) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, k.Marshal(), 0o600)
}

// Rotate makes a new current key and keeps the old one's public key as
// the only previous one, so tokens made before stay good until they
// expire; rotate again after TokenTTL to drop it.
func (k *Keys) Rotate() (*Keys, error) {
	n, err := NewKeys()
	if err != nil {
		return nil, err
	}
	n.Previous = []ed25519.PublicKey{k.Current.Public().(ed25519.PublicKey)}
	return n, nil
}

// resign signs t again as it stands (tests change its times).
func resign(k *Keys, t Token) (Token, error) {
	t.raw = ""
	body, err := json.Marshal(t)
	if err != nil {
		return Token{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	t.raw = payload + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(k.Current, []byte(payload)))
	return t, nil
}
