package vaultstore

import (
	"context"
	"errors"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault/vaulttest"
)

func TestStore(t *testing.T) {
	fake := vaulttest.New(t)
	client, _ := vault.New(fake.URL, fake.Token)
	var lost []error
	legacy, _ := config.NewCipher(make([]byte, 32))
	s := New(client.KV("foundation"), legacy, func(_ context.Context, err error) { lost = append(lost, err) })
	ctx := context.Background()
	ref := config.SecretRef{Scope: "user:2", Section: "ai.providers", Path: "gemini.apiKey"}

	stored, err := s.Put(ctx, ref, "AIza-mine")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := fake.Value("config/user:2/ai.providers/gemini.apiKey"); v != "AIza-mine" {
		t.Errorf("in Vault: %q", v)
	}
	if stored[config.VaultField].(map[string]any)["version"] != 1 {
		t.Errorf("placeholder %v", stored)
	}

	// A cached value survives Vault going away; an uncached one reports it
	fake.Down = true
	if v, err := s.Get(ctx, ref, stored); err != nil || v != "AIza-mine" {
		t.Errorf("cached: %q, %v", v, err)
	}
	other := config.SecretRef{Scope: "app", Section: "ai.providers", Path: "gemini.apiKey"}
	if _, err := s.Get(ctx, other, stored); err == nil || len(lost) != 1 {
		t.Errorf("uncached while down: %v, lost %v", err, lost)
	}
	fake.Down = false

	// A missing secret is not a lost Vault
	if _, err := s.Get(ctx, other, stored); !errors.Is(err, vault.ErrNotFound) || len(lost) != 1 {
		t.Errorf("missing: %v, lost %v", err, lost)
	}

	// Values sealed before Vault still open
	sealed, _ := legacy.Encrypt(other.Section, other.Path, "AIza-old")
	if v, err := s.Get(ctx, other, map[string]any{config.SecretField: sealed}); err != nil || v != "AIza-old" {
		t.Errorf("legacy: %q, %v", v, err)
	}

	if err := s.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.Value("config/user:2/ai.providers/gemini.apiKey"); ok {
		t.Error("still in Vault after Delete")
	}
}
