package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

func newTestService(t *testing.T, opts ...Option) (*service, *config.Changes) {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, config.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	aisettings.Contribute(reg)
	cipher, err := config.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	changes := config.NewChanges()
	return newService(append([]Option{WithDB(gdb), WithRegistry(reg), WithCipher(cipher), WithChanges(changes)}, opts...)...), changes
}

var local = features.ServiceArgs{Principal: features.LocalPrincipal()}

func set(t *testing.T, s *service, section, patch string, revision int64) configservice.SetResponse {
	t.Helper()
	return s.Set(configservice.SetArgs{ServiceArgs: local, Section: section, Scope: configservice.ScopeApp, Patch: json.RawMessage(patch), Revision: revision})
}

func TestDefaultsAndSources(t *testing.T) {
	s, _ := newTestService(t)
	resp := s.Get(configservice.GetArgs{ServiceArgs: local, Section: aisettings.LevelsID, Scope: configservice.ScopeApp})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("get: %d %s", resp.Common.Code, resp.Common.Message)
	}
	if levels := resp.Value["levels"].([]any); len(levels) != 3 || resp.Sources["levels"] != configservice.SourceDefault || resp.Revision != 0 {
		t.Errorf("view = %+v", resp.View)
	}
	if code := s.Get(configservice.GetArgs{ServiceArgs: local, Section: "nope"}).Common.Code; code != http.StatusNotFound {
		t.Errorf("unknown section: %d", code)
	}
}

func TestSecretsAreSealedAndRedacted(t *testing.T) {
	s, changes := newTestService(t)
	var seen []config.Changed
	changes.Subscribe(func(c config.Changed) { seen = append(seen, c) })

	resp := set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "AIza-secret-key-1234"}}`, 0)
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("set: %d %s %+v", resp.Common.Code, resp.Common.Message, resp.Errors)
	}
	shown := resp.Value["gemini"].(map[string]any)["apiKey"].(map[string]any)
	if shown["set"] != true || shown["hint"] != "…1234" || resp.Sources["gemini.apiKey"] != "app" || resp.Revision != 1 {
		t.Errorf("view = %+v", resp.View)
	}
	if len(seen) != 1 || seen[0] != (config.Changed{Section: aisettings.ProvidersID, Scope: "app"}) {
		t.Errorf("changes = %+v", seen)
	}

	var row config.Configuration
	if err := s.db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.Value, "AIza") || !strings.Contains(row.Value, config.SecretField) {
		t.Errorf("stored %s", row.Value)
	}

	got, err := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID)
	if err != nil || got.Gemini.APIKey != "AIza-secret-key-1234" {
		t.Errorf("read %+v, %v", got, err)
	}
}

func TestEnvWinsForSecretsOnly(t *testing.T) {
	s, _ := newTestService(t,
		WithEnv(aisettings.ProvidersID, "gemini.apiKey", "from-env-key-9999"),
		WithEnv(aisettings.LevelsID, "levels", []any{map[string]any{"id": "only", "label": "Only", "modelRef": "gemini-2.5-pro", "default": true}}))

	set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "stored-key-00000000"}}`, 0)
	got, _ := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID)
	if got.Gemini.APIKey != "from-env-key-9999" {
		t.Errorf("secret = %q, want the env value", got.Gemini.APIKey)
	}

	levels, _ := config.Read[aisettings.Levels](s, local, aisettings.LevelsID)
	if len(levels.Levels) != 1 {
		t.Fatalf("env levels not applied over defaults: %+v", levels)
	}
	set(t, s, aisettings.LevelsID, `{"levels": [{"id": "a", "label": "A", "modelRef": "gemini-2.5-flash", "default": true}]}`, 0)
	levels, _ = config.Read[aisettings.Levels](s, local, aisettings.LevelsID)
	if len(levels.Levels) != 1 || levels.Levels[0].ID != "a" {
		t.Errorf("stored levels should win over env: %+v", levels)
	}
}

