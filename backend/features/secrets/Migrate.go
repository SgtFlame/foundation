package secrets

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"gorm.io/gorm"
)

// MigrateSettings moves every secret setting still sealed in the tenant
// database ({"$secret": …}, opened with old) into kv, leaving
// {"$vault": {"version": n}} in its place. Revisions keep only
// {"$vault": {}}, so no sealed value is left behind. It is idempotent: a
// second run finds nothing to move.
func MigrateSettings(ctx context.Context, tenantDB *gorm.DB, old *config.Cipher, kv vault.KV) (moved int, err error) {
	var rows []config.Configuration
	if err := tenantDB.WithContext(ctx).Find(&rows).Error; err != nil {
		return 0, err
	}
	for _, row := range rows {
		value, err := config.Decode([]byte(row.Value))
		if err != nil {
			return moved, fmt.Errorf("%s at %s does not decode: %w", row.Section, row.Scope, err)
		}
		n := 0
		value, err = replaceSealed(value, "", func(path, sealed string) (any, error) {
			plain, err := old.Decrypt(row.Section, path, sealed)
			if err != nil {
				return nil, fmt.Errorf("%s %s at %s: %w", row.Section, path, row.Scope, err)
			}
			ref := config.SecretRef{Scope: row.Scope, Section: row.Section, Path: path}
			version, err := kv.Put(ctx, ref.Name(), map[string]any{"value": plain})
			if err != nil {
				return nil, err
			}
			if err := kv.SetCustomMetadata(ctx, ref.Name(), map[string]string{"updated_by": "operator:migrate"}); err != nil {
				return nil, err
			}
			n++
			return map[string]any{config.VaultField: map[string]any{"version": version}}, nil
		})
		if err != nil {
			return moved, err
		}
		if n == 0 {
			continue
		}
		raw, _ := json.Marshal(value)
		if err := tenantDB.WithContext(ctx).Model(&config.Configuration{}).Where("id = ?", row.ID).Update("value", string(raw)).Error; err != nil {
			return moved, err
		}
		moved += n
	}
	return moved, redactRevisions(ctx, tenantDB)
}

// redactRevisions replaces sealed values in the settings history.
func redactRevisions(ctx context.Context, tenantDB *gorm.DB) error {
	var revs []config.ConfigurationRevision
	if err := tenantDB.WithContext(ctx).Where("value LIKE ?", `%"`+config.SecretField+`"%`).Find(&revs).Error; err != nil {
		return err
	}
	for _, r := range revs {
		value, err := config.Decode([]byte(r.Value))
		if err != nil {
			continue
		}
		value, _ = replaceSealed(value, "", func(string, string) (any, error) {
			return map[string]any{config.VaultField: map[string]any{}}, nil
		})
		raw, _ := json.Marshal(value)
		if err := tenantDB.WithContext(ctx).Model(&config.ConfigurationRevision{}).Where("id = ?", r.ID).Update("value", string(raw)).Error; err != nil {
			return err
		}
	}
	return nil
}

// Reseal moves what is sealed with from to to: the JWT signing key in the
// application database and any secret setting still sealed in the tenant
// database. A value that already opens with to is left alone, so a run
// that stopped half way can be run again.
func Reseal(ctx context.Context, appDB, tenantDB *gorm.DB, from, to *config.Cipher) (resealed int, err error) {
	err = appDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var keys []identity.SigningKey
		if err := tx.Find(&keys).Error; err != nil {
			return err
		}
		for _, k := range keys {
			if _, err := to.Decrypt("identity", "signingKey", k.Sealed); err == nil {
				continue
			}
			plain, err := from.Decrypt("identity", "signingKey", k.Sealed)
			if err != nil {
				return fmt.Errorf("the signing key: %w", err)
			}
			sealed, err := to.Encrypt("identity", "signingKey", plain)
			if err != nil {
				return err
			}
			if err := tx.Model(&identity.SigningKey{}).Where("id = ?", k.ID).Update("sealed", sealed).Error; err != nil {
				return err
			}
			resealed++
		}
		return nil
	})
	if err != nil {
		return resealed, err
	}
	err = tenantDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []config.Configuration
		if err := tx.Where("value LIKE ?", `%"`+config.SecretField+`"%`).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			value, err := config.Decode([]byte(row.Value))
			if err != nil {
				return err
			}
			value, err = replaceSealed(value, "", func(path, sealed string) (any, error) {
				if _, err := to.Decrypt(row.Section, path, sealed); err == nil {
					return map[string]any{config.SecretField: sealed}, nil
				}
				plain, err := from.Decrypt(row.Section, path, sealed)
				if err != nil {
					return nil, fmt.Errorf("%s %s at %s: %w", row.Section, path, row.Scope, err)
				}
				next, err := to.Encrypt(row.Section, path, plain)
				resealed++
				return map[string]any{config.SecretField: next}, err
			})
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(value)
			if err := tx.Model(&config.Configuration{}).Where("id = ?", row.ID).Update("value", string(raw)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return resealed, err
}

// replaceSealed walks a stored value and replaces each {"$secret": s} with
// what fn returns for its dotted path (array items by index).
func replaceSealed(value map[string]any, path string, fn func(path, sealed string) (any, error)) (map[string]any, error) {
	out, err := replaceIn(value, path, fn)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func replaceIn(v any, path string, fn func(string, string) (any, error)) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		if sealed, ok := x[config.SecretField].(string); ok && len(x) == 1 {
			return fn(path, sealed)
		}
		for k, child := range x {
			next, err := replaceIn(child, config.Join(path, k), fn)
			if err != nil {
				return nil, err
			}
			x[k] = next
		}
	case []any:
		for i, child := range x {
			next, err := replaceIn(child, config.Join(path, fmt.Sprint(i)), fn)
			if err != nil {
				return nil, err
			}
			x[i] = next
		}
	}
	return v, nil
}
