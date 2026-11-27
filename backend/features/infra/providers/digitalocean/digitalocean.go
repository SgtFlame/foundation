// Package digitalocean is the infra.digitalocean connector type (forge
// 22m): infra.compute@1 on DigitalOcean's API v2 with a personal access
// token. A host is a Droplet named by our resource id and tagged axiom and
// axiom-host:<id>, by which it is found again.
package digitalocean

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/providers"
)

// TypeID is the connector type.
const TypeID = "infra.digitalocean"

// API is DigitalOcean's API root.
const API = "https://api.digitalocean.com"

// Tag marks every Droplet a connection made; HostTag prefixes the tag
// holding our resource id.
const (
	Tag     = "axiom"
	HostTag = "axiom-host:"
)

// SettingsSchema is infra.digitalocean's settings.
const SettingsSchema = `{"type":"object","properties":{
  "token":{"type":"secret","title":"Access token","description":"A personal access token with read and write scope"},
  "sshKeys":{"type":"array","title":"SSH keys","description":"Fingerprints or ids of the account's SSH keys put on every host","items":{"type":"string"}},
  "vpc":{"type":"string","title":"VPC","description":"The VPC's id hosts join; the region's default when empty"}},
  "required":["token"]}`

// Type is infra.digitalocean.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{infra.ComputeRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Meta: connectors.Meta{Title: "DigitalOcean", Description: "Makes hosts as DigitalOcean Droplets with a personal access token: implements infra.compute@1 with the account's sizes and regions (DigitalOcean's slugs), each Droplet named by its resource id and tagged so it is found again. DigitalOcean does not promise nested virtualization, so no size offers vms until a host reports it.", Tags: []string{"infrastructure", "compute", "digitalocean"}},
	}
}

// Settings are a connection's settings, resolved.
type Settings struct {
	Token   string   `json:"token"`
	SSHKeys []string `json:"sshKeys"`
	VPC     string   `json:"vpc"`
}

// Options are for tests: Base replaces the API's root, HTTP the client.
type Options struct {
	Base string
	HTTP *http.Client
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

// Backend is one connection's account.
type Backend struct {
	st Settings
	c  providers.Client
}

// New makes a backend from settings.
func New(st Settings, o Options) (*Backend, error) {
	if st.Token == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the access token is required")
	}
	base := o.Base
	if base == "" {
		base = API
	}
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: providers.DefaultTimeout}
	}
	token := st.Token
	return &Backend{st: st, c: providers.Client{Base: base, HTTP: hc, Message: message,
		Auth: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }}}, nil
}

func message(body []byte) string {
	var e struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Message
}

// links is a page's links.
type links struct {
	Pages struct {
		Next string `json:"next"`
	} `json:"pages"`
}

// list gets every page of a list whose items are under key.
func list[T any](ctx context.Context, b *Backend, path string, q url.Values, key string) ([]T, error) {
	var all []T
	for n := 0; n < 100; n++ {
		var p map[string]json.RawMessage
		if _, err := b.c.Do(ctx, http.MethodGet, path, q, nil, &p); err != nil {
			return nil, err
		}
		var items []T
		if raw, ok := p[key]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, connectors.Errorf(connectors.CodeUnavailable, "DigitalOcean's %s are not what they should be: %v", key, err)
			}
		}
		all = append(all, items...)
		var l links
		_ = json.Unmarshal(p["links"], &l)
		if l.Pages.Next == "" {
			break
		}
		u, err := url.Parse(l.Pages.Next)
		if err != nil {
			break
		}
		path, q = u.Path, u.Query()
	}
	return all, nil
}

// Check reads the account.
func (b *Backend) Check(ctx context.Context) error {
	var a struct {
		Account struct {
			Status string `json:"status"`
		} `json:"account"`
	}
	if _, err := b.c.Do(ctx, http.MethodGet, "/v2/account", nil, nil, &a); err != nil {
		return err
	}
	if a.Account.Status != "" && a.Account.Status != "active" {
		return connectors.Errorf(connectors.CodeAuth, "the DigitalOcean account is %s", a.Account.Status)
	}
	return nil
}

