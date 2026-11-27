package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/spf13/cobra"
)

var (
	cpDataDir string
	cpAppDB   string
	cpUser    uint64
)

// controlplaneCmd groups the operator's commands for the control plane
// (forge iteration 20, documentation/ControlPlane.md).
var controlplaneCmd = &cobra.Command{
	Use:   "controlplane",
	Short: "Set up the control plane",
}

var controlplaneInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Make the root tenant, the only tenant that holds infrastructure",
	Long: `Make the root tenant (id 3, Infrastructure) in serve's application database
(forge 20b-1), with --user its member and Owner, so the only infrastructure
administrator is whoever sets it up; not even a Super Administrator holds
infrastructure otherwise. Run again with another --user to add an Owner.
serve --control-plane makes the root tenant too, with user 1 its Owner.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := cpDataDir
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			dir = filepath.Join(home, ".foundation")
		}
		path := cpAppDB
		if path == "" {
			path = filepath.Join(dir, "app.db")
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("no application database at %s (run serve once first): %w", path, err)
		}
		appDB, err := db.Open(db.WithPath(path))
		if err != nil {
			return err
		}
		if err := db.Migrate(appDB, append(identity.Models(), authz.Models()...)...); err != nil {
			return err
		}
		reg := extensions.NewRegistry()
		authz.Contribute(reg)
		identity.ContributeAccess(reg)
		controlplane.ContributeAccess(reg)
		authz.Install(authz.NewPolicy(appDB, reg))
		made, err := controlplane.EnsureRootTenant(cmd.Context(), appDB, cpUser)
		if err != nil {
			return err
		}
		if made {
			fmt.Fprintf(cmd.OutOrStdout(), "made the root tenant %d with user %d its Owner\n", features.RootTenantID, cpUser)
			return nil
		}
		if err := controlplane.AddRootOwner(cmd.Context(), appDB, cpUser); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "user %d is an Owner of the root tenant %d\n", cpUser, features.RootTenantID)
		return nil
	},
}

var (
	cpTarget, cpKeys, cpNatsURL, cpNatsCA string
	cpTTL                                 time.Duration
)

var controlplaneEnrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "Mint a one-time enrollment token for a daemon",
	Long: `Mint a one-time token for the daemon of --target (host.<id> or inst.<id>,
the infrastructure record's id; forge 20c-1), printed once: only its hash is
kept, in the control account's ctl_enroll bucket. On the machine, run
  axiomd enroll --token <token> --url <nats> --enroll-creds <keys>/enroll.creds
which exchanges it for creds scoped to that target alone. It connects to the
control account with --keys (serve --nats-keys).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		t, ok := ctl.TargetOfKey(cpTarget)
		if !ok {
			return fmt.Errorf("--target must be host.<id> or inst.<id>")
		}
		o := natsd.Options{URL: cpNatsURL, CA: cpNatsCA}.WithEnv()
		conn, err := natsauth.AccountConn(o, cpKeys, natsauth.Control, "cli controlplane enroll")
		if err != nil {
			return err
		}
		if conn == nil {
			return fmt.Errorf("--keys (or $%s) and --nats-url (or $%s) are needed", natsauth.EnvKeys, natsd.EnvURL)
		}
		defer conn.Close()
		c := &ctl.Control{NC: conn.Conn(), JS: conn.JS()}
		tok, err := c.MintToken(cmd.Context(), t, cpTTL)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), tok)
		return nil
	},
}

func init() {
	controlplaneEnrollCmd.Flags().StringVar(&cpTarget, "target", "", "the daemon's target: host.<id> or inst.<id>")
	controlplaneEnrollCmd.Flags().StringVar(&cpKeys, "keys", "", "the deployment's keys (default $FOUNDATION_NATS_KEYS)")
	controlplaneEnrollCmd.Flags().StringVar(&cpNatsURL, "nats-url", "", "the cluster's URLs (default $FOUNDATION_NATS_URL)")
	controlplaneEnrollCmd.Flags().StringVar(&cpNatsCA, "nats-ca", "", "the deployment's CA certificate (default $FOUNDATION_NATS_CA)")
	controlplaneEnrollCmd.Flags().DurationVar(&cpTTL, "ttl", time.Hour, "how long the token may be used")
	_ = controlplaneEnrollCmd.MarkFlagRequired("target")
	controlplaneCmd.AddCommand(controlplaneEnrollCmd)
	controlplaneInitCmd.Flags().StringVar(&cpDataDir, "data-dir", "", "serve's data directory (default ~/.foundation)")
	controlplaneInitCmd.Flags().StringVar(&cpAppDB, "app-db", "", "the application database (default <data-dir>/app.db)")
	controlplaneInitCmd.Flags().Uint64Var(&cpUser, "user", 0, "the user who becomes the root tenant's Owner")
	_ = controlplaneInitCmd.MarkFlagRequired("user")
	controlplaneCmd.AddCommand(controlplaneInitCmd)
	rootCmd.AddCommand(controlplaneCmd)
}
