package ctl

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go/jetstream"
)

// Signed scripts (forge 20c-3): the only executable content a daemon
// accepts from the network. A script is kept in the control account's KV
// ctl_scripts under sha256.<hex> of its bytes, with an Ed25519 signature
// over that hash by the root tenant's script signing key. Scripts are
// small, so a KV value (read by direct get, as desired state is) stands in
// for the object store the plan named.
const (
	ScriptsBucket = "ctl_scripts"
	// MaxScript is the largest script kept.
	MaxScript = 256 << 10
)

// ScriptKey is a script's key: sha256.<hex>.
func ScriptKey(hash string) string { return strings.Replace(hash, ":", ".", 1) }

// Script is what ctl_scripts holds for a hash.
type Script struct {
	Content   string `json:"content"`
	Signature string `json:"signature"` // base64 Ed25519 over the hash text
	Key       string `json:"key"`       // the signing key's public half, base64
}

// HashOf is a script's name: sha256:<hex> of its bytes.
func HashOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PublicKey is a key's public half as daemons hold it (base64).
func PublicKey(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

// LoadOrMakeSigningKey reads the script signing key's seed at path, or
// makes one there (0600) when there is none.
func LoadOrMakeSigningKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, log.Code("bad_signing_key").Errorf("%s is not a script signing key", path)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, log.Wrap(err)
	}
	return MakeSigningKey(path)
}

// MakeSigningKey makes a new script signing key at path, replacing any
// (a rotation: daemons then accept only scripts the new key signs, once
// desired state carries it).
func MakeSigningKey(path string) (ed25519.PrivateKey, error) {
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, log.Wrap(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, log.Wrap(err)
	}
	seed := base64.StdEncoding.EncodeToString(k.Seed())
	return k, log.Wrap(os.WriteFile(path, []byte(seed+"\n"), 0o600))
}

// CheckScript is what every script must be: bash, failing on errors.
func CheckScript(content string) error {
	if len(content) > MaxScript {
		return log.Code("script_too_large").Errorf("a script is at most %d KB", MaxScript>>10)
	}
	lines := strings.SplitN(content, "\n", 3)
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "#!/usr/bin/env bash" || strings.TrimSpace(lines[1]) != "set -euo pipefail" {
		return log.Code("bad_script").Errorf("a script starts with #!/usr/bin/env bash, then set -euo pipefail")
	}
	return nil
}

func (c *Control) scriptsKV(ctx context.Context) (jetstream.KeyValue, error) {
	kv, err := c.JS.CreateOrUpdateKeyValue(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: ScriptsBucket, Description: "Signed scripts, by content hash", History: 1, Storage: jetstream.FileStorage}))
	return kv, log.Wrap(err)
}

// PutScript checks, signs and keeps a script, and returns its hash.
func (c *Control) PutScript(ctx context.Context, key ed25519.PrivateKey, content string) (string, error) {
	if err := CheckScript(content); err != nil {
		return "", err
	}
	kv, err := c.scriptsKV(ctx)
	if err != nil {
		return "", err
	}
	hash := HashOf([]byte(content))
	raw, _ := json.Marshal(Script{Content: content, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(hash))), Key: PublicKey(key)})
	_, err = kv.Put(ctx, ScriptKey(hash), raw)
	return hash, log.Wrap(err)
}

// FetchScript reads a script and checks it: its bytes must hash to its
// name and its signature must be by one of keys (base64 public keys).
func (c *Control) FetchScript(ctx context.Context, hash string, keys []string) ([]byte, error) {
	if !strings.HasPrefix(hash, "sha256:") {
		return nil, log.Code("bad_hash").Errorf("a script is named sha256:<hex>")
	}
	kv, err := c.JS.KeyValue(ctx, ScriptsBucket)
	if err != nil {
		return nil, log.Code("no_script").Wrapf(err, "no script store")
	}
	e, err := kv.Get(ctx, ScriptKey(hash))
	if err != nil {
		return nil, log.Code("no_script").Wrapf(err, "no script %s", hash)
	}
	var s Script
	if err := json.Unmarshal(e.Value(), &s); err != nil {
		return nil, log.Wrap(err)
	}
	return VerifyScript(hash, s, keys)
}

// VerifyScript checks a script against its hash and the trusted keys.
func VerifyScript(hash string, s Script, keys []string) ([]byte, error) {
	if HashOf([]byte(s.Content)) != hash {
		return nil, log.Code("script_changed").Errorf("script %s does not match its hash", hash)
	}
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return nil, log.Code("bad_signature").Errorf("script %s has no valid signature", hash)
	}
	for _, k := range keys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, []byte(hash), sig) {
			return []byte(s.Content), nil
		}
	}
	return nil, log.Code("bad_signature").Errorf("script %s is not signed by a trusted key", hash)
}
