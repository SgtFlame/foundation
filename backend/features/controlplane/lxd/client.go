// Package lxd is a small client of the LXD (or Incus) REST API over its
// local Unix socket (forge 20c-2): instances, snapshots, profiles,
// operations, storage pools, networks and the server's info. It has no
// LXD SDK, as secrets/vault has no Vault SDK.
package lxd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("controlplane").Tags("lxd")

// Sockets are where LXD listens, in order of preference (the snap's, a
// package's, Incus's).
var Sockets = []string{"/var/snap/lxd/common/lxd/unix.socket", "/var/lib/lxd/unix.socket", "/var/lib/incus/unix.socket"}

// Client talks to one LXD server.
type Client struct {
	http *http.Client
	base string
}

// Dial finds a local socket (or uses socket when given).
func Dial(socket string) (*Client, error) {
	if socket == "" {
		for _, s := range Sockets {
			if _, err := os.Stat(s); err == nil {
				socket = s
				break
			}
		}
	}
	if socket == "" {
		return nil, log.Code("no_lxd").Errorf("no LXD socket found")
	}
	return New(socket), nil
}

// New is a client of the server on a Unix socket.
func New(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{http: &http.Client{Transport: tr}, base: "http://lxd"}
}

// Error is LXD's error answer; Status is its HTTP status (404 for a
// missing instance or profile).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("lxd: %d %s", e.Status, e.Message) }

// IsNotFound tells a missing object.
func IsNotFound(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Status == http.StatusNotFound
}

type response struct {
	Type       string          `json:"type"` // sync, async, error
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	Operation  string          `json:"operation"`
	ErrorCode  int             `json:"error_code"`
	Error      string          `json:"error"`
	Metadata   json.RawMessage `json:"metadata"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (*response, error) {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, log.Wrap(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, log.Wrapf(err, "reach LXD")
	}
	defer res.Body.Close()
	var r response
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, log.Wrapf(err, "read LXD's answer to %s %s", method, path)
	}
	if r.Type == "error" || res.StatusCode >= 400 {
		code := r.ErrorCode
		if code == 0 {
			code = res.StatusCode
		}
		return nil, &Error{Status: code, Message: r.Error}
	}
	if out != nil && len(r.Metadata) > 0 {
		if err := json.Unmarshal(r.Metadata, out); err != nil {
			return nil, log.Wrap(err)
		}
	}
	return &r, nil
}

// wait waits for an async answer's operation to end.
func (c *Client) wait(ctx context.Context, r *response) error {
	if r == nil || r.Type != "async" || r.Operation == "" {
		return nil
	}
	for {
		var op Operation
		_, err := c.do(ctx, http.MethodGet, r.Operation+"/wait?timeout=30", nil, &op)
		if err != nil {
			return err
		}
		switch op.Status {
		case "Success":
			return nil
		case "Failure", "Cancelled":
			return &Error{Status: http.StatusInternalServerError, Message: op.Err}
		}
		if err := ctx.Err(); err != nil {
			return log.Wrap(err)
		}
	}
}

// Operation is an async operation's state.
type Operation struct {
	ID     string `json:"id"`
	Status string `json:"status"` // Running, Success, Failure, Cancelled
	Err    string `json:"err"`
}

// Server is what /1.0 tells.
type Server struct {
	APIVersion  string `json:"api_version"`
	Environment struct {
		ServerVersion   string `json:"server_version"`
		ServerName      string `json:"server_name"`
		ServerClustered bool   `json:"server_clustered"`
		Driver          string `json:"driver"` // "lxc | qemu"
	} `json:"environment"`
}

// Drivers are the server's instance drivers.
func (s Server) Drivers() []string {
	var out []string
	for _, d := range strings.Split(s.Environment.Driver, "|") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func (c *Client) Server(ctx context.Context) (Server, error) {
	var s Server
	_, err := c.do(ctx, http.MethodGet, "/1.0", nil, &s)
	return s, err
}

// Instance is an LXD instance (container or virtual-machine).
type Instance struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`   // container, virtual-machine
	Status   string            `json:"status"` // Running, Stopped, ...
	Profiles []string          `json:"profiles"`
	Config   map[string]string `json:"config"`
	Location string            `json:"location,omitempty"`
}

// Running tells whether it runs.
func (i Instance) Running() bool { return i.Status == "Running" }

func (c *Client) Instances(ctx context.Context) ([]Instance, error) {
	var out []Instance
	_, err := c.do(ctx, http.MethodGet, "/1.0/instances?recursion=1", nil, &out)
	return out, err
}

