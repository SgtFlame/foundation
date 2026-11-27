package cmd

import "github.com/SgtFlame/foundation/backend/features/services/gen"

func init() {
	rootCmd.AddCommand(gen.Command())
}
