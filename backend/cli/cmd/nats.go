package cmd

import (
	"fmt"

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

func init() {
	for _, c := range []*cobra.Command{natsReplicasCmd} {
		c.Flags().StringVar(&natsURL, "nats-url", "", "the cluster's URLs, comma separated (default $FOUNDATION_NATS_URL)")
		c.Flags().StringVar(&natsCreds, "nats-creds", "", "a credentials file (default $FOUNDATION_NATS_CREDS)")
		c.Flags().StringVar(&natsCA, "nats-ca", "", "the deployment's CA certificate (default $FOUNDATION_NATS_CA)")
	}
	natsReplicasCmd.Flags().IntVar(&natsReplicas, "set", 3, "the replica count")
	natsCmd.AddCommand(natsReplicasCmd)
	rootCmd.AddCommand(natsCmd)
}
