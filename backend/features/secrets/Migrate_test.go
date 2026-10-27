package secrets

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault/vaulttest"
	"gorm.io/gorm"
)

func cipher(t *testing.T, b byte) *config.Cipher {
	key := make([]byte, 32)
	key[0] = b
	c, err := config.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func openDBs(t *testing.T) (appDB, tenantDB *gorm.DB) {
	dir := t.TempDir()
	var err error
	if appDB, err = db.Open(db.WithPath(filepath.Join(dir, "app.db"))); err != nil {
		t.Fatal(err)
	}
	if tenantDB, err = db.Open(db.WithPath(filepath.Join(dir, "foundation.db"))); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, identity.Models()...); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(tenantDB, config.Models()...); err != nil {
		t.Fatal(err)
	}
	return appDB, tenantDB
}

// seed stores a signing key and a setting with secrets in an object and
// an array, all sealed with c.
func seed(t *testing.T, appDB, tenantDB *gorm.DB, c *config.Cipher) {
	sealed, _ := c.Encrypt("identity", "signingKey", "signing")
	appDB.Create(&identity.SigningKey{Sealed: sealed})
	a, _ := c.Encrypt("s", "a.key", "secret-a")
	b, _ := c.Encrypt("s", "list.1.key", "secret-b")
	value, _ := json.Marshal(map[string]any{
		"a":    map[string]any{"key": map[string]any{config.SecretField: a}, "plain": 1},
		"list": []any{map[string]any{"name": "x"}, map[string]any{"key": map[string]any{config.SecretField: b}}},
	})
	row := config.Configuration{Scope: "app", Section: "s", Value: string(value), Revision: 1}
	tenantDB.Create(&row)
	tenantDB.Create(&config.ConfigurationRevision{ConfigurationID: row.ID, Revision: 1, Value: row.Value})
}

func TestReseal(t *testing.T) {
	appDB, tenantDB := openDBs(t)
	from, to := cipher(t, 1), cipher(t, 2)
	seed(t, appDB, tenantDB, from)
	ctx := context.Background()

	n, err := Reseal(ctx, appDB, tenantDB, from, to)
	if err != nil || n != 3 {
		t.Fatalf("resealed %d, %v", n, err)
	}
	var key identity.SigningKey
	appDB.First(&key)
	if plain, err := to.Decrypt("identity", "signingKey", key.Sealed); err != nil || plain != "signing" {
		t.Errorf("signing key: %q, %v", plain, err)
	}
	var row config.Configuration
	tenantDB.First(&row)
	value, _ := config.Decode([]byte(row.Value))
	stored, _ := config.Get(value, "list.1.key")
	if plain, err := to.Decrypt("s", "list.1.key", stored.(map[string]any)[config.SecretField].(string)); err != nil || plain != "secret-b" {
		t.Errorf("array secret: %q, %v", plain, err)
	}
	if n, err := Reseal(ctx, appDB, tenantDB, from, to); err != nil || n != 0 {
		t.Errorf("second run resealed %d, %v", n, err)
	}
}

func TestMigrateSettings(t *testing.T) {
	appDB, tenantDB := openDBs(t)
	old := cipher(t, 1)
	seed(t, appDB, tenantDB, old)
	fake := vaulttest.New(t)
	client, _ := vault.New(fake.URL, fake.Token)
	ctx := context.Background()

	moved, err := MigrateSettings(ctx, tenantDB, old, client.KV("foundation"))
	if err != nil || moved != 2 {
		t.Fatalf("moved %d, %v", moved, err)
	}
	if v, _ := fake.Value("config/app/s/list.1.key"); v != "secret-b" {
		t.Errorf("in Vault: %q", v)
	}
	var row config.Configuration
	tenantDB.First(&row)
	var rev config.ConfigurationRevision
	tenantDB.First(&rev)
	for _, v := range []string{row.Value, rev.Value} {
		if strings.Contains(v, config.SecretField) || !strings.Contains(v, config.VaultField) {
			t.Errorf("left %s", v)
		}
	}
	if moved, err := MigrateSettings(ctx, tenantDB, old, client.KV("foundation")); err != nil || moved != 0 {
		t.Errorf("second run moved %d, %v", moved, err)
	}
}
