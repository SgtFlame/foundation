// Package azure is the infra.azure connector type (forge 22l):
// infra.compute@1 on the Azure Resource Manager REST API, signed in as a
// service principal with a client secret (OAuth2 client credentials). A
// host is a resource group of its own, <prefix>-host-<host>, tagged
// axiom-host=<id>, holding the VM, its network, public address and
// interface, so deleting the group removes everything the host made.
//
// Resource groups follow one naming standard, <prefix>-<kind>-<name>
// (GroupName): the prefix is axiom in the primary organization's
// subscription and axiom-<tenant short name> in a tenant's own; the kinds
// are host (one per host) and net (networking shared by a location's
// hosts, such as a cluster's network).
package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/providers"
)

// TypeID is the connector type.
const TypeID = "infra.azure"

// The service roots, and the API versions used.
const (
	Login          = "https://login.microsoftonline.com"
	Management     = "https://management.azure.com"
	groupsVersion  = "2021-04-01"
	computeVersion = "2024-03-01"
	networkVersion = "2023-09-01"
	skusVersion    = "2021-07-01"
	subsVersion    = "2022-12-01"
)

// HostTag is the tag holding our resource id.
const HostTag = "axiom-host"

// SettingsSchema is infra.azure's settings.
const SettingsSchema = `{"type":"object","properties":{
  "tenantId":{"type":"string","title":"Directory (tenant) id","description":"The Microsoft Entra tenant the service principal is in"},
  "clientId":{"type":"string","title":"Application (client) id","description":"The service principal's application id"},
  "clientSecret":{"type":"secret","title":"Client secret","description":"The service principal's client secret"},
  "subscriptionId":{"type":"string","title":"Subscription","description":"The subscription hosts are made in; the service principal needs Contributor on it"},
  "location":{"type":"string","title":"Default location","description":"The location size.list uses when no region is given, e.g. westeurope"},
  "resourceGroupPrefix":{"type":"string","title":"Resource group prefix","description":"Resource groups are named <prefix>-<kind>-<name>, a host's <prefix>-host-<host>: axiom in the primary organization's subscription (the default), axiom-<tenant short name> in a tenant's own"},
  "adminUsername":{"type":"string","title":"Admin user","description":"The VM's admin user; axiom when empty"},
  "sshPublicKey":{"type":"string","title":"SSH public key","description":"The admin user's SSH public key (password sign-in is off)"},
  "waitSeconds":{"type":"integer","title":"Wait","description":"How long host.create waits for Azure's operations before answering creating; 120 seconds when empty","unit":"s","min":1,"max":1800}},
  "required":["tenantId","clientId","clientSecret","subscriptionId","sshPublicKey"]}`

// Type is infra.azure.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{infra.ComputeRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Meta: connectors.Meta{Title: "Azure", Description: "Makes hosts as Azure virtual machines through Azure Resource Manager, signed in as a service principal: implements infra.compute@1 with the subscription's VM sizes (vms on the families that run nested virtualization, Dv3 and Ev3 and later) and locations. Each host is a resource group of its own, <prefix>-host-<host>, holding its VM, network and public address, tagged with its resource id; groups follow the naming standard <prefix>-<kind>-<name>.", Tags: []string{"infrastructure", "compute", "azure"}},
	}
}

// Settings are a connection's settings, resolved.
type Settings struct {
	TenantID            string `json:"tenantId"`
	ClientID            string `json:"clientId"`
	ClientSecret        string `json:"clientSecret"`
	SubscriptionID      string `json:"subscriptionId"`
	Location            string `json:"location"`
	ResourceGroupPrefix string `json:"resourceGroupPrefix"`
	AdminUsername       string `json:"adminUsername"`
	SSHPublicKey        string `json:"sshPublicKey"`
	WaitSeconds         int    `json:"waitSeconds"`
}

