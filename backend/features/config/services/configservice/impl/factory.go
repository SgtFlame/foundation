package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"gorm.io/gorm"
)

// Option configures the service created by CreateService.
type Option func(*service)

// WithDB sets the database holding the Configuration tables.
func WithDB(gdb *gorm.DB) Option {
	return func(s *service) {
		s.db = gdb
	}
}

// WithRegistry sets the registry whose config.Sections are served.
func WithRegistry(registry *extensions.Registry) Option {
	return func(s *service) {
		s.registry = registry
	}
}

// WithCipher keeps secrets sealed with cipher in the Configuration table
// (config.CipherStore).
func WithCipher(cipher *config.Cipher) Option {
	return func(s *service) {
		s.secrets = config.CipherStore{Cipher: cipher}
	}
}

// WithSecrets sets where secrets are kept, such as Vault (vaultstore).
func WithSecrets(store config.SecretStore) Option {
	return func(s *service) {
		s.secrets = store
	}
}

// WithServerSecret sets a secret of the server's own, from Vault's
// bootstrap bundle, for the leaf at path of section. Unlike WithEnv it sits
// under every stored scope, so a key a user sets wins over the server's.
// An empty string sets nothing.
func WithServerSecret(section, path, value string) Option {
	return func(s *service) {
		if value == "" {
			return
		}
		if s.server[section] == nil {
			s.server[section] = map[string]any{}
		}
		config.Set(s.server[section], path, value)
	}
}

// WithChanges sets the bus each stored change is published on.
func WithChanges(changes *config.Changes) Option {
	return func(s *service) {
		s.changes = changes
	}
}

// WithEnv sets a value from a flag or env var for the leaf at path of
// section. It sits under every stored scope, except for a secret, which it
// overrides everywhere. An empty string sets nothing.
func WithEnv(section, path string, value any) Option {
	return func(s *service) {
		if value == "" || value == nil {
			return
		}
		if s.env[section] == nil {
			s.env[section] = map[string]any{}
		}
		config.Set(s.env[section], path, value)
	}
}

var (
	instance *service
	once     sync.Once
)

// CreateService returns the singleton configservice.Service. Options are only
// applied by the first call; later calls return the existing instance.
func CreateService(opts ...Option) configservice.Service {
	once.Do(func() {
		instance = newService(opts...)
	})
	return instance
}

func newService(opts ...Option) *service {
	s := &service{registry: extensions.NewRegistry(), env: map[string]map[string]any{}, server: map[string]map[string]any{}, cache: map[string][]byte{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
