// Package vaultstore keeps secret settings in Vault's KV v2 mount, at
// config/<scope>/<section>/<path> (documentation/plans/Secrets.md ›
// SecretStore). A Configuration value holds only {"$vault": {"version": n}}.
package vaultstore

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
)

// UpdatedBy is the custom metadata key naming who wrote a secret:
// "user:<id>", or "operator:<name>" from the scripts.
const UpdatedBy = "updated_by"

// Store is a config.SecretStore over a KV v2 mount. Values are cached in
// memory, so the server keeps running on them while Vault is unreachable.
type Store struct {
	kv vault.KV
	// legacy opens the {"$secret": …} values left from before Vault, until
	// `cli secrets migrate` moves them
	legacy *config.Cipher
	// lost is told about every failed request but a missing secret
	lost func(context.Context, error)

	mu    sync.RWMutex
	cache map[string]string // by secret name
}

// New keeps secrets in kv; legacy and lost may be nil.
func New(kv vault.KV, legacy *config.Cipher, lost func(context.Context, error)) *Store {
	if lost == nil {
		lost = func(context.Context, error) {}
	}
	return &Store{kv: kv, legacy: legacy, lost: lost, cache: map[string]string{}}
}

func (s *Store) Put(ctx context.Context, ref config.SecretRef, value string) (map[string]any, error) {
	name := ref.Name()
	version, err := s.kv.Put(ctx, name, map[string]any{"value": value})
	if err != nil {
		s.failed(ctx, err)
		return nil, err
	}
	if p, _, ok := features.CallFrom(ctx); ok {
		if err := s.kv.SetCustomMetadata(ctx, name, map[string]string{UpdatedBy: fmt.Sprintf("user:%d", p.UserID)}); err != nil {
			s.failed(ctx, err)
		}
	}
	s.mu.Lock()
	s.cache[name] = value
	s.mu.Unlock()
	return map[string]any{config.VaultField: map[string]any{"version": version}}, nil
}

func (s *Store) Get(ctx context.Context, ref config.SecretRef, stored map[string]any) (string, error) {
	if _, ok := stored[config.SecretField]; ok {
		return config.CipherStore{Cipher: s.legacy}.Get(ctx, ref, stored)
	}
	name := ref.Name()
	s.mu.RLock()
	v, ok := s.cache[name]
	s.mu.RUnlock()
	if ok {
		return v, nil
	}
	secret, err := s.kv.Get(ctx, name)
	if err != nil {
		s.failed(ctx, err)
		return "", err
	}
	v, _ = secret.Data["value"].(string)
	s.mu.Lock()
	s.cache[name] = v
	s.mu.Unlock()
	return v, nil
}

func (s *Store) Delete(ctx context.Context, ref config.SecretRef) error {
	name := ref.Name()
	s.mu.Lock()
	delete(s.cache, name)
	s.mu.Unlock()
	err := s.kv.Delete(ctx, name)
	s.failed(ctx, err)
	return err
}

// Hints is false: nothing of a value held in Vault is shown (Secrets
// question 4).
func (s *Store) Hints() bool { return false }

// Forget drops a cached value written elsewhere, such as by the Secrets
// editor.
func (s *Store) Forget(name string) {
	s.mu.Lock()
	delete(s.cache, name)
	s.mu.Unlock()
}

func (s *Store) failed(ctx context.Context, err error) {
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		s.lost(ctx, err)
	}
}
