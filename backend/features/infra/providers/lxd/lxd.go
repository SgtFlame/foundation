// Package lxd is the infra.lxd connector type (forge 22g): infra.compute@1
// on an LXD server the operator already has, over its HTTPS REST API with
// a trusted client certificate. A host is an LXD virtual machine named by
// our resource id, with the id in user.axiom.host, its size's limits, its
// cloud-init user data and a root disk on the connection's storage pool.
package lxd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/providers"
)

// TypeID is the connector type.
const TypeID = "infra.lxd"

// HostKey is the instance config key holding our resource id.
const HostKey = "user.axiom.host"

// SettingsSchema is infra.lxd's settings (Configuration's schema
// language; the client key is a secret).
const SettingsSchema = `{"type":"object","properties":{
  "baseUrl":{"type":"string","title":"Server","description":"The LXD server's HTTPS address, e.g. https://lxd.example.com:8443"},
  "project":{"type":"string","title":"Project","description":"The LXD project hosts are made in; default when empty"},
  "profile":{"type":"string","title":"Profile","description":"The profile every host gets; default when empty"},
  "storagePool":{"type":"string","title":"Storage pool","description":"The pool for hosts' root disks; the profile's when empty"},
  "region":{"type":"string","title":"Region","description":"The one region this server is, as sites name it; local when empty"},
  "target":{"type":"string","title":"Cluster member","description":"The cluster member hosts go on; LXD's choice when empty"},
  "imageServer":{"type":"string","title":"Image server","description":"Where images come from (simplestreams); Ubuntu's cloud images when empty"},
  "vms":{"type":"boolean","title":"Nested virtualization","description":"Whether this server's machines let hosts run virtual machines (nested KVM); off when empty"},
  "waitSeconds":{"type":"integer","title":"Wait","description":"How long host.create waits for LXD's operation before answering creating; 60 seconds when empty","unit":"s","min":1,"max":600},
  "serverCert":{"type":"string","title":"Server certificate","description":"The server's certificate (PEM) to trust; the system's roots when empty"},
  "clientCert":{"type":"string","title":"Client certificate","description":"The client certificate (PEM) the server trusts"},
  "clientKey":{"type":"secret","title":"Client key","description":"The client certificate's private key (PEM)"}},
  "required":["baseUrl","clientCert","clientKey"]}`

// Type is infra.lxd.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{infra.ComputeRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Meta: connectors.Meta{Title: "LXD", Description: "Makes hosts as virtual machines on an LXD server the operator already has, over its HTTPS API with a trusted client certificate: implements infra.compute@1 with sizes s, m, l and xl and one region, each host named by its resource id and started with its cloud-init user data.", Tags: []string{"infrastructure", "compute", "lxd"}},
	}
}

// Sizes are the sizes every LXD connection offers; vms follows the
// connection's setting.
var Sizes = []infra.ProviderSize{
	{ID: "s", VCPUs: 1, MemoryMB: 2048, DiskGB: 20},
	{ID: "m", VCPUs: 2, MemoryMB: 4096, DiskGB: 40},
	{ID: "l", VCPUs: 4, MemoryMB: 8192, DiskGB: 80},
	{ID: "xl", VCPUs: 8, MemoryMB: 16384, DiskGB: 160},
}

// Settings are a connection's settings, resolved.
type Settings struct {
	BaseURL     string `json:"baseUrl"`
	Project     string `json:"project"`
	Profile     string `json:"profile"`
	StoragePool string `json:"storagePool"`
	Region      string `json:"region"`
	Target      string `json:"target"`
	ImageServer string `json:"imageServer"`
	VMs         bool   `json:"vms"`
	WaitSeconds int    `json:"waitSeconds"`
	ServerCert  string `json:"serverCert"`
	ClientCert  string `json:"clientCert"`
	ClientKey   string `json:"clientKey"`
}

// Options are for tests: HTTP replaces the client the connection's
// certificates make; Wrap wraps that client's transport (a recorder).
type Options struct {
	HTTP *http.Client
	Wrap func(http.RoundTripper) http.RoundTripper
}

