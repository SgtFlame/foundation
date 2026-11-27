package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/spf13/cobra"
)

// secretsRotateCallKeyCmd rotates the key that signs call tokens (forge
// 21b): a new current key, the old one kept as previous so tokens made
// before stay good until they expire. Run it twice, at least
// services.TokenTTL apart, to drop the old key. Without Vault the keys are
// <data-dir>/call.key; with Vault they are server/call-key (forge 21h).
var secretsRotateCallKeyCmd = &cobra.Command{
	Use:   "rotate-call-key",
	Short: "Rotate the key that signs service call tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := secretsDir()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "call.key")
		keys, err := services.LoadOrCreateKeys(path)
		if err != nil {
			return err
		}
		next, err := keys.Rotate()
		if err != nil {
			return err
		}
		if err := services.SaveKeys(path, next); err != nil {
			return err
		}
		fmt.Printf("rotated %s; tokens of the old key are accepted until the next rotation. Restart every process.\n", path)
		return nil
	},
}

func init() {
	secretsCmd.AddCommand(secretsRotateCallKeyCmd)
}
