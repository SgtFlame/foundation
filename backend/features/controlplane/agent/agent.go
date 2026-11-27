// Package agent is axiomd's agent mode (forge 20d-1): inside an instance
// it makes the machine match the instance's desired state, in order: apt
// sources and keys (approved ones only), packages pinned and held, files
// from templates, systemd units, unattended upgrades; it runs each unit's
// health checks and reports managed package versions and unit states.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/daemon"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("axiomd").Tags("agent")

// Desired is an instance's resolved desired state (controlplane.Resolve).
type Desired struct {
	Profile struct {
		Packages           []string `json:"packages"`
		UnattendedUpgrades *bool    `json:"unattendedUpgrades"`
	} `json:"profile"`
	Units   []Unit   `json:"units"`
	Pins    []Pin    `json:"pins"`
	Sources []Source `json:"sources"`
}

// Unit is a service the instance runs.
type Unit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// ServiceSet is a serve unit's (serve --services).
	ServiceSet string          `json:"serviceSet,omitempty"`
	Package    string          `json:"package"`
	Version    string          `json:"version"`
	Config     json.RawMessage `json:"config"`
	Files      []File          `json:"files"`
	Health     []Check         `json:"health"`
	Status     string          `json:"status"` // running (default), stopped
}

// File is a file a unit needs: content is a Go template over .Config
// and .Facts.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    string `json:"mode,omitempty"` // octal, 0644 by default
	Owner   string `json:"owner,omitempty"`
}

// Check is a health check: unit (systemd active), tcp (the port
// answers), http (a 2xx status), nats (the port greets with INFO).
type Check struct {
	Kind            string `json:"kind"`
	Host            string `json:"host,omitempty"`
	Port            int    `json:"port,omitempty"`
	URL             string `json:"url,omitempty"`
	IntervalSeconds int    `json:"intervalSeconds,omitempty"`
}

// Pin is a package at a version (a channel is resolved before it reaches
// the agent; with neither, the newest).
type Pin struct {
	Name       string `json:"name"`
	Source     string `json:"source"` // firstParty, ubuntu, thirdParty
	Version    string `json:"version"`
	Repository string `json:"repository"`
}

// Source is a third-party apt repository.
type Source struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URIs       string `json:"uris"`
	Suites     string `json:"suites"`
	Components string `json:"components"`
	Key        string `json:"key"`
	Approved   bool   `json:"approved"`
}

// Agent does an instance's work.
type Agent struct {
	// Root prefixes every path it writes ("" is /), State is where it
	// keeps what it installed, Path the commands' PATH and Env more of
	// their environment (tests point these at fakes).
	Root, State, Path string
	Env               []string
	// Control is the agent's own connection, Instance its record id and
	// NatsURLs the cluster's: serve units' credentials (forge 20d-2).
	Control  func() *ctl.Control
	Instance string
	NatsURLs []string

	mu     sync.Mutex
	facts  map[string]any
	hmu    sync.Mutex
	health map[string]checked
	want   []Unit
}

type checked struct {
	at     time.Time
	status string
}

// New is an agent daemon for the config.
func New(cfg daemon.Config) (*daemon.Daemon, *Agent) {
	a := &Agent{State: cfg.State}
	return a.Daemon(cfg), a
}

// Daemon is the daemon running this agent's work.
func (a *Agent) Daemon(cfg daemon.Config) *daemon.Daemon {
	if a.State == "" {
		a.State = cfg.State
	}
	d := &daemon.Daemon{Config: cfg, Reconcile: a.Reconcile, Facts: a.Facts, Units: a.Health}
	if t, ok := cfg.TargetOf(); ok && a.Instance == "" {
		a.Instance = t.ID
	}
	if a.Control == nil {
		a.Control = d.Control
	}
	if a.NatsURLs == nil {
		a.NatsURLs = cfg.NatsURLs
	}
	return d
}

func (a *Agent) path(p string) string { return filepath.Join(a.Root, p) }

// run runs a command with a non-interactive environment.
func (a *Agent) run(ctx context.Context, name string, args ...string) (string, error) {
	path := a.Path
	if path == "" {
		path = daemon.ScriptPath
	}
	full, err := lookPath(name, path)
	if err != nil {
		return "", log.Code("command_missing").Errorf("%s is not installed", name)
	}
	cmd := exec.CommandContext(ctx, full, args...)
	cmd.Env = append([]string{"PATH=" + path, "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C"}, a.Env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return out.String(), log.Code("command_failed").Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, lastLine(out.String()))
	}
	return out.String(), nil
}

