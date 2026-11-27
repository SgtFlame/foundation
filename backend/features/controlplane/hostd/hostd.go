// Package hostd is axiomd's host mode on LXD (forge 20c-2): it makes,
// starts, stops, snapshots and deletes its host's LXD instances from
// desired state, each found by the user key user.axiom.id, and reports
// the host's capabilities.
package hostd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/daemon"
	"github.com/SgtFlame/foundation/backend/features/controlplane/lxd"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("axiomd").Tags("host")

// KeyID is the LXD user key holding an instance's record id.
const KeyID = "user.axiom.id"

// Desired is a host's resolved desired state (controlplane.Resolve).
type Desired struct {
	Instances []Instance         `json:"instances"`
	Profiles  map[string]Profile `json:"profiles"`
}

// Instance is an instance placed on the host.
type Instance struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Kind    string `json:"kind"`   // container, vm
	Status  string `json:"status"` // running (default), stopped, absent
}

// Want is its wanted status.
func (i Instance) Want() string {
	if i.Status == "" {
		return "running"
	}
	return i.Status
}

// Profile is an Axiom profile, an LXD profile axiom-<id>.
type Profile struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Image  string  `json:"image"`
	CPU    float64 `json:"cpu"`
	Memory string  `json:"memory"`
	Disk   string  `json:"disk"`
}

// Host is a host daemon's LXD work.
type Host struct {
	LXD *lxd.Client
	// Mint gets an enrollment token for an instance placed here; File
	// makes its agent's enrollment file from it.
	Mint func(ctx context.Context, instance string) (string, error)
	File func(token string) (daemon.EnrollFile, error)
	// KVM tells whether virtual machines can run (/dev/kvm opens).
	KVM func() bool
	mu  sync.Mutex
}

// New makes a host daemon for the config, with its LXD at socket ("" finds it).
func New(cfg daemon.Config, socket string) (*daemon.Daemon, *Host, error) {
	c, err := lxd.Dial(socket)
	if err != nil {
		return nil, nil, err
	}
	h := &Host{LXD: c}
	d := h.Daemon(cfg)
	return d, h, nil
}

// Daemon is the daemon running this host's work.
func (h *Host) Daemon(cfg daemon.Config) *daemon.Daemon {
	d := &daemon.Daemon{Config: cfg}
	t, _ := cfg.TargetOf()
	if h.Mint == nil {
		h.Mint = func(ctx context.Context, instance string) (string, error) {
			return d.Control().Mint(ctx, t.ID, instance)
		}
	}
	if h.File == nil {
		h.File = func(token string) (daemon.EnrollFile, error) { return cfg.File("agent", token) }
	}
	d.Reconcile = h.Reconcile
	d.Facts = h.Facts
	d.Handlers = map[string]daemon.Handler{
		"start":    h.state("start"),
		"stop":     h.state("stop"),
		"restart":  h.state("restart"),
		"snapshot": h.snapshot,
		"restore":  h.restore,
		"delete":   func(ctx context.Context, r ctl.Request, p func(string)) error { return h.delete(ctx, d, r) },
	}
	return d
}

func (h *Host) kvm() bool {
	if h.KVM != nil {
		return h.KVM()
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// Facts adds lxd and capabilities to the host's own.
func (h *Host) Facts(ctx context.Context) map[string]any {
	f := daemon.HostFacts(ctx)
	s, err := h.LXD.Server(ctx)
	if err != nil {
		f["lxd"] = map[string]any{"error": err.Error()}
		return f
	}
	pools, _ := h.LXD.StoragePools(ctx)
	nets, _ := h.LXD.Networks(ctx)
	var poolNames, netNames []string
	for _, p := range pools {
		poolNames = append(poolNames, p.Name)
	}
	for _, n := range nets {
		if n.Managed {
			netNames = append(netNames, n.Name)
		}
	}
	f["lxd"] = map[string]any{"version": s.Environment.ServerVersion, "clustered": s.Environment.ServerClustered, "storagePools": poolNames, "networks": netNames}
	caps := []string{"containers"}
	for _, d := range s.Drivers() {
		if d == "qemu" && h.kvm() {
			caps = append(caps, "vms")
		}
	}
	f["capabilities"] = caps
	return f
}

var unsafeName = regexp.MustCompile(`[^a-z0-9-]+`)

// LXDName is an instance's name in LXD: its record's name made a
// hostname, else axiom-<id>.
func LXDName(i Instance) string {
	n := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(i.Name), "-"), "-")
	if n == "" || n[0] < 'a' || n[0] > 'z' || len(n) > 63 {
		return "axiom-" + i.ID
	}
	return n
}