func (c *Client) Instance(ctx context.Context, name string) (Instance, error) {
	var i Instance
	_, err := c.do(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(name), nil, &i)
	return i, err
}

// Source is where an instance's image comes from.
type Source struct {
	Type     string `json:"type"` // image
	Alias    string `json:"alias,omitempty"`
	Server   string `json:"server,omitempty"`
	Protocol string `json:"protocol,omitempty"` // simplestreams
}

// InstanceCreate is a new instance.
type InstanceCreate struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Profiles []string          `json:"profiles"`
	Config   map[string]string `json:"config"`
	Source   Source            `json:"source"`
	Start    bool              `json:"start,omitempty"`
}

// CreateInstance makes an instance and waits; target, when not empty,
// is the cluster member it goes on.
func (c *Client) CreateInstance(ctx context.Context, in InstanceCreate, target string) error {
	path := "/1.0/instances"
	if target != "" {
		path += "?target=" + url.QueryEscape(target)
	}
	r, err := c.do(ctx, http.MethodPost, path, in, nil)
	if err != nil {
		return err
	}
	return c.wait(ctx, r)
}

// SetState starts, stops or restarts an instance and waits.
func (c *Client) SetState(ctx context.Context, name, action string, force bool) error {
	r, err := c.do(ctx, http.MethodPut, "/1.0/instances/"+url.PathEscape(name)+"/state", map[string]any{"action": action, "timeout": 30, "force": force}, nil)
	if err != nil {
		return err
	}
	return c.wait(ctx, r)
}

// DeleteInstance deletes a stopped instance and waits.
func (c *Client) DeleteInstance(ctx context.Context, name string) error {
	r, err := c.do(ctx, http.MethodDelete, "/1.0/instances/"+url.PathEscape(name), nil, nil)
	if err != nil {
		return err
	}
	return c.wait(ctx, r)
}

// Snapshot snapshots an instance and waits.
func (c *Client) Snapshot(ctx context.Context, name, snapshot string) error {
	r, err := c.do(ctx, http.MethodPost, "/1.0/instances/"+url.PathEscape(name)+"/snapshots", map[string]any{"name": snapshot}, nil)
	if err != nil {
		return err
	}
	return c.wait(ctx, r)
}

// Restore restores an instance from a snapshot and waits.
func (c *Client) Restore(ctx context.Context, name, snapshot string) error {
	r, err := c.do(ctx, http.MethodPut, "/1.0/instances/"+url.PathEscape(name), map[string]any{"restore": snapshot}, nil)
	if err != nil {
		return err
	}
	return c.wait(ctx, r)
}

// Snapshots lists an instance's snapshot names.
func (c *Client) Snapshots(ctx context.Context, name string) ([]string, error) {
	var urls []string
	_, err := c.do(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(name)+"/snapshots", nil, &urls)
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		out = append(out, u[strings.LastIndex(u, "/")+1:])
	}
	return out, err
}

// Profile is an LXD profile.
type Profile struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Config      map[string]string            `json:"config"`
	Devices     map[string]map[string]string `json:"devices"`
}

func (c *Client) Profile(ctx context.Context, name string) (Profile, error) {
	var p Profile
	_, err := c.do(ctx, http.MethodGet, "/1.0/profiles/"+url.PathEscape(name), nil, &p)
	return p, err
}

// PutProfile makes the profile or replaces its config and devices.
func (c *Client) PutProfile(ctx context.Context, p Profile) error {
	_, err := c.Profile(ctx, p.Name)
	if IsNotFound(err) {
		_, err = c.do(ctx, http.MethodPost, "/1.0/profiles", p, nil)
		return err
	}
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPut, "/1.0/profiles/"+url.PathEscape(p.Name), map[string]any{"description": p.Description, "config": p.Config, "devices": p.Devices}, nil)
	return err
}

// StoragePool and Network are what facts report of them.
type StoragePool struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Status string `json:"status,omitempty"`
}

type Network struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Managed bool   `json:"managed"`
}

func (c *Client) StoragePools(ctx context.Context) ([]StoragePool, error) {
	var out []StoragePool
	_, err := c.do(ctx, http.MethodGet, "/1.0/storage-pools?recursion=1", nil, &out)
	return out, err
}

func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	var out []Network
	_, err := c.do(ctx, http.MethodGet, "/1.0/networks?recursion=1", nil, &out)
	return out, err
}
