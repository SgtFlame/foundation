package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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
every nats-server.conf to <keys>/trust.conf, and three creds files: serve.creds
(an APP user for serve), sys.creds (a SYS user that pushes accounts) and
enroll.creds (the CTL user every axiomd enrolls with, which may only ask to
enroll; forge 20c-1).
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
		enroll, err := d.Account(natsauth.Control).NewUser("enroll", ctl.EnrollPermissions, 0)
		if err != nil {
			return err
		}
		if err := k.Put("enroll.creds", enroll.Creds); err != nil {
			return err
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
		if d.Operator.Key == nil && d.Operator.Signing == nil {
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

var natsCluster, natsPlaceKeys string

var natsPlaceCmd = &cobra.Command{
	Use:   "place",
	Short: "Move a tenant's streams to a cluster of the supercluster",
	Long: `Move every stream, KV bucket and object store of tenant --tenant to cluster
--cluster (forge 20a-4), one at a time, waiting until each has every replica
current there; JetStream moves the messages. A stream already there is left
alone, so it can be run again. With --keys holding the tenant's account
(serve --nats-keys) it connects as a user of that account; otherwise with
--nats-creds. Set the tenant's placement too (serve --nats-placements), or
streams made later go where JetStream puts them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		o := natsd.Options{URL: natsURL, Creds: natsCreds, CA: natsCA}.WithEnv()
		if o.URL == "" {
			return fmt.Errorf("--nats-url or $%s is needed", natsd.EnvURL)
		}
		var js jetstream.JetStream
		acc, err := natsauth.TenantAccounts(o, natsPlaceKeys)
		if err != nil {
			return err
		}
		if acc != nil {
			defer acc.Close()
			if js, err = acc.For(cmd.Context(), natsTenant); err != nil {
				return err
			}
		} else {
			s, err := natsd.Start(cmd.Context(), o)
			if err != nil {
				return err
			}
			defer s.Close()
			js = s.JS()
		}
		moved, err := natsd.Place(cmd.Context(), js, natsTenant, natsd.Placement{Cluster: natsCluster})
		for _, name := range moved {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: in cluster %s\n", name, natsCluster)
		}
		if err == nil && len(moved) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "every stream of tenant %d is already in cluster %s\n", natsTenant, natsCluster)
		}
		return err
	},
}

func init() {
	home, _ := os.UserHomeDir()
	natsPlaceCmd.Flags().Uint64Var(&natsTenant, "tenant", 0, "the tenant's id")
	natsPlaceCmd.Flags().StringVar(&natsCluster, "cluster", "", "the cluster to move to")
	natsPlaceCmd.Flags().StringVar(&natsPlaceKeys, "keys", "", "the deployment's keys, to reach the tenant's own account (default $FOUNDATION_NATS_KEYS)")
	_ = natsPlaceCmd.MarkFlagRequired("tenant")
	_ = natsPlaceCmd.MarkFlagRequired("cluster")
	natsCmd.AddCommand(natsPlaceCmd)
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

	for _, c := range []*cobra.Command{natsReplicasCmd, natsTenantAddCmd, natsPlaceCmd} {
		c.Flags().StringVar(&natsURL, "nats-url", "", "the cluster's URLs, comma separated (default $FOUNDATION_NATS_URL)")
		c.Flags().StringVar(&natsCreds, "nats-creds", "", "a credentials file (default $FOUNDATION_NATS_CREDS)")
		c.Flags().StringVar(&natsCA, "nats-ca", "", "the deployment's CA certificate (default $FOUNDATION_NATS_CA)")
	}
	natsReplicasCmd.Flags().IntVar(&natsReplicas, "set", 3, "the replica count")
	natsCmd.AddCommand(natsReplicasCmd)
	rootCmd.AddCommand(natsCmd)
}