func lookPath(name, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Facts are the machine's, plus what the last reconcile saw.
func (a *Agent) Facts(ctx context.Context) map[string]any {
	f := daemon.HostFacts(ctx)
	a.mu.Lock()
	for k, v := range a.facts {
		f[k] = v
	}
	a.mu.Unlock()
	if _, err := os.Stat(a.path("/var/run/reboot-required")); err == nil {
		f["rebootRequired"] = true // reported only (part question 1)
	}
	return f
}

// Reconcile makes the instance match desired state.
func (a *Agent) Reconcile(ctx context.Context, raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	var want Desired
	if err := json.Unmarshal(raw, &want); err != nil {
		return log.Wrap(err)
	}
	var errs []error
	refused, err := a.sources(ctx, want.Sources)
	if err != nil {
		errs = append(errs, err)
	}
	pkgs, perrs := a.packages(ctx, want, refused)
	errs = append(errs, perrs...)
	units := serveUnits(want.Units)
	restart, err := a.files(units)
	if err != nil {
		errs = append(errs, err)
	}
	states, err := a.units(ctx, units, restart)
	if err != nil {
		errs = append(errs, err)
	}
	if err := a.renew(ctx, want.Units); err != nil {
		errs = append(errs, err)
	}
	a.forget(want.Units)
	if want.Profile.UnattendedUpgrades != nil {
		if err := a.unattended(*want.Profile.UnattendedUpgrades); err != nil {
			errs = append(errs, err)
		}
	}
	a.mu.Lock()
	a.facts = map[string]any{"packages": pkgs, "units": states}
	a.mu.Unlock()
	a.hmu.Lock()
	a.want = units
	a.hmu.Unlock()
	return errors.Join(errs...)
}

var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// sources writes approved third-party sources and removes the rest of
// Axiom's; it returns the refused ones' ids.
func (a *Agent) sources(ctx context.Context, want []Source) (map[string]bool, error) {
	refused := map[string]bool{}
	keep := map[string]bool{}
	changed := false
	var errs []error
	for _, s := range want {
		if !s.Approved || !safeName.MatchString(s.Name) {
			refused[s.ID] = true
			errs = append(errs, log.Code("source_refused").Errorf("apt source %s is not on the allow list", s.Name))
			continue
		}
		list := a.path("/etc/apt/sources.list.d/axiom-" + s.Name + ".sources")
		key := "/etc/apt/keyrings/axiom-" + s.Name + ".asc"
		keep[filepath.Base(list)] = true
		body := "Types: deb\nURIs: " + s.URIs + "\nSuites: " + s.Suites + "\nComponents: " + s.Components + "\n"
		if s.Key != "" {
			body += "Signed-By: " + key + "\n"
			c, err := writeIfChanged(a.path(key), []byte(s.Key), 0o644)
			if err != nil {
				return refused, err
			}
			changed = changed || c
		}
		c, err := writeIfChanged(list, []byte(body), 0o644)
		if err != nil {
			return refused, err
		}
		changed = changed || c
	}
	dir := a.path("/etc/apt/sources.list.d")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "axiom-") && strings.HasSuffix(e.Name(), ".sources") && !keep[e.Name()] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
			_ = os.Remove(a.path("/etc/apt/keyrings/" + strings.TrimSuffix(e.Name(), ".sources") + ".asc"))
			changed = true
		}
	}
	if changed {
		if _, err := a.run(ctx, "apt-get", "update", "-q"); err != nil {
			errs = append(errs, err)
		}
	}
	return refused, errors.Join(errs...)
}

// managedFile records the packages Axiom installed.
func (a *Agent) managedFile() string { return filepath.Join(a.State, "packages.json") }