// ProfileName is an Axiom profile's LXD profile.
func ProfileName(id string) string { return "axiom-" + id }

func lxdType(kind string) string {
	if kind == "vm" {
		return "virtual-machine"
	}
	return "container"
}

// source turns a profile's image into an LXD source: ubuntu/<release>
// from Ubuntu's cloud images, anything else from images.linuxcontainers.org.
func source(image string) lxd.Source {
	if rel, ok := strings.CutPrefix(image, "ubuntu/"); ok {
		return lxd.Source{Type: "image", Alias: rel, Server: "https://cloud-images.ubuntu.com/releases", Protocol: "simplestreams"}
	}
	if image == "" {
		image = "ubuntu/24.04"
		return source(image)
	}
	return lxd.Source{Type: "image", Alias: image, Server: "https://images.linuxcontainers.org", Protocol: "simplestreams"}
}

func profileOf(p Profile) lxd.Profile {
	cfg := map[string]string{}
	if p.CPU > 0 {
		cfg["limits.cpu"] = fmt.Sprint(p.CPU)
	}
	if p.Memory != "" {
		cfg["limits.memory"] = p.Memory
	}
	dev := map[string]map[string]string{}
	if p.Disk != "" {
		dev["root"] = map[string]string{"type": "disk", "path": "/", "pool": "default", "size": p.Disk}
	}
	return lxd.Profile{Name: ProfileName(p.ID), Description: "Axiom profile " + p.Name, Config: cfg, Devices: dev}
}

// managed is the host's instances carrying user.axiom.id, by record id.
func (h *Host) managed(ctx context.Context) (map[string]lxd.Instance, map[string]bool, error) {
	all, err := h.LXD.Instances(ctx)
	if err != nil {
		return nil, nil, err
	}
	out, names := map[string]lxd.Instance{}, map[string]bool{}
	for _, i := range all {
		names[i.Name] = true
		if id := i.Config[KeyID]; id != "" {
			out[id] = i
		}
	}
	return out, names, nil
}

