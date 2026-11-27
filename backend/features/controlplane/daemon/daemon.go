// Package daemon is what axiomd's host and agent modes share (forge
// 20c-1, documentation/ControlPlane.md): enrollment, its configuration and
// credentials, the request server, the reconcile loop, facts and
// heartbeat. A mode gives it a Reconcile function and facts.
package daemon

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
)

var log = logging.In("axiomd")

// Version is axiomd's, set at build time.
var Version = "dev"

// Config is /etc/axiomd/axiomd.json.
type Config struct {
	Mode     string   `json:"mode"` // host or agent
	Target   string   `json:"target"`
	NatsURLs []string `json:"natsUrls"`
	CA       string   `json:"ca,omitempty"`
	// EnrollCreds is the shared enroll user's creds file, kept by a host
	// to hand to the instances it makes (forge 20c-2).
	EnrollCreds string `json:"enrollCreds,omitempty"`
	// ScriptKeys are the script signing keys trusted until desired state
	// names its own (scriptKeys; forge 20c-3), base64 Ed25519 public keys.
	ScriptKeys []string `json:"scriptKeys,omitempty"`
	// Creds is the creds file's path (/var/lib/axiomd/creds), State the
	// directory of its own state (/var/lib/axiomd).
	Creds string `json:"creds"`
	State string `json:"state"`
}

// TargetOf is the config's target (host.<id> or inst.<id>).
func (c Config) TargetOf() (ctl.Target, bool) { return ctl.TargetOfKey(c.Target) }

// LoadConfig reads a config file.
func LoadConfig(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, log.Wrap(err)
	}
	return c, log.Wrap(json.Unmarshal(raw, &c))
}

// Save writes it, readable by its owner only.
func (c Config) Save(path string) error {
	raw, _ := json.MarshalIndent(c, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return log.Wrap(err)
	}
	return log.Wrap(os.WriteFile(path, raw, 0o600))
}

func connectOptions(c Config, name string) []nats.Option {
	opts := []nats.Option{nats.Name(name), nats.MaxReconnects(-1)}
	if c.CA != "" {
		opts = append(opts, nats.RootCAs(c.CA))
	}
	return opts
}

// Enroll exchanges a one-time token for the daemon's credentials: it makes
// a user key here, connects as the shared enroll user (enrollCreds) and
// gets back a JWT for that key scoped to its target, then writes the
// creds file (the seed never leaves the machine) and the config.
func Enroll(ctx context.Context, c Config, enrollCreds, token, configPath string) (Config, error) {
	kp, err := nkeys.CreateUser()
	if err != nil {
		return c, log.Wrap(err)
	}
	pub, _ := kp.PublicKey()
	opts := connectOptions(c, "axiomd-enroll")
	if enrollCreds != "" {
		opts = append(opts, nats.UserCredentials(enrollCreds))
	}
	nc, err := nats.Connect(joinURLs(c.NatsURLs), opts...)
	if err != nil {
		return c, log.Wrapf(err, "connect to NATS to enroll")
	}
	defer nc.Close()
	raw, _ := json.Marshal(ctl.EnrollRequest{Token: token, PublicKey: pub})
	m, err := nc.RequestWithContext(ctx, ctl.EnrollSubject, raw)
	if err != nil {
		return c, log.Wrapf(err, "enroll")
	}
	var rep ctl.EnrollReply
	if err := json.Unmarshal(m.Data, &rep); err != nil {
		return c, log.Wrap(err)
	}
	if rep.Error != "" {
		return c, log.Code("enroll_refused").Errorf("enrollment refused: %s", rep.Error)
	}
	seed, _ := kp.Seed()
	creds, err := jwt.FormatUserConfig(rep.JWT, seed)
	if err != nil {
		return c, log.Wrap(err)
	}
	if c.State == "" {
		c.State = "/var/lib/axiomd"
	}
	if c.Creds == "" {
		c.Creds = filepath.Join(c.State, "creds")
	}
	if err := os.MkdirAll(filepath.Dir(c.Creds), 0o700); err != nil {
		return c, log.Wrap(err)
	}
	if err := os.WriteFile(c.Creds, creds, 0o600); err != nil {
		return c, log.Wrap(err)
	}
	if enrollCreds != "" {
		if raw, err := os.ReadFile(enrollCreds); err == nil {
			kept := filepath.Join(c.State, "enroll.creds")
			if err := os.WriteFile(kept, raw, 0o600); err != nil {
				return c, log.Wrap(err)
			}
			c.EnrollCreds = kept
		}
	}
	c.Target = rep.Target.Key()
	if configPath != "" {
		if err := c.Save(configPath); err != nil {
			return c, err
		}
	}
	return c, nil
}