func TestValidation(t *testing.T) {
	s, _ := newTestService(t)
	for name, tc := range map[string]struct{ section, patch, path string }{
		"unknown key":  {aisettings.ProvidersID, `{"gemini": {"nope": 1}}`, "gemini.nope"},
		"wrong type":   {aisettings.LevelsID, `{"levels": [{"id": "a", "label": "A", "modelRef": "gemini-2.5-pro", "default": true, "temperature": "hot"}]}`, "levels.0.temperature"},
		"bad ref":      {aisettings.LevelsID, `{"levels": [{"id": "a", "label": "A", "modelRef": "gpt-9", "default": true}]}`, "levels.0.modelRef"},
		"two defaults": {aisettings.LevelsID, `{"levels": [{"id": "a", "label": "A", "modelRef": "gemini-2.5-pro", "default": true}, {"id": "b", "label": "B", "modelRef": "gemini-2.5-pro", "default": true}]}`, "levels"},
		"repeated id":  {aisettings.LevelsID, `{"levels": [{"id": "a", "label": "A", "modelRef": "gemini-2.5-pro", "default": true}, {"id": "a", "label": "B", "modelRef": "gemini-2.5-pro"}]}`, "levels.1.id"},
		"bad scope":    {aisettings.ProvidersID, `{}`, ""},
	} {
		if name == "bad scope" {
			// The model catalog is the app's alone
			resp := s.Set(configservice.SetArgs{ServiceArgs: local, Section: aisettings.ModelsID, Scope: configservice.ScopeUser, Patch: json.RawMessage(`{"models": []}`)})
			if resp.Common.Code != http.StatusBadRequest || len(resp.Errors) != 1 || resp.Errors[0].Path != "models" {
				t.Errorf("%s: %d %+v", name, resp.Common.Code, resp.Errors)
			}
			continue
		}
		resp := set(t, s, tc.section, tc.patch, 0)
		if resp.Common.Code != http.StatusBadRequest || len(resp.Errors) == 0 || resp.Errors[0].Path != tc.path || resp.Common.ErrorCode != "invalid_settings" {
			t.Errorf("%s: %d %+v", name, resp.Common.Code, resp.Errors)
		}
	}
}

func TestConflictAndReset(t *testing.T) {
	s, _ := newTestService(t)
	if resp := set(t, s, aisettings.LevelsID, `{"levels": [{"id": "a", "label": "A", "modelRef": "gemini-2.5-pro", "default": true}]}`, 0); resp.Revision != 1 {
		t.Fatalf("first set: %+v", resp)
	}
	if code := set(t, s, aisettings.LevelsID, `{"levels": null}`, 0).Common.Code; code != http.StatusConflict {
		t.Errorf("stale revision: %d", code)
	}
	resp := s.Reset(configservice.ResetArgs{ServiceArgs: local, Section: aisettings.LevelsID, Scope: configservice.ScopeApp, Keys: []string{"levels"}})
	if resp.Common.Code != http.StatusOK || resp.Sources["levels"] != configservice.SourceDefault || resp.Revision != 2 {
		t.Errorf("reset: %+v", resp)
	}
	var revisions int64
	s.db.Model(&config.ConfigurationRevision{}).Count(&revisions)
	if revisions != 2 {
		t.Errorf("revisions = %d", revisions)
	}
}