// Reconcile makes the host's LXD match desired: profiles first, then each
// instance made (adopted when it exists), started, stopped or deleted.
// Instances whose record is gone are left alone.
func (h *Host) Reconcile(ctx context.Context, raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	var want Desired
	if err := json.Unmarshal(raw, &want); err != nil {
		return log.Wrap(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	srv, err := h.LXD.Server(ctx)
	if err != nil {
		return err
	}
	target := ""
	if srv.Environment.ServerClustered {
		target = srv.Environment.ServerName // each member makes its own (part question 1)
	}
	have, names, err := h.managed(ctx)
	if err != nil {
		return err
	}
	var errs []error
	done := map[string]bool{}
	for _, i := range want.Instances {
		if i.Want() == "absent" {
			continue
		}
		if p, ok := want.Profiles[i.Profile]; ok && !done[p.ID] {
			done[p.ID] = true
			if err := h.LXD.PutProfile(ctx, profileOf(p)); err != nil {
				errs = append(errs, fmt.Errorf("profile %s: %w", p.Name, err))
			}
		}
	}
	for _, i := range want.Instances {
		if err := h.one(ctx, i, want.Profiles[i.Profile], have, names, target); err != nil {
			errs = append(errs, fmt.Errorf("instance %s: %w", i.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (h *Host) one(ctx context.Context, i Instance, p Profile, have map[string]lxd.Instance, names map[string]bool, target string) error {
	cur, exists := have[i.ID]
	switch i.Want() {
	case "absent":
		if !exists {
			return nil
		}
		if cur.Running() {
			if err := h.LXD.SetState(ctx, cur.Name, "stop", true); err != nil {
				return err
			}
		}
		return h.LXD.DeleteInstance(ctx, cur.Name)
	}
	if !exists {
		name := LXDName(i)
		if names[name] {
			return log.Code("name_taken").Errorf("an instance not made by Axiom is named %s", name)
		}
		tok, err := h.Mint(ctx, i.ID)
		if err != nil {
			return err
		}
		f, err := h.File(tok)
		if err != nil {
			return err
		}
		kind := i.Kind
		if kind == "" {
			kind = p.Kind
		}
		profiles := []string{"default"}
		if p.ID != "" {
			profiles = append(profiles, ProfileName(p.ID))
		}
		in := lxd.InstanceCreate{Name: name, Type: lxdType(kind), Profiles: profiles, Source: source(p.Image), Start: i.Want() == "running",
			Config: map[string]string{KeyID: i.ID, "cloud-init.user-data": UserData(f)}}
		err = h.LXD.CreateInstance(ctx, in, target)
		var le *lxd.Error
		if errors.As(err, &le) && le.Status == 409 {
			return nil // made meanwhile: adopted at the next pass
		}
		return err
	}
	switch {
	case i.Want() == "running" && !cur.Running():
		return h.LXD.SetState(ctx, cur.Name, "start", false)
	case i.Want() == "stopped" && cur.Running():
		return h.LXD.SetState(ctx, cur.Name, "stop", false)
	}
	return nil
}

// UserData is the cloud-init that hands an instance its enrollment
// file; installing and starting the agent's package comes with 20d-1.
func UserData(f daemon.EnrollFile) string {
	raw, _ := json.Marshal(f)
	return "#cloud-config\nwrite_files:\n  - path: /etc/axiomd/enroll.json\n    permissions: '0600'\n    content: " + string(mustJSON(string(raw))) + "\n"
}

func mustJSON(s string) []byte {
	raw, _ := json.Marshal(s) // a JSON string is a valid YAML double-quoted scalar
	return raw
}

// ReadUserData reads the enrollment file back out of UserData's text.
func ReadUserData(s string) (daemon.EnrollFile, error) {
	var f daemon.EnrollFile
	_, after, ok := strings.Cut(s, "content: ")
	if !ok {
		return f, log.Code("no_enroll_file").Errorf("no enrollment file in the user data")
	}
	var text string
	if err := json.Unmarshal([]byte(strings.TrimSpace(after)), &text); err != nil {
		return f, log.Wrap(err)
	}
	return f, log.Wrap(json.Unmarshal([]byte(text), &f))
}

// args of the instance verbs.
type args struct {
	Instance string `json:"instance"`
	Name     string `json:"name,omitempty"`
}

func (h *Host) find(ctx context.Context, r ctl.Request) (lxd.Instance, args, error) {
	var a args
	if len(r.Args) > 0 {
		if err := json.Unmarshal(r.Args, &a); err != nil {
			return lxd.Instance{}, a, log.Code("bad_args").Errorf("bad arguments: %v", err)
		}
	}
	have, _, err := h.managed(ctx)
	if err != nil {
		return lxd.Instance{}, a, err
	}
	i, ok := have[a.Instance]
	if !ok {
		return lxd.Instance{}, a, log.Code("no_instance").Errorf("no instance %s on this host", a.Instance)
	}
	return i, a, nil
}

func (h *Host) state(action string) daemon.Handler {
	return func(ctx context.Context, r ctl.Request, _ func(string)) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		i, _, err := h.find(ctx, r)
		if err != nil {
			return err
		}
		if (action == "start" && i.Running()) || (action == "stop" && !i.Running()) {
			return nil
		}
		return h.LXD.SetState(ctx, i.Name, action, false)
	}
}

func (h *Host) snapshot(ctx context.Context, r ctl.Request, _ func(string)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	i, a, err := h.find(ctx, r)
	if err != nil {
		return err
	}
	if a.Name == "" {
		return log.Code("bad_args").Errorf("a snapshot needs a name")
	}
	snaps, err := h.LXD.Snapshots(ctx, i.Name)
	if err != nil {
		return err
	}
	for _, s := range snaps {
		if s == a.Name {
			return nil // taken already: once by name
		}
	}
	return h.LXD.Snapshot(ctx, i.Name, a.Name)
}

func (h *Host) restore(ctx context.Context, r ctl.Request, _ func(string)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	i, a, err := h.find(ctx, r)
	if err != nil {
		return err
	}
	if a.Name == "" {
		return log.Code("bad_args").Errorf("a restore needs a snapshot's name")
	}
	return h.LXD.Restore(ctx, i.Name, a.Name)
}

// delete deletes an instance only when its desired status is absent.
func (h *Host) delete(ctx context.Context, d *daemon.Daemon, r ctl.Request) error {
	t, _ := d.Config.TargetOf()
	raw, _, err := d.Control().Desired(ctx, t)
	if err != nil {
		return err
	}
	var want Desired
	_ = json.Unmarshal(raw, &want)
	var a args
	_ = json.Unmarshal(r.Args, &a)
	for _, i := range want.Instances {
		if i.ID == a.Instance && i.Want() != "absent" {
			return log.Code("delete_refused").Errorf("instance %s is desired %s: set it absent first", i.Name, i.Want())
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	i, _, err := h.find(ctx, r)
	if err != nil {
		return err
	}
	if i.Running() {
		if err := h.LXD.SetState(ctx, i.Name, "stop", true); err != nil {
			return err
		}
	}
	return h.LXD.DeleteInstance(ctx, i.Name)
}