func (a *Agent) managed() map[string]string {
	m := map[string]string{}
	if raw, err := os.ReadFile(a.managedFile()); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func (a *Agent) saveManaged(m map[string]string) error {
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(a.State, 0o700); err != nil {
		return log.Wrap(err)
	}
	return log.Wrap(os.WriteFile(a.managedFile(), raw, 0o600))
}

func (a *Agent) installed(ctx context.Context, name string) string {
	out, err := a.run(ctx, "dpkg-query", "-W", "-f=${Version}", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// packages installs and holds what is wanted, and removes what Axiom
// installed and no longer wants; it returns the managed versions.
func (a *Agent) packages(ctx context.Context, want Desired, refused map[string]bool) (map[string]string, []error) {
	wanted := map[string]string{}
	for _, p := range want.Profile.Packages {
		wanted[p] = ""
	}
	for _, u := range want.Units {
		if u.Package != "" {
			wanted[u.Package] = u.Version
		}
	}
	var errs []error
	for _, p := range want.Pins {
		if p.Source == "thirdParty" && (p.Repository == "" || refused[p.Repository]) {
			errs = append(errs, log.Code("source_refused").Errorf("package %s: its source is not on the allow list", p.Name))
			continue
		}
		wanted[p.Name] = p.Version
	}
	m := a.managed()
	names := make([]string, 0, len(wanted))
	for n := range wanted {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := wanted[n]
		have := a.installed(ctx, n)
		if have != "" && (v == "" || have == v) {
			if _, ok := m[n]; !ok {
				_, _ = a.run(ctx, "apt-mark", "hold", n) // there already: held where it is
			}
			m[n] = have
			continue
		}
		spec := n
		if v != "" {
			spec = n + "=" + v
		}
		if _, err := a.run(ctx, "apt-get", "install", "-y", "--no-install-recommends", "--allow-change-held-packages", "--allow-downgrades", spec); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := a.run(ctx, "apt-mark", "hold", n); err != nil {
			errs = append(errs, err)
		}
		m[n] = a.installed(ctx, n)
	}
	for n := range m {
		if _, ok := wanted[n]; ok {
			continue
		}
		_, _ = a.run(ctx, "apt-mark", "unhold", n)
		if _, err := a.run(ctx, "apt-get", "remove", "-y", n); err != nil {
			errs = append(errs, err)
			continue
		}
		delete(m, n)
	}
	if err := a.saveManaged(m); err != nil {
		errs = append(errs, err)
	}
	return m, errs
}

// files renders and writes every unit's files; it returns the units whose
// files changed, and whether a unit file did.
func (a *Agent) files(units []Unit) (map[string]bool, error) {
	changed := map[string]bool{}
	a.mu.Lock()
	facts := a.facts
	a.mu.Unlock()
	var errs []error
	for _, u := range units {
		var cfg any
		_ = json.Unmarshal(u.Config, &cfg)
		for _, f := range u.Files {
			if !filepath.IsAbs(f.Path) || strings.Contains(f.Path, "..") {
				errs = append(errs, log.Code("bad_path").Errorf("unit %s: %s is not an absolute path", u.Name, f.Path))
				continue
			}
			t, err := template.New(f.Path).Option("missingkey=zero").Parse(f.Content)
			if err != nil {
				errs = append(errs, log.Code("bad_template").Errorf("unit %s, %s: %v", u.Name, f.Path, err))
				continue
			}
			var out bytes.Buffer
			if err := t.Execute(&out, map[string]any{"Config": cfg, "Facts": facts, "Unit": u.Name}); err != nil {
				errs = append(errs, log.Code("bad_template").Errorf("unit %s, %s: %v", u.Name, f.Path, err))
				continue
			}
			mode := os.FileMode(0o644)
			if f.Mode != "" {
				if m, err := strconv.ParseUint(f.Mode, 8, 32); err == nil {
					mode = os.FileMode(m)
				}
			}
			c, err := writeIfChanged(a.path(f.Path), out.Bytes(), mode)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if f.Owner != "" {
				if usr, err := user.Lookup(f.Owner); err == nil {
					uid, _ := strconv.Atoi(usr.Uid)
					gid, _ := strconv.Atoi(usr.Gid)
					_ = os.Chown(a.path(f.Path), uid, gid)
				}
			}
			if c {
				changed[u.Name] = true
				if strings.HasPrefix(f.Path, "/etc/systemd/system/") {
					changed[""] = true // a unit file: daemon-reload
				}
			}
		}
	}
	return changed, errors.Join(errs...)
}

// writeIfChanged writes atomically, through a dotted temp file, only when
// the content or mode differs.
func writeIfChanged(path string, content []byte, mode os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		if st, err := os.Stat(path); err == nil && st.Mode().Perm() == mode {
			return false, nil
		}
		return true, log.Wrap(os.Chmod(path, mode))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, log.Wrap(err)
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".axiomd")
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return false, log.Wrap(err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return false, log.Wrap(err)
	}
	return true, log.Wrap(os.Rename(tmp, path))
}

func (a *Agent) active(ctx context.Context, name string) bool {
	out, _ := a.run(ctx, "systemctl", "is-active", name)
	return strings.TrimSpace(out) == "active"
}

// units starts, stops and restarts units; it returns their states.
func (a *Agent) units(ctx context.Context, units []Unit, changed map[string]bool) (map[string]string, error) {
	var errs []error
	if changed[""] {
		if _, err := a.run(ctx, "systemctl", "daemon-reload"); err != nil {
			errs = append(errs, err)
		}
	}
	states := map[string]string{}
	for _, u := range units {
		if u.Name == "" {
			continue
		}
		on := a.active(ctx, u.Name)
		var err error
		switch {
		case u.Status == "stopped" && on:
			_, err = a.run(ctx, "systemctl", "disable", "--now", u.Name)
		case u.Status != "stopped" && !on:
			_, err = a.run(ctx, "systemctl", "enable", "--now", u.Name)
		case u.Status != "stopped" && changed[u.Name]:
			_, err = a.run(ctx, "systemctl", "restart", u.Name)
		}
		if err != nil {
			errs = append(errs, err)
		}
		states[u.Name] = "inactive"
		if a.active(ctx, u.Name) {
			states[u.Name] = "active"
		}
	}
	return states, errors.Join(errs...)
}

func (a *Agent) unattended(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	body := fmt.Sprintf("APT::Periodic::Update-Package-Lists \"%s\";\nAPT::Periodic::Unattended-Upgrade \"%s\";\n", v, v)
	_, err := writeIfChanged(a.path("/etc/apt/apt.conf.d/20auto-upgrades"), []byte(body), 0o644)
	return err
}