// Impl is the adapter implementation.
func Impl(o Options) adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if len(conn.Settings) > 0 {
			if err := json.Unmarshal(conn.Settings, &st); err != nil {
				return nil, connectors.Errorf(connectors.CodeInvalid, "the connection's settings are not valid: %v", err)
			}
		}
		b, err := New(st, o)
		if err != nil {
			return nil, err
		}
		return providers.Session{B: b}, nil
	}}
}

// Backend is one connection's LXD server.
type Backend struct {
	st Settings
	c  providers.Client
}

// New makes a backend from settings.
func New(st Settings, o Options) (*Backend, error) {
	if st.BaseURL == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "baseUrl is required")
	}
	if u, err := url.Parse(st.BaseURL); err != nil || u.Scheme != "https" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "baseUrl must be an https address")
	}
	if st.Project == "" {
		st.Project = "default"
	}
	if st.Profile == "" {
		st.Profile = "default"
	}
	if st.Region == "" {
		st.Region = "local"
	}
	if st.ImageServer == "" {
		st.ImageServer = "https://cloud-images.ubuntu.com/releases"
	}
	if st.WaitSeconds <= 0 {
		st.WaitSeconds = 60
	}
	hc := o.HTTP
	if hc == nil {
		cert, err := tls.X509KeyPair([]byte(st.ClientCert), []byte(st.ClientKey))
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "the client certificate and key do not make a pair")
		}
		cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		if st.ServerCert != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(st.ServerCert)) {
				return nil, connectors.Errorf(connectors.CodeInvalid, "the server certificate is not PEM")
			}
			cfg.RootCAs = pool
		}
		var tr http.RoundTripper = &http.Transport{TLSClientConfig: cfg}
		if o.Wrap != nil {
			tr = o.Wrap(tr)
		}
		hc = &http.Client{Timeout: time.Duration(st.WaitSeconds+30) * time.Second, Transport: tr}
	}
	return &Backend{st: st, c: providers.Client{Base: st.BaseURL, HTTP: hc, Message: message}}, nil
}

func message(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Error
}

// envelope is LXD's answer.
type envelope struct {
	Type      string          `json:"type"`
	Operation string          `json:"operation"`
	Metadata  json.RawMessage `json:"metadata"`
}

