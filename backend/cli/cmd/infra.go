package cmd

import (
	"github.com/SgtFlame/foundation/backend/features/infra/infracli"
	"github.com/spf13/cobra"
)

// infraCmd groups the operator's commands for Infrastructure as Code
// (forge iteration 22, documentation/Infrastructure.md).
var infraCmd = &cobra.Command{
	Use:   "infra",
	Short: "Infrastructure as Code",
}

func init() {
	infraCmd.AddCommand(infracli.ImportCommand("import <dir>"))
	rootCmd.AddCommand(infraCmd)
}
