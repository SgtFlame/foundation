package config

import (
	"context"
	"errors"
)

// SecretRef names one secret setting: the scope row it is stored at ("app",
// "tenant:1", "user:2"), its section and its leaf path.
type SecretRef struct {
	Scope, Section, Path string
}

// Name is the ref as a secret name, "config/<scope>/<section>/<path>"; it
// is the secret's path in Vault.
func (r SecretRef) Name() string {
	return "config/" + r.Scope + "/" + r.Section + "/" + r.Path
}

// SecretStore keeps the values of secret settings
// (documentation/Secrets.md › SecretStore). A Configuration value holds
// only the placeholder Put returns, which Get turns back into the value.
type SecretStore interface {
	// Put stores value and returns the placeholder kept in its place.
	Put(ctx context.Context, ref SecretRef, value string) (stored map[string]any, err error)
	// Get returns the value behind a placeholder Put returned.
	Get(ctx context.Context, ref SecretRef, stored map[string]any) (string, error)
	// Delete forgets a secret that is no longer set.
	Delete(ctx context.Context, ref SecretRef) error
	// Hints tells whether a client may see a secret's last four characters
	// (Secrets question 4: not when Vault holds it).
	Hints() bool
}

// CipherStore is slice 2's store: each secret sealed with the Cipher inside
// the Configuration value, as {"$secret": <ciphertext>}.
type CipherStore struct {
	Cipher *Cipher
}

var errNoCipher = errors.New("config: the cipher is not configured")

func (s CipherStore) Put(_ context.Context, ref SecretRef, value string) (map[string]any, error) {
	if s.Cipher == nil {
		return nil, errNoCipher
	}
	sealed, err := s.Cipher.Encrypt(ref.Section, ref.Path, value)
	if err != nil {
		return nil, err
	}
	return map[string]any{SecretField: sealed}, nil
}

func (s CipherStore) Get(_ context.Context, ref SecretRef, stored map[string]any) (string, error) {
	sealed, _ := stored[SecretField].(string)
	if s.Cipher == nil || sealed == "" {
		return "", errNoCipher
	}
	return s.Cipher.Decrypt(ref.Section, ref.Path, sealed)
}

func (CipherStore) Delete(context.Context, SecretRef) error { return nil }

func (CipherStore) Hints() bool { return true }

// IsPlaceholder tells whether a stored value is a secret's placeholder
// ({"$secret": …} or {"$vault": …}) rather than a plain value.
func IsPlaceholder(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	_, sealed := m[SecretField]
	_, vault := m[VaultField]
	return sealed || vault
}

// VaultField is how a secret held in Vault appears in a Configuration value:
// {"$vault": {"version": n}}.
const VaultField = "$vault"