type size struct {
	Slug        string   `json:"slug"`
	Memory      int      `json:"memory"`
	VCPUs       int      `json:"vcpus"`
	Disk        int      `json:"disk"`
	PriceHourly float64  `json:"price_hourly"`
	Regions     []string `json:"regions"`
	Available   bool     `json:"available"`
}

// Sizes lists the available sizes (in region, when given).
func (b *Backend) Sizes(ctx context.Context, region string) ([]infra.ProviderSize, error) {
	all, err := list[size](ctx, b, "/v2/sizes", url.Values{"per_page": {"200"}}, "sizes")
	if err != nil {
		return nil, err
	}
	var out []infra.ProviderSize
	for _, s := range all {
		if !s.Available || (region != "" && !contains(s.Regions, region)) {
			continue
		}
		out = append(out, infra.ProviderSize{ID: s.Slug, VCPUs: s.VCPUs, MemoryMB: s.Memory, DiskGB: s.Disk, Price: s.PriceHourly})
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Images are Ubuntu's LTS releases.
func (b *Backend) Images(context.Context) ([]providers.Named, error) {
	return []providers.Named{{ID: "ubuntu-24.04", Name: "Ubuntu 24.04 LTS"}, {ID: "ubuntu-22.04", Name: "Ubuntu 22.04 LTS"}}, nil
}

// Slug is DigitalOcean's image slug for an image id.
func Slug(image string) string {
	if v, ok := strings.CutPrefix(image, "ubuntu-"); ok {
		return "ubuntu-" + strings.ReplaceAll(v, ".", "-") + "-x64"
	}
	return image
}

// Regions lists the available regions.
func (b *Backend) Regions(ctx context.Context) ([]providers.Named, error) {
	type region struct {
		Slug      string `json:"slug"`
		Name      string `json:"name"`
		Available bool   `json:"available"`
	}
	all, err := list[region](ctx, b, "/v2/regions", url.Values{"per_page": {"200"}}, "regions")
	if err != nil {
		return nil, err
	}
	var out []providers.Named
	for _, r := range all {
		if r.Available {
			out = append(out, providers.Named{ID: r.Slug, Name: r.Name})
		}
	}
	return out, nil
}

// droplet is the part of a Droplet a host needs.
type droplet struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	SizeSlug string   `json:"size_slug"`
	Tags     []string `json:"tags"`
	Region   struct {
		Slug string `json:"slug"`
	} `json:"region"`
	Image struct {
		Slug string `json:"slug"`
	} `json:"image"`
	Networks struct {
		V4 []struct {
			IPAddress string `json:"ip_address"`
			Type      string `json:"type"`
		} `json:"v4"`
	} `json:"networks"`
	CreatedAt string `json:"created_at"`
}

func host(d droplet) infra.ProviderHost {
	h := infra.ProviderHost{ProviderID: strconv.FormatInt(d.ID, 10), Name: d.Name, Size: d.SizeSlug, Region: d.Region.Slug, Addresses: []string{}}
	for _, t := range d.Tags {
		if id, ok := strings.CutPrefix(t, HostTag); ok {
			h.ID = id
		}
	}
	if v, ok := strings.CutPrefix(d.Image.Slug, "ubuntu-"); ok {
		h.Image = "ubuntu-" + strings.Replace(strings.TrimSuffix(v, "-x64"), "-", ".", 1)
	}
	switch d.Status {
	case "active":
		h.State = infra.HostRunning
	case "off":
		h.State = infra.HostStopped
	case "archive":
		h.State = infra.HostGone
	default:
		h.State = infra.HostCreating
	}
	for _, kind := range []string{"public", "private"} {
		for _, n := range d.Networks.V4 {
			if n.Type == kind {
				h.Addresses = append(h.Addresses, n.IPAddress)
			}
		}
	}
	return h
}

// HostTagOf is the tag naming a resource id. DigitalOcean's tags take
// letters, digits, colons, dashes and underscores.
func HostTagOf(id string) string { return HostTag + providers.Hostname(id) }

func (b *Backend) find(ctx context.Context, tag string) ([]droplet, error) {
	return list[droplet](ctx, b, "/v2/droplets", url.Values{"per_page": {"200"}, "tag_name": {tag}}, "droplets")
}

func (b *Backend) droplet(ctx context.Context, id string) (droplet, error) {
	ds, err := b.find(ctx, HostTagOf(id))
	if err != nil {
		return droplet{}, err
	}
	for _, d := range ds {
		if d.Status != "archive" {
			return d, nil
		}
	}
	return droplet{}, connectors.Errorf(connectors.CodeNotFound, "there is no host %q", id)
}

// Create makes the Droplet; it answers new (creating) at once.
func (b *Backend) Create(ctx context.Context, in providers.CreateArgs) (infra.ProviderHost, error) {
	if in.Image == "" {
		in.Image = "ubuntu-24.04"
	}
	body := map[string]any{"name": providers.Hostname(in.Name), "region": in.Region, "size": in.Size, "image": Slug(in.Image),
		"tags": []string{Tag, HostTagOf(in.ID)}, "monitoring": true}
	if in.UserData != "" {
		body["user_data"] = in.UserData
	}
	if len(b.st.SSHKeys) > 0 {
		body["ssh_keys"] = b.st.SSHKeys
	}
	if b.st.VPC != "" {
		body["vpc_uuid"] = b.st.VPC
	}
	var out struct {
		Droplet droplet `json:"droplet"`
	}
	if _, err := b.c.Do(ctx, http.MethodPost, "/v2/droplets", nil, body, &out); err != nil {
		return infra.ProviderHost{}, err
	}
	h := host(out.Droplet)
	h.ID = in.ID
	return h, nil
}

// Get finds the Droplet by its tag.
func (b *Backend) Get(ctx context.Context, id string) (infra.ProviderHost, error) {
	d, err := b.droplet(ctx, id)
	if err != nil {
		return infra.ProviderHost{}, err
	}
	h := host(d)
	h.ID = id
	return h, nil
}

// List lists the Droplets tagged axiom.
func (b *Backend) List(ctx context.Context, region string) ([]infra.ProviderHost, error) {
	ds, err := b.find(ctx, Tag)
	if err != nil {
		return nil, err
	}
	var out []infra.ProviderHost
	for _, d := range ds {
		h := host(d)
		if h.ID != "" && (region == "" || h.Region == region) {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(a, c int) bool { return out[a].ID < out[c].ID })
	return out, nil
}

func (b *Backend) action(ctx context.Context, d droplet, typ string) error {
	_, err := b.c.Do(ctx, http.MethodPost, "/v2/droplets/"+strconv.FormatInt(d.ID, 10)+"/actions", nil, map[string]any{"type": typ}, nil)
	return err
}

// Start powers a stopped Droplet on; it is running once DigitalOcean's
// action ends (host.get).
func (b *Backend) Start(ctx context.Context, id string) (infra.ProviderHost, error) {
	d, err := b.droplet(ctx, id)
	if err != nil {
		return infra.ProviderHost{}, err
	}
	h := host(d)
	h.ID = id
	if h.State != infra.HostStopped {
		return h, nil
	}
	if err := b.action(ctx, d, "power_on"); err != nil {
		return h, err
	}
	return h, nil
}

// Stop shuts a running Droplet down.
func (b *Backend) Stop(ctx context.Context, id string) (infra.ProviderHost, error) {
	d, err := b.droplet(ctx, id)
	if err != nil {
		return infra.ProviderHost{}, err
	}
	h := host(d)
	h.ID = id
	if h.State != infra.HostRunning {
		return h, nil
	}
	if err := b.action(ctx, d, "shutdown"); err != nil {
		return h, err
	}
	return h, nil
}

// Delete deletes the Droplet with its disk.
func (b *Backend) Delete(ctx context.Context, id string) (bool, error) {
	d, err := b.droplet(ctx, id)
	if providers.NotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = b.c.Do(ctx, http.MethodDelete, "/v2/droplets/"+strconv.FormatInt(d.ID, 10), nil, nil, nil)
	if providers.NotFound(err) {
		return false, nil
	}
	return err == nil, err
}
