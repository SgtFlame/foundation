package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/spf13/cobra"
)

var (
	natsURL      string
	natsCreds    string
	natsCA       string
	natsReplicas int
)

// natsCmd groups the operator's commands for deployed NATS (forge 20a,
// documentation/Live.md › Deployed NATS).
var natsCmd = &cobra.Command{
	Use:   "nats",
	Short: "Maintain the deployed NATS cluster the live core uses",
}

var natsReplicasCmd = &cobra.Command{
	Use:   "replicas",
	Short: "Set the replica count of every stream, KV bucket and object store",
	Long: `Set the replica count of every stream, KV bucket and object store in the
account the credentials reach, for a deployment grown from one node to a
cluster. JetStream copies the data; it is idempotent.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if natsReplicas < 1 {
			return fmt.Errorf("--set must be at least 1")
		}
		o := natsd.Options{URL: natsURL, Creds: natsCreds, CA: natsCA}.WithEnv()
		if o.URL == "" {
			return fmt.Errorf("--nats-url or $%s is needed", natsd.EnvURL)
		}
		s, err := natsd.Start(cmd.Context(), o)
		if err != nil {
			return err
		}
		defer s.Close()
		changed, err := natsd.SetReplicas(cmd.Context(), s.JS(), natsReplicas)
		for _, name := range changed {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d replicas\n", name, natsReplicas)
		}
		if err == nil && len(changed) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "every stream already has that many replicas")
		}
		return err
	},
}

var (
	natsKeysDir string
	natsAccount string
	natsUser    string
)

var natsInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Make the deployment's operator and its SYS, CTL and APP accounts",
	Long: `Make a new deployment's NATS trust chain (forge 20a-2): an operator, the
system account, the control account and the application account, each with
a signing key. It writes the keys to --keys (mode 0600), the trust lines for
every nats-server.conf to <keys>/trust.conf, and two creds files: serve.creds
(an APP user for serve) and sys.creds (a SYS user that pushes accounts).
Take operator.nk offline afterwards: accounts then mint users without it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		k := natsauth.DirKeys(natsKeysDir)
		if _, err := k.Get("operator.jwt"); err == nil {
			return fmt.Errorf("%s already holds a deployment", natsKeysDir)
		}
		d, err := natsauth.Init("axiom")
		if err != nil {
			return err
		}
		if err := d.Save(k); err != nil {
			return err
		}
		if err := k.Put("trust.conf", []byte(d.ServerConfig("/var/lib/nats/resolver"))); err != nil {
			return err
		}
		for _, u := range []struct{ account, name string }{{natsauth.App, "serve"}, {natsauth.System, "sys"}} {
			user, err := d.Account(u.account).NewUser(u.name, natsauth.Everything, 0)
			if err != nil {
				return err
			}
			if err := k.Put(u.name+".creds", user.Creds); err != nil {
				return err
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "deployment written to %s\n", natsKeysDir)
		return nil
	},
}

var natsUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Mint a user of an account and print its creds file",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := natsauth.Load(natsauth.DirKeys(natsKeysDir))
		if err != nil {
			return err
		}
		a := d.Account(natsAccount)
		if a == nil {
			return fmt.Errorf("no account %s", natsAccount)
		}
		u, err := a.NewUser(natsUser, natsauth.Everything, 0)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(u.Creds)
		return err
	},
}

func init() {
	home, _ := os.UserHomeDir()
	for _, c := range []*cobra.Command{natsInitCmd, natsUserCmd} {
		c.Flags().StringVar(&natsKeysDir, "keys", filepath.Join(home, ".foundation", "nats-keys"), "where the deployment's keys are kept")
	}
	natsUserCmd.Flags().StringVar(&natsAccount, "account", natsauth.App, "the account")
	natsUserCmd.Flags().StringVar(&natsUser, "name", "serve", "the user's name")
	natsCmd.AddCommand(natsInitCmd, natsUserCmd)

	for _, c := range []*cobra.Command{natsReplicasCmd} {
		c.Flags().StringVar(&natsURL, "nats-url", "", "the cluster's URLs, comma separated (default $FOUNDATION_NATS_URL)")
		c.Flags().StringVar(&natsCreds, "nats-creds", "", "a credentials file (default $FOUNDATION_NATS_CREDS)")
		c.Flags().StringVar(&natsCA, "nats-ca", "", "the deployment's CA certificate (default $FOUNDATION_NATS_CA)")
	}
	natsReplicasCmd.Flags().IntVar(&natsReplicas, "set", 3, "the replica count")
	natsCmd.AddCommand(natsReplicasCmd)
	rootCmd.AddCommand(natsCmd)
}
