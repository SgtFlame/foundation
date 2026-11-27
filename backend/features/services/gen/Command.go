package gen

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Command is `gen` with its `services` subcommand, for a module's CLI
// (Foundation's and Forge's): it regenerates every service's proxy and
// server under the module root (the working directory, else --root), or
// with --check lists what is stale and fails.
func Command() *cobra.Command {
	var root string
	var check bool
	services := &cobra.Command{
		Use:   "services",
		Short: "Generate every service's proxy and server (forge 21a-2)",
		Long: `Read every features/**/services/<s>service/Service.go and write, beside it,
proxy/proxy.go and server/server.go, and features/services/catalog. Run it
from the module root (backend) after adding or changing an operation.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				root = wd
			}
			o := Options{Root: root}
			if check {
				stale, err := Check(o)
				if err != nil {
					return err
				}
				if len(stale) > 0 {
					return fmt.Errorf("generated services are stale; run cli gen services:\n  %s", strings.Join(stale, "\n  "))
				}
				fmt.Fprintln(cmd.OutOrStdout(), "generated services are current")
				return nil
			}
			files, err := Write(o)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %d files\n", len(files))
			return nil
		},
	}
	services.Flags().StringVar(&root, "root", "", "the module root (default: the working directory)")
	services.Flags().BoolVar(&check, "check", false, "list stale files and fail, writing nothing")
	gen := &cobra.Command{Use: "gen", Short: "Generate code"}
	gen.AddCommand(services)
	return gen
}
