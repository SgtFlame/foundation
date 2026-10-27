package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	secretsDataDir string
	secretsDB      string
	secretsAppDB   string
	secretsMount   string
)

// secretsCmd groups the operator's commands for the server's secrets
// (documentation/plans/Secrets.md). They use the operator's own VAULT_ADDR
// and VAULT_TOKEN, and should run while serve is stopped.
var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Move the server's secrets into Vault, and rotate the database key",
	Long: `Maintain the server's secrets. The scripts in backend/scripts/secrets/ call
these; run them while serve is stopped. They use the operator's VAULT_ADDR
and VAULT_TOKEN.`,
}

var secretsMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move secrets sealed with the key file into Vault",
	Long: `Move every secret setting sealed with the key file ($FOUNDATION_SECRET_KEY
or <data-dir>/secret.key) into Vault, at <mount>/config/<scope>/<section>/<path>,
and re-seal the JWT signing key with Vault's server/db-key (made by init.sh).
It is idempotent. Afterwards, start serve with --vault-addr and delete the
key file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		kv, err := operatorKV()
		if err != nil {
			return err
		}
		dataDir, err := secretsDir()
		if err != nil {
			return err
		}
		keyFile := filepath.Join(dataDir, "secret.key")
		if _, err := os.Stat(keyFile); errors.Is(err, os.ErrNotExist) && os.Getenv(config.SecretKeyEnv) == "" {
			return fmt.Errorf("no key file at %s and no $%s: nothing is sealed with it", keyFile, config.SecretKeyEnv)
		}
		old, err := config.LoadCipher(dataDir)
		if err != nil {
			return err
		}
		next, err := vaultCipher(ctx, kv)
		if err != nil {
			return err
		}
		appDB, tenantDB, err := secretsDBs(dataDir)
		if err != nil {
			return err
		}
		moved, err := secrets.MigrateSettings(ctx, tenantDB, old, kv)
		if err != nil {
			return err
		}
		resealed, err := secrets.Reseal(ctx, appDB, tenantDB, old, next)
		if err != nil {
			return err
		}
		fmt.Printf("moved %d secret settings into Vault and re-sealed %d values with server/db-key\n", moved, resealed)
		fmt.Printf("start serve with --vault-addr, check it, then delete %s\n", keyFile)
		return nil
	},
}

var secretsRekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Rotate the database key and re-seal what it seals",
	Long: `Make a new database key and re-seal the JWT signing key (and any secret
setting still sealed) under it. With VAULT_ADDR the key is Vault's
server/db-key, which gets a new version; without it, the key file. Restart
serve afterwards.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		dataDir, err := secretsDir()
		if err != nil {
			return err
		}
		appDB, tenantDB, err := secretsDBs(dataDir)
		if err != nil {
			return err
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		encoded := base64.StdEncoding.EncodeToString(key)
		next, err := config.NewCipher(key)
		if err != nil {
			return err
		}

		if os.Getenv("VAULT_ADDR") == "" {
			if os.Getenv(config.SecretKeyEnv) != "" {
				return fmt.Errorf("the key is $%s: rotate it where it is set", config.SecretKeyEnv)
			}
			old, err := config.LoadCipher(dataDir)
			if err != nil {
				return err
			}
			n, err := secrets.Reseal(ctx, appDB, tenantDB, old, next)
			if err != nil {
				return err
			}
			path := filepath.Join(dataDir, "secret.key")
			if err := os.WriteFile(path+".new", []byte(encoded+"\n"), 0o600); err != nil {
				return err
			}
			if err := os.Rename(path+".new", path); err != nil {
				return err
			}
			fmt.Printf("re-sealed %d values; the new key is in %s\n", n, path)
			return nil
		}

		kv, err := operatorKV()
		if err != nil {
			return err
		}
		old, err := vaultCipher(ctx, kv)
		if err != nil {
			return err
		}
		n, err := secrets.Reseal(ctx, appDB, tenantDB, old, next)
		if err != nil {
			return err
		}
		version, err := kv.Put(ctx, "server/db-key", map[string]any{"value": encoded})
		if err != nil {
			// Put what was re-sealed back under the key Vault still has
			if _, back := secrets.Reseal(ctx, appDB, tenantDB, next, old); back != nil {
				return fmt.Errorf("cannot write the new key to Vault (%w), nor re-seal back (%v): delete the signing_keys row so serve makes a new key (everyone signs in again)", err, back)
			}
			return fmt.Errorf("cannot write the new key to Vault, so nothing changed: %w", err)
		}
		_ = kv.SetCustomMetadata(ctx, "server/db-key", map[string]string{"updated_by": "operator:" + os.Getenv("USER")})
		fmt.Printf("re-sealed %d values; server/db-key is now version %d. Restart serve.\n", n, version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(secretsCmd)
	secretsCmd.AddCommand(secretsMigrateCmd, secretsRekeyCmd)
	secretsCmd.PersistentFlags().StringVar(&secretsDataDir, "data-dir", "", "serve's data directory (default ~/.foundation)")
	secretsCmd.PersistentFlags().StringVar(&secretsDB, "db", "", "the tenant database (default <data-dir>/foundation.db)")
	secretsCmd.PersistentFlags().StringVar(&secretsAppDB, "app-db", "", "the application database (default <data-dir>/app.db)")
	secretsCmd.PersistentFlags().StringVar(&secretsMount, "vault-mount", "foundation", "Vault KV v2 mount of the server's secrets")
}

// operatorKV is the server's KV mount as the operator's token sees it.
func operatorKV() (vault.KV, error) {
	addr, token := os.Getenv("VAULT_ADDR"), os.Getenv("VAULT_TOKEN")
	if addr == "" || token == "" {
		return vault.KV{}, errors.New("set VAULT_ADDR and VAULT_TOKEN to the Vault and your own token")
	}
	client, err := vault.New(addr, token)
	if err != nil {
		return vault.KV{}, err
	}
	return client.KV(secretsMount), nil
}

// vaultCipher uses Vault's server/db-key.
func vaultCipher(ctx context.Context, kv vault.KV) (*config.Cipher, error) {
	s, err := kv.Get(ctx, "server/db-key")
	if err != nil {
		return nil, fmt.Errorf("reading server/db-key (run init.sh first): %w", err)
	}
	encoded, _ := s.Data["value"].(string)
	return config.CipherFrom(encoded)
}

func secretsDir() (string, error) {
	if secretsDataDir != "" {
		return secretsDataDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".foundation"), nil
}

func secretsDBs(dataDir string) (appDB, tenantDB *gorm.DB, err error) {
	appPath, tenantPath := secretsAppDB, secretsDB
	if appPath == "" {
		appPath = filepath.Join(dataDir, "app.db")
	}
	if tenantPath == "" {
		tenantPath = filepath.Join(dataDir, "foundation.db")
	}
	for _, p := range []string{appPath, tenantPath} {
		if _, err := os.Stat(p); err != nil {
			return nil, nil, fmt.Errorf("no database at %s: %w", p, err)
		}
	}
	if appDB, err = db.Open(db.WithPath(appPath)); err != nil {
		return nil, nil, err
	}
	tenantDB, err = db.Open(db.WithPath(tenantPath))
	return appDB, tenantDB, err
}