func joinURLs(urls []string) string {
	out := ""
	for i, u := range urls {
		if i > 0 {
			out += ","
		}
		out += u
	}
	return out
}

// Handler runs a request's work; progress reports along the way.
type Handler func(ctx context.Context, r ctl.Request, progress func(msg string)) error

// Daemon runs one target's daemon.
type Daemon struct {
	Config Config
	// Reconcile makes the machine match the desired state (nil when the
	// target has none yet); it is run on start, on a reconcile request and
	// every Period, and must be idempotent.
	Reconcile func(ctx context.Context, desired json.RawMessage) error
	// Facts are reported on start and whenever they change.
	Facts func(ctx context.Context) map[string]any
	// Handlers are verbs beyond reconcile.
	Handlers map[string]Handler
	// Period is the reconcile pass's (5 min, jittered), Beat the
	// heartbeat's (HealthTTL / 3).
	Period, Beat time.Duration

	c      *ctl.Control
	target ctl.Target
	mu     sync.Mutex
	rmu    sync.Mutex // one reconcile at a time
	run    chan struct{}
}

// Connect connects with the daemon's creds (or conn, in tests).
func (d *Daemon) Connect(conn *nats.Conn) error {
	t, ok := d.Config.TargetOf()
	if !ok {
		return log.Code("bad_config").Errorf("the config names no target")
	}
	d.target = t
	nc := conn
	if nc == nil {
		opts := connectOptions(d.Config, "axiomd-"+t.Key())
		opts = append(opts, nats.UserCredentials(d.Config.Creds))
		var err error
		if nc, err = nats.Connect(joinURLs(d.Config.NatsURLs), opts...); err != nil {
			return log.Wrapf(err, "connect to NATS")
		}
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return log.Wrap(err)
	}
	d.c = &ctl.Control{NC: nc, JS: js}
	return nil
}

// Control is the daemon's control connection.
func (d *Daemon) Control() *ctl.Control { return d.c }

// Run serves requests, reconciles and beats until ctx ends. Requests
// interrupted by a stop are run again first.
func (d *Daemon) Run(ctx context.Context) error {
	if d.c == nil {
		if err := d.Connect(nil); err != nil {
			return err
		}
	}
	d.run = make(chan struct{}, 1)
	if d.Period == 0 {
		d.Period = 5 * time.Minute
	}
	if d.Beat == 0 {
		d.Beat = ctl.HealthTTL / 3
	}
	sub, err := d.c.NC.Subscribe(d.target.Requests(), func(m *nats.Msg) { d.serve(ctx, m) })
	if err != nil {
		return log.Wrap(err)
	}
	defer sub.Unsubscribe()
	if err := d.c.NC.Flush(); err != nil {
		return log.Wrap(err)
	}
	for _, r := range d.pending() {
		go d.handle(ctx, r)
	}
	d.kick()
	go d.beat(ctx)
	timer := time.NewTimer(d.jitter())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.run:
		case <-timer.C:
			timer.Reset(d.jitter())
		}
		if err := d.reconcile(ctx); err != nil {
			log.Error(ctx, err, "axiomd: reconcile failed")
		}
	}
}

func (d *Daemon) jitter() time.Duration {
	return d.Period/2 + time.Duration(rand.Int64N(int64(d.Period)))
}

func (d *Daemon) kick() {
	select {
	case d.run <- struct{}{}:
	default:
	}
}

func (d *Daemon) reconcile(ctx context.Context) error {
	if d.Reconcile == nil {
		return nil
	}
	d.rmu.Lock()
	defer d.rmu.Unlock()
	desired, _, err := d.c.Desired(ctx, d.target)
	if err != nil {
		return err
	}
	return d.Reconcile(ctx, desired)
}

