// Command axiomd is the control plane's daemon (forge 20c-1): on a host
// (axiomd host) or inside an instance (axiomd agent) it enrolls once, then
// reconciles its target's desired state, serves its control requests and
// reports facts and heartbeats. Build it with scripts/build.sh -o out/axiomd.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/agent"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/daemon"
	"github.com/SgtFlame/foundation/backend/features/controlplane/hostd"
	"github.com/SgtFlame/foundation/backend/features/controlplane/packages"
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
	case "repo-serve":
		err = repoServe(os.Args[2:])
	case "unit-credentials":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		err = agent.AskLocal("", os.Args[2]) // forge 20d-2: run by axiom-serve-creds@<unit>
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
  axiomd unit-credentials <unit>
  axiomd repo-serve [--config f] [--listen addr]
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
	d, a := agent.New(c)                          // forge 20d-1: packages, files, units, health
	if err := a.ServeLocal(ctx, ""); err != nil { // forge 20d-2: serve units' credentials
		return err
	}
	return d.Run(ctx)
}

// repoServe serves the first-party apt repository from the control
// account's object store over HTTP (forge 20f-1), with the daemon's own
// credentials; the agent runs it on every NATS node's instance.
func repoServe(args []string) error {
	fs := flag.NewFlagSet("repo-serve", flag.ExitOnError)
	config := fs.String("config", defaultConfig, "the config file")
	listen := fs.String("listen", ":"+strconv.Itoa(ctl.RepoPort), "the address to serve on")
	_ = fs.Parse(args)
	c, err := daemon.LoadConfig(*config)
	if err != nil {
		return err
	}
	d := &daemon.Daemon{Config: c}
	if err := d.Connect(nil); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Addr: *listen, Handler: &packages.Server{JS: d.Control().JS}, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
