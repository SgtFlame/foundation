package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SecretKeyEnv holds the base64 key secrets are encrypted with; without it
// the key is read from (or created at) <data-dir>/secret.key.
const SecretKeyEnv = "FOUNDATION_SECRET_KEY"

// Cipher encrypts secrets at rest with AES-256-GCM: secret settings without
// Vault, and the JWT signing key. Each ciphertext is bound to its section and
// path, so it cannot be moved to another key. With Vault its key is
// server/db-key in the bootstrap bundle (documentation/Secrets.md).
type Cipher struct {
	aead cipher.AEAD
}

// LoadCipher takes the key from $FOUNDATION_SECRET_KEY, or else from
// dataDir/secret.key, creating that file (mode 0600) on first use.
func LoadCipher(dataDir string) (*Cipher, error) {
	encoded := strings.TrimSpace(os.Getenv(SecretKeyEnv))
	if encoded == "" {
		path := filepath.Join(dataDir, "secret.key")
		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}
			encoded = base64.StdEncoding.EncodeToString(key)
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte(encoded+"\n"), 0o600); err != nil {
				return nil, fmt.Errorf("config: create %s: %w", path, err)
			}
		case err != nil:
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		default:
			encoded = strings.TrimSpace(string(data))
		}
	}
	return CipherFrom(encoded)
}

// CipherFrom uses a base64 key, such as Vault's server/db-key.
func CipherFrom(encoded string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("config: the secret key is not base64: %w", err)
	}
	return NewCipher(key)
}

// NewCipher uses a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("config: the secret key must be 32 bytes, not %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plain for section and path, as base64 of nonce and ciphertext.
func (c *Cipher) Encrypt(section, path, plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), []byte(section+"#"+path))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens what Encrypt sealed for the same section and path.
func (c *Cipher) Decrypt(section, path, sealed string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	n := c.aead.NonceSize()
	if len(data) < n {
		return "", errors.New("config: the secret is truncated")
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], []byte(section+"#"+path))
	if err != nil {
		return "", errors.New("config: the secret does not decrypt with this key")
	}
	return string(plain), nil
}

// SecretField is how a stored secret appears in a Configuration value.
const SecretField = "$secret"

// Hint is what a client sees of a secret: the last four characters of a
// long enough one.
func Hint(plain string) string {
	if len(plain) < 12 {
		return ""
	}
	return "…" + plain[len(plain)-4:]
}