func TestUndecryptableSecretIsDropped(t *testing.T) {
	s, _ := newTestService(t)
	set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "AIza-secret-key-1234"}}`, 0)
	other := make([]byte, 32)
	other[0] = 1
	cipher, _ := config.NewCipher(other)
	s.secrets = config.CipherStore{Cipher: cipher}
	s.cache = map[string][]byte{}
	got, err := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID)
	if err != nil || got.Gemini.APIKey != "" {
		t.Errorf("read %+v, %v", got, err)
	}
}

// memStore is a SecretStore like Vault's: values kept outside the row,
// counting its writes.
type memStore struct {
	values  map[string]string
	puts    int
	deleted []string
}

func (m *memStore) Put(_ context.Context, ref config.SecretRef, value string) (map[string]any, error) {
	m.puts++
	m.values[ref.Name()] = value
	return map[string]any{config.VaultField: map[string]any{"version": m.puts}}, nil
}

func (m *memStore) Get(_ context.Context, ref config.SecretRef, _ map[string]any) (string, error) {
	v, ok := m.values[ref.Name()]
	if !ok {
		return "", fmt.Errorf("%s is not stored", ref.Name())
	}
	return v, nil
}

func (m *memStore) Delete(_ context.Context, ref config.SecretRef) error {
	delete(m.values, ref.Name())
	m.deleted = append(m.deleted, ref.Name())
	return nil
}

func (m *memStore) Hints() bool { return false }

func TestStoreKeepsUnchangedSecrets(t *testing.T) {
	store := &memStore{values: map[string]string{}}
	s, _ := newTestService(t, WithSecrets(store))
	resp := set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "AIza-secret-key-1234"}}`, 0)
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("set: %d %s", resp.Common.Code, resp.Common.Message)
	}
	if shown := resp.Value["gemini"].(map[string]any)["apiKey"].(map[string]any); shown["set"] != true || shown["hint"] != nil {
		t.Errorf("shown %+v, want set with no hint", shown)
	}
	if store.values["config/app/ai.providers/gemini.apiKey"] != "AIza-secret-key-1234" {
		t.Errorf("store = %+v", store.values)
	}
	var row config.Configuration
	if err := s.db.First(&row).Error; err != nil || strings.Contains(row.Value, "AIza") || !strings.Contains(row.Value, config.VaultField) {
		t.Errorf("stored %s, %v", row.Value, err)
	}

	// Saving the section again with the same key writes no new version
	s.cache = map[string][]byte{}
	if got, _ := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID); got.Gemini.APIKey != "AIza-secret-key-1234" {
		t.Errorf("read %q", got.Gemini.APIKey)
	}
	if resp := set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "AIza-secret-key-1234"}}`, 1); resp.Common.Code != http.StatusOK || store.puts != 1 {
		t.Errorf("same key: %d, puts %d", resp.Common.Code, store.puts)
	}
	if resp := set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": null}}`, 2); resp.Common.Code != http.StatusOK || len(store.deleted) != 1 {
		t.Errorf("cleared: %d, deleted %v", resp.Common.Code, store.deleted)
	}
}

func TestUnreadableSecretIsNotCached(t *testing.T) {
	store := &memStore{values: map[string]string{}}
	s, _ := newTestService(t, WithSecrets(store))
	set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "AIza-secret-key-1234"}}`, 0)
	s.cache = map[string][]byte{}
	kept := store.values
	store.values = map[string]string{} // Vault is unreachable
	if got, _ := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID); got.Gemini.APIKey != "" {
		t.Errorf("read %q while unreachable", got.Gemini.APIKey)
	}
	store.values = kept
	if got, _ := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID); got.Gemini.APIKey != "AIza-secret-key-1234" {
		t.Errorf("read %q once back", got.Gemini.APIKey)
	}
}

func TestServerSecretSitsUnderScopes(t *testing.T) {
	s, _ := newTestService(t, WithServerSecret(aisettings.ProvidersID, "gemini.apiKey", "AIza-from-vault-0000"))
	if got, _ := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID); got.Gemini.APIKey != "AIza-from-vault-0000" {
		t.Errorf("server key: %q", got.Gemini.APIKey)
	}
	resp := s.Get(configservice.GetArgs{ServiceArgs: local, Section: aisettings.ProvidersID, Scope: configservice.ScopeApp})
	if resp.Sources["gemini.apiKey"] != configservice.SourceServer {
		t.Errorf("source %q", resp.Sources["gemini.apiKey"])
	}
	set(t, s, aisettings.ProvidersID, `{"gemini": {"apiKey": "AIza-secret-key-1234"}}`, 0)
	if got, _ := config.Read[aisettings.Providers](s, local, aisettings.ProvidersID); got.Gemini.APIKey != "AIza-secret-key-1234" {
		t.Errorf("stored key should win: %q", got.Gemini.APIKey)
	}
}