func (d *Daemon) beat(ctx context.Context) {
	var last string
	t := time.NewTicker(d.Beat)
	defer t.Stop()
	for {
		if d.Facts != nil {
			f := d.Facts(ctx)
			if f == nil {
				f = map[string]any{}
			}
			f["axiomd"] = Version
			f["goos"], f["goarch"] = runtime.GOOS, runtime.GOARCH
			raw, _ := json.Marshal(f)
			if string(raw) != last {
				if err := d.c.PutFacts(ctx, d.target, f); err == nil {
					last = string(raw)
				}
			}
		}
		_ = d.c.Beat(ctx, d.target, ctl.Health{Status: "up"})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (d *Daemon) serve(ctx context.Context, m *nats.Msg) {
	var r ctl.Request
	if err := json.Unmarshal(m.Data, &r); err != nil || r.ID == "" {
		raw, _ := json.Marshal(ctl.Reply{Error: "bad request"})
		_ = m.Respond(raw)
		return
	}
	if r.Verb != "reconcile" && r.Verb != "script" && d.Handlers[r.Verb] == nil {
		raw, _ := json.Marshal(ctl.Reply{Error: "unknown verb " + r.Verb})
		_ = m.Respond(raw)
		return
	}
	raw, _ := json.Marshal(ctl.Reply{Accepted: true})
	_ = m.Respond(raw)
	go d.handle(ctx, r)
}

// handle runs a request once: a request finished before is reported
// done again (the same message, dropped as a duplicate), one interrupted
// is kept in pending/ and run again on restart.
func (d *Daemon) handle(ctx context.Context, r ctl.Request) {
	report := func(kind, msg string, seq int) {
		_ = d.c.Report(ctx, ctl.Event{Request: r.ID, Kind: kind, Target: d.target, Verb: r.Verb, Message: msg, Seq: seq})
	}
	if d.finished(r.ID) {
		report(ctl.EventDone, r.Verb+" done", 0)
		return
	}
	d.keep(r)
	report(ctl.EventAccepted, "", 0)
	n := 0
	progress := func(msg string) { n++; report(ctl.EventProgress, msg, n) }
	var err error
	switch r.Verb {
	case "reconcile":
		err = d.reconcile(ctx)
	case "script":
		err = d.script(ctx, r, func(msg string, seq int) { report(ctl.EventProgress, msg, seq) })
	default:
		err = d.Handlers[r.Verb](ctx, r, progress)
	}
	if ctx.Err() != nil {
		return // stopped: run again on restart
	}
	if err != nil {
		report(ctl.EventFailed, err.Error(), 0)
	} else {
		report(ctl.EventDone, r.Verb+" done", 0)
	}
	d.finish(r.ID)
}

func (d *Daemon) stateDir(sub string) string {
	dir := filepath.Join(d.Config.State, sub)
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func (d *Daemon) keep(r ctl.Request) {
	if d.Config.State == "" {
		return
	}
	raw, _ := json.Marshal(r)
	_ = os.WriteFile(filepath.Join(d.stateDir("pending"), safe(r.ID)+".json"), raw, 0o600)
}

func (d *Daemon) pending() []ctl.Request {
	if d.Config.State == "" {
		return nil
	}
	entries, _ := os.ReadDir(d.stateDir("pending"))
	var out []ctl.Request
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(d.stateDir("pending"), e.Name()))
		var r ctl.Request
		if err == nil && json.Unmarshal(raw, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// finish moves a request from pending to the last 200 finished ids.
func (d *Daemon) finish(id string) {
	if d.Config.State == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	done := d.done()
	done = append(done, id)
	if len(done) > 200 {
		done = done[len(done)-200:]
	}
	raw, _ := json.Marshal(done)
	_ = os.WriteFile(filepath.Join(d.Config.State, "done.json"), raw, 0o600)
	_ = os.Remove(filepath.Join(d.stateDir("pending"), safe(id)+".json"))
}

func (d *Daemon) finished(id string) bool {
	if d.Config.State == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Contains(d.done(), id)
}

func (d *Daemon) done() []string {
	var done []string
	raw, err := os.ReadFile(filepath.Join(d.Config.State, "done.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &done)
	}
	return done
}

func safe(id string) string {
	b := []byte(id)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			b[i] = '_'
		}
	}
	return string(b)
}
