// Command axiomd is the control plane's daemon (forge 20c-1): on a host
// (axiomd host) or inside an instance (axiomd agent) it enrolls once, then
// reconciles its target's desired state, serves its control requests and
// reports facts and heartbeats. Build it with scripts/build.sh -o out/axiomd.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/SgtFlame/foundation/backend/features/controlplane/daemon"
	"github.com/SgtFlame/foundation/backend/features/controlplane/hostd"
)

const defaultConfig = "/etc/axiomd/axiomd.json"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "enroll":
		err = enroll(os.Args[2:])
	case "host", "agent":
		err = run(os.Args[1], os.Args[2:])
	case "version":
		fmt.Println(daemon.Version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "axiomd:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  axiomd enroll --token <t> --url <nats urls> [--enroll-creds f] [--ca f] [--mode host|agent] [--config f] [--state dir]
  axiomd enroll --from /etc/axiomd/enroll.json [--config f] [--state dir]
  axiomd host [--config f] [--lxd-socket path]
  axiomd agent [--config f]
  axiomd version`)
}

func enroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	token := fs.String("token", "", "the one-time enrollment token")
	urls := fs.String("url", "", "the NATS URLs, comma separated")
	creds := fs.String("enroll-creds", "", "the shared enroll user's creds file")
	ca := fs.String("ca", "", "the deployment's CA certificate")
	mode := fs.String("mode", "host", "host or agent")
	config := fs.String("config", defaultConfig, "where the config is written")
	state := fs.String("state", "/var/lib/axiomd", "the daemon's state directory (the creds file is kept here)")
	from := fs.String("from", "", "an enrollment file a host wrote (cloud-init), in place of the other flags")
	_ = fs.Parse(args)
	if *from != "" {
		c, err := daemon.EnrollFrom(context.Background(), *from, *state, *config)
		if err != nil {
			return err
		}
		_ = os.Remove(*from) // its token is spent
		fmt.Printf("enrolled as %s; config written to %s\n", c.Target, *config)
		return nil
	}
	if *token == "" || *urls == "" {
		return fmt.Errorf("--token and --url are required")
	}
	c := daemon.Config{Mode: *mode, NatsURLs: strings.Split(*urls, ","), CA: *ca, State: *state}
	c, err := daemon.Enroll(context.Background(), c, *creds, *token, *config)
	if err != nil {
		return err
	}
	fmt.Printf("enrolled as %s; config written to %s\n", c.Target, *config)
	return nil
}

func run(mode string, args []string) error {
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	config := fs.String("config", defaultConfig, "the config file")
	socket := fs.String("lxd-socket", "", "LXD's socket (host mode; found when left out)")
	_ = fs.Parse(args)
	c, err := daemon.LoadConfig(*config)
	if err != nil {
		return err
	}
	if c.Mode != "" && c.Mode != mode {
		return fmt.Errorf("the config is for %s mode", c.Mode)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if mode == "host" {
		d, _, err := hostd.New(c, *socket) // forge 20c-2: LXD instances
		if err != nil {
			return err
		}
		return d.Run(ctx)
	}
	// What agent mode does to an instance comes with 20d-1 (packages)
	d := &daemon.Daemon{Config: c, Facts: daemon.HostFacts}
	return d.Run(ctx)
}