// Options are for tests: Login and Management replace the service roots,
// HTTP the client, and Poll the pause between polls of an operation (2 s
// when zero; negative for none).
type Options struct {
	Login, Management string
	HTTP              *http.Client
	Poll              time.Duration
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

// Backend is one connection's subscription.
type Backend struct {
	st    Settings
	o     Options
	login providers.Client
	c     providers.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New makes a backend from settings.
func New(st Settings, o Options) (*Backend, error) {
	for name, v := range map[string]string{"tenantId": st.TenantID, "clientId": st.ClientID, "clientSecret": st.ClientSecret, "subscriptionId": st.SubscriptionID, "sshPublicKey": st.SSHPublicKey} {
		if v == "" {
			return nil, connectors.Errorf(connectors.CodeInvalid, "%s is required", name)
		}
	}
	if st.ResourceGroupPrefix == "" {
		st.ResourceGroupPrefix = "axiom"
	}
	if !prefixRE.MatchString(st.ResourceGroupPrefix) {
		return nil, connectors.Errorf(connectors.CodeInvalid, "resourceGroupPrefix %q is lower case letters, digits and dashes, at most 40, starting with a letter", st.ResourceGroupPrefix)
	}
	if st.AdminUsername == "" {
		st.AdminUsername = "axiom"
	}
	if st.WaitSeconds <= 0 {
		st.WaitSeconds = 120
	}
	if o.Login == "" {
		o.Login = Login
	}
	if o.Management == "" {
		o.Management = Management
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: providers.DefaultTimeout}
	}
	if o.Poll == 0 {
		o.Poll = 2 * time.Second
	}
	b := &Backend{st: st, o: o}
	b.login = providers.Client{Base: o.Login, HTTP: o.HTTP, Message: loginMessage}
	b.c = providers.Client{Base: o.Management, HTTP: o.HTTP, Message: message, Auth: b.auth}
	return b, nil
}

func message(body []byte) string {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Error.Code != "" {
		return e.Error.Code + ": " + e.Error.Message
	}
	return e.Error.Message
}

func loginMessage(body []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Error == "" {
		return ""
	}
	first, _, _ := strings.Cut(e.Description, "\r\n")
	return e.Error + ": " + first
}

// auth sets the bearer token signIn got (the client Do runs it after
// signIn, under the same call).
func (b *Backend) auth(r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r.Header.Set("Authorization", "Bearer "+b.token)
}

// signIn gets a token unless the cached one lasts another minute.
func (b *Backend) signIn(ctx context.Context) error {
	b.mu.Lock()
	ok := b.token != "" && time.Now().Add(time.Minute).Before(b.expires)
	b.mu.Unlock()
	if ok {
		return nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {b.st.ClientID}, "client_secret": {b.st.ClientSecret}, "scope": {strings.TrimRight(Management, "/") + "/.default"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.o.Login, "/")+"/"+url.PathEscape(b.st.TenantID)+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	if err != nil {
		return connectors.Errorf(connectors.CodeInvalid, "the tenant id is not valid")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := b.o.HTTP.Do(req)
	if err != nil {
		return connectors.Errorf(connectors.CodeUnavailable, "Microsoft Entra cannot be reached")
	}
	defer res.Body.Close()
	var t struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
	}
	var raw json.RawMessage
	_ = json.NewDecoder(res.Body).Decode(&raw)
	if code := providers.Status(res.StatusCode); code != "" {
		if res.StatusCode == http.StatusBadRequest {
			code = connectors.CodeAuth // a wrong secret or client is 400 invalid_client
		}
		return connectors.Errorf(code, "signing in to Microsoft Entra: %d %s", res.StatusCode, loginMessage(raw))
	}
	_ = json.Unmarshal(raw, &t)
	secs, _ := strconv.Atoi(strings.Trim(string(t.ExpiresIn), `"`))
	if t.AccessToken == "" {
		return connectors.Errorf(connectors.CodeAuth, "Microsoft Entra gave no token")
	}
	b.mu.Lock()
	b.token, b.expires = t.AccessToken, time.Now().Add(time.Duration(secs)*time.Second)
	b.mu.Unlock()
	return nil
}

func (b *Backend) do(ctx context.Context, method, path, version string, q url.Values, body, out any) (http.Header, error) {
	if err := b.signIn(ctx); err != nil {
		return nil, err
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("api-version", version)
	return b.c.Do(ctx, method, path, q, body, out)
}

func (b *Backend) sub() string { return "/subscriptions/" + url.PathEscape(b.st.SubscriptionID) }

// The kinds of resource group in the naming standard.
const (
	// GroupHost is a host's own group: its VM, interface, address and,
	// until hosts share one, its network.
	GroupHost = "host"
	// GroupNetwork is networking a location's hosts share.
	GroupNetwork = "net"
)

var prefixRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// GroupName is the naming standard's resource group name,
// <prefix>-<kind>-<name>, cut to Azure's 90 characters.
func GroupName(prefix, kind, name string) string {
	n := prefix + "-" + kind + "-" + providers.Hostname(name)
	if len(n) > 90 {
		n = strings.TrimRight(n[:90], "-")
	}
	return n
}

// Group is a host's resource group, <prefix>-host-<host>.
func (b *Backend) Group(id string) string {
	return GroupName(b.st.ResourceGroupPrefix, GroupHost, id)
}

func (b *Backend) groupPath(id string) string {
	return b.sub() + "/resourceGroups/" + url.PathEscape(b.Group(id))
}

func (b *Backend) vmPath(id string) string {
	return b.groupPath(id) + "/providers/Microsoft.Compute/virtualMachines/" + url.PathEscape(providers.Hostname(id))
}

func (b *Backend) netPath(id, kind, suffix string) string {
	return b.groupPath(id) + "/providers/Microsoft.Network/" + kind + "/" + url.PathEscape(providers.Hostname(id)+suffix)
}

// wait polls an operation (Azure-AsyncOperation, else Location) until it
// ends or seconds pass; done false when it still runs.
func (b *Backend) wait(ctx context.Context, h http.Header, seconds int) (bool, error) {
	op, loc := h.Get("Azure-AsyncOperation"), h.Get("Location")
	if op == "" && loc == "" {
		return true, nil
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for {
		if op != "" {
			u, err := url.Parse(op)
			if err != nil {
				return true, nil
			}
			var s struct {
				Status string `json:"status"`
				Error  struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if _, err := b.c.Do(ctx, http.MethodGet, u.Path, u.Query(), nil, &s); err != nil {
				return false, err
			}
			switch s.Status {
			case "Succeeded":
				return true, nil
			case "Failed", "Canceled":
				return false, connectors.Errorf(connectors.CodeUnavailable, "Azure's operation failed: %s %s", s.Error.Code, s.Error.Message)
			}
		} else {
			u, err := url.Parse(loc)
			if err != nil {
				return true, nil
			}
			res, err := b.c.HTTP.Do(b.request(ctx, u))
			if err != nil {
				return false, connectors.Errorf(connectors.CodeUnavailable, "Azure cannot be reached")
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusAccepted {
				if code := providers.Status(res.StatusCode); code != "" {
					return false, connectors.Errorf(code, "Azure's operation failed: %d", res.StatusCode)
				}
				return true, nil
			}
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		if b.o.Poll > 0 {
			select {
			case <-ctx.Done():
				return false, connectors.Errorf(connectors.CodeUnavailable, "cancelled")
			case <-time.After(b.o.Poll):
			}
		}
	}
}

func (b *Backend) request(ctx context.Context, u *url.URL) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(b.o.Management, "/")+u.Path+"?"+u.RawQuery, nil)
	b.auth(req)
	return req
}

// Check signs in and reads the subscription.
func (b *Backend) Check(ctx context.Context) error {
	var s struct {
		State string `json:"state"`
	}
	if _, err := b.do(ctx, http.MethodGet, b.sub(), subsVersion, nil, nil, &s); err != nil {
		return err
	}
	if s.State != "" && s.State != "Enabled" {
		return connectors.Errorf(connectors.CodeAuth, "the subscription is %s", s.State)
	}
	return nil
}

// nested are the families that run nested virtualization: D and E of
// version 3 and later.
var nested = regexp.MustCompile(`^Standard_[DE]\d+[a-z]*_v([3-9]|\d\d)$`)

// NestedCapable tells whether a VM size runs nested virtualization.
func NestedCapable(size string) bool { return nested.MatchString(size) }

// Sizes lists the VM sizes offered in region (the default location when
// empty) with no restriction.
func (b *Backend) Sizes(ctx context.Context, region string) ([]infra.ProviderSize, error) {
	if region == "" {
		region = b.st.Location
	}
	q := url.Values{}
	if region != "" {
		q.Set("$filter", "location eq '"+region+"'")
	}
	var r struct {
		Value []struct {
			ResourceType string   `json:"resourceType"`
			Name         string   `json:"name"`
			Locations    []string `json:"locations"`
			Capabilities []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"capabilities"`
			Restrictions []json.RawMessage `json:"restrictions"`
		} `json:"value"`
	}
	if _, err := b.do(ctx, http.MethodGet, b.sub()+"/providers/Microsoft.Compute/skus", skusVersion, q, nil, &r); err != nil {
		return nil, err
	}
	var out []infra.ProviderSize
	for _, s := range r.Value {
		if s.ResourceType != "virtualMachines" || len(s.Restrictions) > 0 {
			continue
		}
		z := infra.ProviderSize{ID: s.Name, VMs: NestedCapable(s.Name)}
		for _, c := range s.Capabilities {
			switch c.Name {
			case "vCPUs":
				z.VCPUs, _ = strconv.Atoi(c.Value)
			case "MemoryGB":
				f, _ := strconv.ParseFloat(c.Value, 64)
				z.MemoryMB = int(f * 1024)
			case "MaxResourceVolumeMB":
				mb, _ := strconv.Atoi(c.Value)
				z.DiskGB = mb / 1024
			}
		}
		out = append(out, z)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Images are Ubuntu's LTS releases from Canonical.
func (b *Backend) Images(context.Context) ([]providers.Named, error) {
	return []providers.Named{{ID: "ubuntu-24.04", Name: "Ubuntu 24.04 LTS"}, {ID: "ubuntu-22.04", Name: "Ubuntu 22.04 LTS"}}, nil
}

// ImageReference is Canonical's image for an image id.
func ImageReference(image string) map[string]string {
	switch image {
	case "ubuntu-22.04":
		return map[string]string{"publisher": "Canonical", "offer": "0001-com-ubuntu-server-jammy", "sku": "22_04-lts-gen2", "version": "latest"}
	default:
		return map[string]string{"publisher": "Canonical", "offer": "ubuntu-24_04-lts", "sku": "server", "version": "latest"}
	}
}

// Regions lists the subscription's physical locations.
func (b *Backend) Regions(ctx context.Context) ([]providers.Named, error) {
	var r struct {
		Value []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Metadata    struct {
				RegionType string `json:"regionType"`
			} `json:"metadata"`
		} `json:"value"`
	}
	if _, err := b.do(ctx, http.MethodGet, b.sub()+"/locations", subsVersion, nil, nil, &r); err != nil {
		return nil, err
	}
	var out []providers.Named
	for _, l := range r.Value {
		if l.Metadata.RegionType == "" || l.Metadata.RegionType == "Physical" {
			out = append(out, providers.Named{ID: l.Name, Name: l.DisplayName})
		}
	}
	return out, nil
}

func (b *Backend) tags(id string) map[string]string {
	return map[string]string{HostTag: id, "axiom": "true"}
}

// put PUTs a resource and waits for it.
func (b *Backend) put(ctx context.Context, path, version string, body any, seconds int) (bool, error) {
	h, err := b.do(ctx, http.MethodPut, path, version, nil, body, nil)
	if err != nil {
		return false, err
	}
	return b.wait(ctx, h, seconds)
}

// Create makes the host's resource group, network, public address,
// interface and VM, each PUT idempotent, and waits for the VM up to the
// connection's wait.
func (b *Backend) Create(ctx context.Context, in providers.CreateArgs) (infra.ProviderHost, error) {
	if in.Image == "" {
		in.Image = "ubuntu-24.04"
	}
	name := providers.Hostname(in.ID)
	loc := in.Region
	tags := b.tags(in.ID)
	steps := []struct {
		path, version string
		body          any
	}{
		{b.groupPath(in.ID), groupsVersion, map[string]any{"location": loc, "tags": tags}},
		{b.netPath(in.ID, "virtualNetworks", "-vnet"), networkVersion, map[string]any{"location": loc, "tags": tags, "properties": map[string]any{
			"addressSpace": map[string]any{"addressPrefixes": []string{"10.0.0.0/16"}},
			"subnets":      []any{map[string]any{"name": "default", "properties": map[string]any{"addressPrefix": "10.0.0.0/24"}}}}}},
		{b.netPath(in.ID, "publicIPAddresses", "-ip"), networkVersion, map[string]any{"location": loc, "tags": tags, "sku": map[string]any{"name": "Standard"},
			"properties": map[string]any{"publicIPAllocationMethod": "Static"}}},
		{b.netPath(in.ID, "networkInterfaces", "-nic"), networkVersion, map[string]any{"location": loc, "tags": tags, "properties": map[string]any{
			"ipConfigurations": []any{map[string]any{"name": "ipconfig1", "properties": map[string]any{
				"subnet":          map[string]any{"id": b.netPath(in.ID, "virtualNetworks", "-vnet") + "/subnets/default"},
				"publicIPAddress": map[string]any{"id": b.netPath(in.ID, "publicIPAddresses", "-ip")}}}}}}},
	}
	for _, s := range steps {
		done, err := b.put(ctx, s.path, s.version, s.body, b.st.WaitSeconds)
		if err != nil {
			return infra.ProviderHost{}, err
		}
		if !done {
			return infra.ProviderHost{}, connectors.Errorf(connectors.CodeUnavailable, "Azure is still making %s's network; create it again", in.ID)
		}
	}
	os := map[string]any{"computerName": name, "adminUsername": b.st.AdminUsername, "linuxConfiguration": map[string]any{
		"disablePasswordAuthentication": true,
		"ssh":                           map[string]any{"publicKeys": []any{map[string]any{"path": "/home/" + b.st.AdminUsername + "/.ssh/authorized_keys", "keyData": b.st.SSHPublicKey}}}}}
	if in.UserData != "" {
		os["customData"] = base64.StdEncoding.EncodeToString([]byte(in.UserData))
	}
	vm := map[string]any{"location": loc, "tags": tags, "properties": map[string]any{
		"hardwareProfile": map[string]any{"vmSize": in.Size},
		"storageProfile": map[string]any{"imageReference": ImageReference(in.Image),
			"osDisk": map[string]any{"createOption": "FromImage", "deleteOption": "Delete", "managedDisk": map[string]any{"storageAccountType": "Premium_LRS"}}},
		"osProfile":      os,
		"networkProfile": map[string]any{"networkInterfaces": []any{map[string]any{"id": b.netPath(in.ID, "networkInterfaces", "-nic"), "properties": map[string]any{"deleteOption": "Delete"}}}}}}
	done, err := b.put(ctx, b.vmPath(in.ID), computeVersion, vm, b.st.WaitSeconds)
	if err != nil {
		return infra.ProviderHost{}, err
	}
	if !done {
		return infra.ProviderHost{ID: in.ID, ProviderID: name, Name: name, Size: in.Size, Region: loc, Image: in.Image, State: infra.HostCreating, Nested: in.Nested, Addresses: []string{}}, nil
	}
	return b.Get(ctx, in.ID)
}

// vm is the part of a VM a host needs.
type vm struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags"`
	Properties struct {
		ProvisioningState string `json:"provisioningState"`
		HardwareProfile   struct {
			VMSize string `json:"vmSize"`
		} `json:"hardwareProfile"`
		StorageProfile struct {
			ImageReference struct {
				Offer string `json:"offer"`
			} `json:"imageReference"`
		} `json:"storageProfile"`
		InstanceView *struct {
			Statuses []struct {
				Code string `json:"code"`
			} `json:"statuses"`
		} `json:"instanceView"`
	} `json:"properties"`
}

func host(v vm) infra.ProviderHost {
	h := infra.ProviderHost{ID: v.Tags[HostTag], ProviderID: v.ID, Name: v.Name, Size: v.Properties.HardwareProfile.VMSize, Region: v.Location, Addresses: []string{}, Nested: NestedCapable(v.Properties.HardwareProfile.VMSize)}
	switch {
	case strings.Contains(v.Properties.StorageProfile.ImageReference.Offer, "24_04"):
		h.Image = "ubuntu-24.04"
	case strings.Contains(v.Properties.StorageProfile.ImageReference.Offer, "jammy"):
		h.Image = "ubuntu-22.04"
	}
	power := ""
	if v.Properties.InstanceView != nil {
		for _, s := range v.Properties.InstanceView.Statuses {
			if p, ok := strings.CutPrefix(s.Code, "PowerState/"); ok {
				power = p
			}
		}
	}
	switch v.Properties.ProvisioningState {
	case "Creating", "Updating", "Migrating":
		h.State = infra.HostCreating
	case "Deleting":
		h.State = infra.HostDeleting
	default:
		switch power {
		case "running":
			h.State = infra.HostRunning
		case "stopped", "deallocated", "stopping", "deallocating":
			h.State = infra.HostStopped
		case "starting":
			h.State = infra.HostCreating
		default:
			if v.Properties.ProvisioningState == "Failed" {
				h.State = infra.HostStopped
			} else {
				h.State = infra.HostCreating
			}
		}
	}
	return h
}

// Get reads the VM with its instance view and public address; a missing
// resource group or VM is notFound.
func (b *Backend) Get(ctx context.Context, id string) (infra.ProviderHost, error) {
	var v vm
	if _, err := b.do(ctx, http.MethodGet, b.vmPath(id), computeVersion, url.Values{"$expand": {"instanceView"}}, nil, &v); err != nil {
		return infra.ProviderHost{}, err
	}
	if v.Tags[HostTag] != id {
		return infra.ProviderHost{}, connectors.Errorf(connectors.CodeConflict, "a VM named %s is in %s, but this connection did not make it", v.Name, b.Group(id))
	}
	h := host(v)
	var ip struct {
		Properties struct {
			IPAddress string `json:"ipAddress"`
		} `json:"properties"`
	}
	if _, err := b.do(ctx, http.MethodGet, b.netPath(id, "publicIPAddresses", "-ip"), networkVersion, nil, nil, &ip); err == nil && ip.Properties.IPAddress != "" {
		h.Addresses = append(h.Addresses, ip.Properties.IPAddress)
	}
	return h, nil
}

// List lists the subscription's VMs tagged axiom-host, with their power
// state.
func (b *Backend) List(ctx context.Context, region string) ([]infra.ProviderHost, error) {
	var out []infra.ProviderHost
	path, q := b.sub()+"/providers/Microsoft.Compute/virtualMachines", url.Values{"statusOnly": {"true"}}
	for n := 0; n < 100; n++ {
		var r struct {
			Value    []vm   `json:"value"`
			NextLink string `json:"nextLink"`
		}
		if _, err := b.do(ctx, http.MethodGet, path, computeVersion, q, nil, &r); err != nil {
			return nil, err
		}
		for _, v := range r.Value {
			if v.Tags[HostTag] != "" && (region == "" || v.Location == region) {
				out = append(out, host(v))
			}
		}
		if r.NextLink == "" {
			break
		}
		u, err := url.Parse(r.NextLink)
		if err != nil {
			break
		}
		path, q = u.Path, u.Query()
		q.Del("api-version")
	}
	sort.Slice(out, func(a, c int) bool { return out[a].ID < out[c].ID })
	return out, nil
}

func (b *Backend) power(ctx context.Context, id, action string) error {
	h, err := b.do(ctx, http.MethodPost, b.vmPath(id)+"/"+action, computeVersion, nil, nil, nil)
	if err != nil {
		return err
	}
	_, err = b.wait(ctx, h, b.st.WaitSeconds)
	return err
}

// Start starts a stopped (deallocated) VM.
func (b *Backend) Start(ctx context.Context, id string) (infra.ProviderHost, error) {
	h, err := b.Get(ctx, id)
	if err != nil || h.State != infra.HostStopped {
		return h, err
	}
	if err := b.power(ctx, id, "start"); err != nil {
		return h, err
	}
	return b.Get(ctx, id)
}

// Stop deallocates a running VM, keeping its disk and stopping its
// compute billing.
func (b *Backend) Stop(ctx context.Context, id string) (infra.ProviderHost, error) {
	h, err := b.Get(ctx, id)
	if err != nil || h.State != infra.HostRunning {
		return h, err
	}
	if err := b.power(ctx, id, "deallocate"); err != nil {
		return h, err
	}
	return b.Get(ctx, id)
}

// Delete deletes the host's resource group (whose tag must name it),
// with everything in it, and waits for it.
func (b *Backend) Delete(ctx context.Context, id string) (bool, error) {
	var g struct {
		Tags map[string]string `json:"tags"`
	}
	if _, err := b.do(ctx, http.MethodGet, b.groupPath(id), groupsVersion, nil, nil, &g); providers.NotFound(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if g.Tags[HostTag] != id {
		return false, connectors.Errorf(connectors.CodeConflict, "resource group %s is not this connection's host %s", b.Group(id), id)
	}
	h, err := b.do(ctx, http.MethodDelete, b.groupPath(id), groupsVersion, nil, nil, nil)
	if providers.NotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	done, err := b.wait(ctx, h, b.st.WaitSeconds)
	if err != nil {
		return false, err
	}
	_ = done // still deleting: host.get reports it deleting until it is gone
	return true, nil
}
