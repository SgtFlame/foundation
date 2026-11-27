package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
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

var natsTenant uint64

var natsTenantCmd = &cobra.Command{
	Use:   "tenant",
	Short: "Maintain the tenants' NATS accounts",
}

var natsTenantAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Make a tenant's NATS account and push it to the cluster",
	Long: `Make tenant --tenant's account T<id> (forge 20a-3), where serve run with
--nats-keys keeps that tenant's streams and buckets. It needs the operator's
seed (operator.nk) in --keys. With --nats-url it pushes the account to the
running cluster as a SYS user (<keys>/sys.creds unless --nats-creds), so it
takes effect without a restart; otherwise add it to trust.conf's preload.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		k := natsauth.DirKeys(natsKeysDir)
		d, err := natsauth.Load(k)
		if err != nil {
			return err
		}
		if d.Operator.Key == nil {
			return fmt.Errorf("%s has no operator.nk: bring the operator's seed back to sign a new account", natsKeysDir)
		}
		a, err := d.AddTenant(natsTenant, natsauth.Limits{})
		if err != nil {
			return err
		}
		if err := d.SaveAccount(k, a.Name); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "account %s (%s) saved\n", a.Name, a.Public())
		o := natsd.Options{URL: natsURL, Creds: natsCreds, CA: natsCA}.WithEnv()
		if o.URL == "" {
			return nil
		}
		if natsCreds == "" {
			o.Creds = filepath.Join(natsKeysDir, "sys.creds")
		}
		nc, err := nats.Connect(o.URL, natsd.ConnectOptions(o, "cli nats tenant add")...)
		if err != nil {
			return err
		}
		defer nc.Close()
		if err := natsauth.Push(nc, a); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "account %s pushed\n", a.Name)
		return nil
	},
}

func init() {
	home, _ := os.UserHomeDir()
	natsTenantAddCmd.Flags().Uint64Var(&natsTenant, "tenant", 0, "the tenant's id")
	_ = natsTenantAddCmd.MarkFlagRequired("tenant")
	natsTenantCmd.AddCommand(natsTenantAddCmd)
	natsCmd.AddCommand(natsTenantCmd)
	for _, c := range []*cobra.Command{natsInitCmd, natsUserCmd, natsTenantAddCmd} {
		c.Flags().StringVar(&natsKeysDir, "keys", filepath.Join(home, ".foundation", "nats-keys"), "where the deployment's keys are kept")
	}
	natsUserCmd.Flags().StringVar(&natsAccount, "account", natsauth.App, "the account")
	natsUserCmd.Flags().StringVar(&natsUser, "name", "serve", "the user's name")
	natsCmd.AddCommand(natsInitCmd, natsUserCmd)

	for _, c := range []*cobra.Command{natsReplicasCmd, natsTenantAddCmd} {
		c.Flags().StringVar(&natsURL, "nats-url", "", "the cluster's URLs, comma separated (default $FOUNDATION_NATS_URL)")
		c.Flags().StringVar(&natsCreds, "nats-creds", "", "a credentials file (default $FOUNDATION_NATS_CREDS)")
		c.Flags().StringVar(&natsCA, "nats-ca", "", "the deployment's CA certificate (default $FOUNDATION_NATS_CA)")
	}
	natsReplicasCmd.Flags().IntVar(&natsReplicas, "set", 3, "the replica count")
	natsCmd.AddCommand(natsReplicasCmd)
	rootCmd.AddCommand(natsCmd)
}
