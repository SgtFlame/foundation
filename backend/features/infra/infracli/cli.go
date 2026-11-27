// Package infracli is the operator's command for Infrastructure as Code
// that both Foundation's cli and Forge's add (forge 22o): importing a
// configuration draft into a running serve, as the seed does.
package infracli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/live/liveclient"
	"github.com/spf13/cobra"
)

// ImportCommand is the import command under the name use gives (import
// <dir> under Foundation's infra; infra-import <dir> in Forge, whose
// infra is its developer tool).
func ImportCommand(use string) *cobra.Command {
	var (
		infraServer   string
		infraUsername string
		infraConfig   string
		infraMessage  string
		infraApplied  bool
	)
	c := &cobra.Command{
		Use:   use,
		Short: "Write an export's YAML folder as the configuration's draft and commit it",
		Long: `Sign in to a running serve as --username (the password read from stdin,
never from the command line), switch to the root tenant, write the YAML
files under <dir> as the configuration's draft (infra.config.import) and
commit it with --message, marked applied with --applied (forge 22o: the
seed's first version describes what the seed made). Run again with the same
files it commits nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := infra.ReadFolder(args[0])
			if err != nil {
				return err
			}
			password, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && password == "" {
				return fmt.Errorf("reading the password from stdin: %w", err)
			}
			lc := liveclient.New(infraServer, infraUsername, strings.TrimRight(password, "\r\n"))
			defer lc.Close()
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			r, err := infra.SeedDraft(ctx, lc, infraConfig, files, infraMessage, infraApplied)
			if err != nil {
				return err
			}
			if r.Committed {
				fmt.Printf("committed version %d\n", r.Version)
			} else {
				fmt.Printf("the draft is version %d already\n", r.Version)
			}
			return nil
		},
	}
	c.Flags().StringVar(&infraServer, "server", "http://localhost:8080", "the server")
	c.Flags().StringVar(&infraUsername, "username", "", "the root tenant's administrator")
	c.Flags().StringVar(&infraConfig, "config", "", "the configuration (default main)")
	c.Flags().StringVar(&infraMessage, "message", "The seed", "the version's message")
	c.Flags().BoolVar(&infraApplied, "applied", false, "mark the version applied")
	return c
}