func (b *Backend) q(extra ...string) url.Values {
	v := url.Values{"project": {b.st.Project}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v
}

func (b *Backend) do(ctx context.Context, method, path string, q url.Values, body any, out any) (envelope, error) {
	var e envelope
	if _, err := b.c.Do(ctx, method, path, q, body, &e); err != nil {
		return e, err
	}
	if out != nil && len(e.Metadata) > 0 {
		if err := json.Unmarshal(e.Metadata, out); err != nil {
			return e, connectors.Errorf(connectors.CodeUnavailable, "LXD's answer to %s %s is not what it should be: %v", method, path, err)
		}
	}
	return e, nil
}

// wait waits for an async answer's operation up to seconds; done false
// when it is still running.
func (b *Backend) wait(ctx context.Context, e envelope, seconds int) (bool, error) {
	if e.Type != "async" || e.Operation == "" {
		return true, nil
	}
	var op struct {
		Status string `json:"status"`
		Err    string `json:"err"`
	}
	if _, err := b.do(ctx, http.MethodGet, e.Operation+"/wait", b.q("timeout", strconv.Itoa(seconds)), nil, &op); err != nil {
		return false, err
	}
	switch op.Status {
	case "Success":
		return true, nil
	case "Failure", "Cancelled":
		return false, connectors.Errorf(connectors.CodeUnavailable, "LXD's operation failed: %s", op.Err)
	}
	return false, nil
}

// Check signs in: the server must trust the client certificate.
func (b *Backend) Check(ctx context.Context) error {
	var s struct {
		Auth string `json:"auth"`
	}
	if _, err := b.do(ctx, http.MethodGet, "/1.0", nil, nil, &s); err != nil {
		return err
	}
	if s.Auth != "trusted" {
		return connectors.Errorf(connectors.CodeAuth, "the LXD server does not trust this connection's certificate")
	}
	return nil
}

// Sizes are the size table, with vms from the setting.
func (b *Backend) Sizes(context.Context, string) ([]infra.ProviderSize, error) {
	out := make([]infra.ProviderSize, len(Sizes))
	for i, s := range Sizes {
		s.VMs = b.st.VMs
		out[i] = s
	}
	return out, nil
}

// Images are Ubuntu's LTS releases.
func (b *Backend) Images(context.Context) ([]providers.Named, error) {
	return []providers.Named{{ID: "ubuntu-24.04", Name: "Ubuntu 24.04 LTS"}, {ID: "ubuntu-22.04", Name: "Ubuntu 22.04 LTS"}}, nil
}

// Regions is the one region the server is.
func (b *Backend) Regions(context.Context) ([]providers.Named, error) {
	return []providers.Named{{ID: b.st.Region, Name: b.st.Region}}, nil
}

func sizeOf(id string) (infra.ProviderSize, bool) {
	for _, s := range Sizes {
		if s.ID == id {
			return s, true
		}
	}
	return infra.ProviderSize{}, false
}

// Name is the instance name for a resource id: LXD names are hostnames.
func Name(id string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	n := strings.Trim(sb.String(), "-")
	if len(n) > 63 {
		n = strings.TrimRight(n[:63], "-")
	}
	if n == "" || (n[0] >= '0' && n[0] <= '9') {
		n = "h-" + n
	}
	return n
}

func alias(image string) string {
	if v, ok := strings.CutPrefix(image, "ubuntu-"); ok {
		return v
	}
	return image
}

// Create makes the virtual machine and waits for its operation.
func (b *Backend) Create(ctx context.Context, in providers.CreateArgs) (infra.ProviderHost, error) {
	size, ok := sizeOf(in.Size)
	if !ok {
		return infra.ProviderHost{}, connectors.Errorf(connectors.CodeInvalid, "there is no size %q", in.Size)
	}
	if in.Region != b.st.Region {
		return infra.ProviderHost{}, connectors.Errorf(connectors.CodeInvalid, "there is no region %q: this server is %q", in.Region, b.st.Region)
	}
	if in.Image == "" {
		in.Image = "ubuntu-24.04"
	}
	cfg := map[string]string{
		HostKey:           in.ID,
		"user.axiom.size": in.Size,
		"limits.cpu":      strconv.Itoa(size.VCPUs),
		"limits.memory":   fmt.Sprintf("%dMiB", size.MemoryMB),
	}
	if in.UserData != "" {
		cfg["cloud-init.user-data"] = in.UserData
	}
	root := map[string]string{"type": "disk", "path": "/", "size": fmt.Sprintf("%dGiB", size.DiskGB)}
	if b.st.StoragePool != "" {
		root["pool"] = b.st.StoragePool
	}
	body := map[string]any{
		"name": Name(in.ID), "type": "virtual-machine", "profiles": []string{b.st.Profile}, "config": cfg,
		"devices": map[string]any{"root": root},
		"source":  map[string]any{"type": "image", "alias": alias(in.Image), "server": b.st.ImageServer, "protocol": "simplestreams"},
		"start":   true,
	}
	q := b.q()
	if b.st.Target != "" {
		q.Set("target", b.st.Target)
	}
	e, err := b.do(ctx, http.MethodPost, "/1.0/instances", q, body, nil)
	if err != nil {
		return infra.ProviderHost{}, err
	}
	done, err := b.wait(ctx, e, b.st.WaitSeconds)
	if err != nil {
		return infra.ProviderHost{}, err
	}
	if !done {
		return infra.ProviderHost{ID: in.ID, ProviderID: Name(in.ID), Name: Name(in.ID), Size: in.Size, Region: b.st.Region, Image: in.Image, State: infra.HostCreating, Nested: in.Nested, Addresses: []string{}}, nil
	}
	return b.Get(ctx, in.ID)
}

// instance is the part of an LXD instance a host needs.
type instance struct {
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Location string            `json:"location"`
	Config   map[string]string `json:"config"`
	State    *struct {
		Network map[string]struct {
			Addresses []struct {
				Family  string `json:"family"`
				Address string `json:"address"`
				Scope   string `json:"scope"`
			} `json:"addresses"`
		} `json:"network"`
	} `json:"state"`
}

func (b *Backend) host(i instance) infra.ProviderHost {
	h := infra.ProviderHost{ID: i.Config[HostKey], ProviderID: i.Name, Name: i.Name, Size: i.Config["user.axiom.size"], Region: b.st.Region, Addresses: []string{}, Nested: b.st.VMs}
	if img := i.Config["image.release"]; img != "" {
		h.Image = "ubuntu-" + img
	}
	switch i.Status {
	case "Running":
		h.State = infra.HostRunning
	case "Stopped", "Error", "Frozen":
		h.State = infra.HostStopped
	default:
		h.State = infra.HostCreating
	}
	if i.State != nil {
		names := make([]string, 0, len(i.State.Network))
		for n := range i.State.Network {
			if n != "lo" {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			for _, a := range i.State.Network[n].Addresses {
				if a.Scope == "global" {
					h.Addresses = append(h.Addresses, a.Address)
				}
			}
		}
	}
	if i.Location != "" && i.Location != "none" {
		h.Facts = map[string]any{"member": i.Location}
	}
	return h
}

// Get reads the instance named for id; one this connection did not make
// (no user.axiom.host of id) is a conflict.
func (b *Backend) Get(ctx context.Context, id string) (infra.ProviderHost, error) {
	var i instance
	if _, err := b.do(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(Name(id)), b.q("recursion", "1"), nil, &i); err != nil {
		return infra.ProviderHost{}, err
	}
	if i.Config[HostKey] != id {
		return infra.ProviderHost{}, connectors.Errorf(connectors.CodeConflict, "an instance named %s is on the server, but this connection did not make it", Name(id))
	}
	return b.host(i), nil
}

// List lists the instances with a user.axiom.host.
func (b *Backend) List(ctx context.Context, region string) ([]infra.ProviderHost, error) {
	if region != "" && region != b.st.Region {
		return nil, nil
	}
	var is []instance
	if _, err := b.do(ctx, http.MethodGet, "/1.0/instances", b.q("recursion", "2"), nil, &is); err != nil {
		return nil, err
	}
	var out []infra.ProviderHost
	for _, i := range is {
		if i.Config[HostKey] != "" {
			out = append(out, b.host(i))
		}
	}
	sort.Slice(out, func(a, c int) bool { return out[a].ID < out[c].ID })
	return out, nil
}

func (b *Backend) state(ctx context.Context, id, action string, force bool) error {
	e, err := b.do(ctx, http.MethodPut, "/1.0/instances/"+url.PathEscape(Name(id))+"/state", b.q(), map[string]any{"action": action, "timeout": 30, "force": force}, nil)
	if err != nil {
		return err
	}
	_, err = b.wait(ctx, e, b.st.WaitSeconds)
	return err
}

// Start starts a stopped host.
func (b *Backend) Start(ctx context.Context, id string) (infra.ProviderHost, error) {
	h, err := b.Get(ctx, id)
	if err != nil || h.State != infra.HostStopped {
		return h, err
	}
	if err := b.state(ctx, id, "start", false); err != nil {
		return h, err
	}
	return b.Get(ctx, id)
}

// Stop stops a running host.
func (b *Backend) Stop(ctx context.Context, id string) (infra.ProviderHost, error) {
	h, err := b.Get(ctx, id)
	if err != nil || h.State != infra.HostRunning {
		return h, err
	}
	if err := b.state(ctx, id, "stop", false); err != nil {
		return h, err
	}
	return b.Get(ctx, id)
}

// Delete stops the host if it runs and deletes it with its root disk.
func (b *Backend) Delete(ctx context.Context, id string) (bool, error) {
	h, err := b.Get(ctx, id)
	if providers.NotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if h.State != infra.HostStopped {
		if err := b.state(ctx, id, "stop", true); err != nil {
			return false, err
		}
	}
	e, err := b.do(ctx, http.MethodDelete, "/1.0/instances/"+url.PathEscape(Name(id)), b.q(), nil, nil)
	if providers.NotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := b.wait(ctx, e, b.st.WaitSeconds); err != nil {
		return false, err
	}
	return true, nil
}
