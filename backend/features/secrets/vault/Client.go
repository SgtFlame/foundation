// Package vault is the small part of HashiCorp Vault's HTTP API Foundation
// uses (documentation/plans/Secrets.md): unwrapping a SecretID, AppRole
// login, renewing the token, and a KV v2 mount. It is plain net/http, so the
// server does not carry Vault's own SDK.
package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client talks to one Vault server with one token.
type Client struct {
	addr string
	http *http.Client

	mu    sync.RWMutex
	token string
}

// New makes a client for the Vault at addr, e.g. http://127.0.0.1:8200.
func New(addr, token string) (*Client, error) {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("vault: %q is not an http(s) URL", addr)
	}
	return &Client{addr: strings.TrimRight(addr, "/"), http: &http.Client{Timeout: 10 * time.Second}, token: token}, nil
}

// Addr is the Vault's address.
func (c *Client) Addr() string { return c.addr }

// SetToken replaces the token every later request uses.
func (c *Client) SetToken(token string) {
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

// ErrNotFound is a path with no secret, or a deleted one.
var ErrNotFound = errors.New("vault: not found")

// Error is a failed Vault request, with its status and Vault's messages.
type Error struct {
	Status int
	Errors []string
}

func (e *Error) Error() string {
	return fmt.Sprintf("vault: %d %s", e.Status, strings.Join(e.Errors, "; "))
}

// noToken sends a request with no token at all, as a login does.
const noToken = "-"

// do sends one request with token (the client's when empty) and decodes
// the response's JSON into out, when given.
func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.addr+"/v1/"+path, reader)
	if err != nil {
		return err
	}
	switch token {
	case "":
		c.mu.RLock()
		token = c.token
		c.mu.RUnlock()
	case noToken:
		token = ""
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		e := &Error{Status: resp.StatusCode}
		_ = json.Unmarshal(data, &struct{ Errors *[]string }{&e.Errors})
		return e
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Auth is a login's token and how long it lasts.
type Auth struct {
	Token     string
	TTL       time.Duration
	Renewable bool
}

type authResponse struct {
	Auth struct {
		ClientToken   string `json:"client_token"`
		LeaseDuration int    `json:"lease_duration"`
		Renewable     bool   `json:"renewable"`
	} `json:"auth"`
}

func (r authResponse) auth() Auth {
	return Auth{Token: r.Auth.ClientToken, TTL: time.Duration(r.Auth.LeaseDuration) * time.Second, Renewable: r.Auth.Renewable}
}

// WrapInfo describes a response-wrapping token without using it.
type WrapInfo struct {
	CreationPath string `json:"creation_path"`
	CreationTTL  int    `json:"creation_ttl"`
}

// LookupWrapping describes a wrapping token, so a token wrapping anything
// but the expected response can be refused before it is spent.
func (c *Client) LookupWrapping(ctx context.Context, wrapToken string) (WrapInfo, error) {
	var out struct{ Data WrapInfo }
	err := c.do(ctx, http.MethodPost, "sys/wrapping/lookup", noToken, map[string]string{"token": wrapToken}, &out)
	return out.Data, err
}

// UnwrapSecretID spends a single-use wrapping token and returns the AppRole
// SecretID it wrapped.
func (c *Client) UnwrapSecretID(ctx context.Context, wrapToken string) (string, error) {
	var out struct {
		Data struct {
			SecretID string `json:"secret_id"`
		}
	}
	if err := c.do(ctx, http.MethodPost, "sys/wrapping/unwrap", wrapToken, nil, &out); err != nil {
		return "", err
	}
	if out.Data.SecretID == "" {
		return "", errors.New("vault: the wrapped response holds no SecretID")
	}
	return out.Data.SecretID, nil
}

// LoginAppRole logs in with an AppRole's RoleID and SecretID, and uses the
// token from then on.
func (c *Client) LoginAppRole(ctx context.Context, mount, roleID, secretID string) (Auth, error) {
	var out authResponse
	err := c.do(ctx, http.MethodPost, "auth/"+mount+"/login", noToken, map[string]string{"role_id": roleID, "secret_id": secretID}, &out)
	if err != nil {
		return Auth{}, err
	}
	a := out.auth()
	c.SetToken(a.Token)
	return a, nil
}

// RenewSelf renews the client's token.
func (c *Client) RenewSelf(ctx context.Context) (Auth, error) {
	var out authResponse
	err := c.do(ctx, http.MethodPost, "auth/token/renew-self", "", map[string]any{}, &out)
	return out.auth(), err
}

// Secret is one version of a KV v2 secret: its fields and metadata.
type Secret struct {
	Data    map[string]any
	Version int
}

// Metadata is a KV v2 secret's metadata.
type Metadata struct {
	CurrentVersion int               `json:"current_version"`
	UpdatedTime    time.Time         `json:"updated_time"`
	CustomMetadata map[string]string `json:"custom_metadata"`
	Versions       map[string]struct {
		DeletionTime string `json:"deletion_time"`
		Destroyed    bool   `json:"destroyed"`
	} `json:"versions"`
}

// Deleted tells whether the current version is deleted or destroyed.
func (m Metadata) Deleted() bool {
	v := m.Versions[fmt.Sprint(m.CurrentVersion)]
	return v.DeletionTime != "" || v.Destroyed
}

// KV is a KV v2 mount.
type KV struct {
	c     *Client
	mount string
}

// KV returns the KV v2 mount at mount, e.g. "foundation".
func (c *Client) KV(mount string) KV { return KV{c, strings.Trim(mount, "/")} }

// Get reads a secret's current version; ErrNotFound when there is none or
// it is deleted.
func (kv KV) Get(ctx context.Context, path string) (Secret, error) {
	var out struct {
		Data struct {
			Data     map[string]any
			Metadata struct{ Version int }
		}
	}
	if err := kv.c.do(ctx, http.MethodGet, kv.mount+"/data/"+path, "", nil, &out); err != nil {
		return Secret{}, err
	}
	if out.Data.Data == nil {
		return Secret{}, ErrNotFound
	}
	return Secret{Data: out.Data.Data, Version: out.Data.Metadata.Version}, nil
}

// Put writes a new version of a secret and returns its number.
func (kv KV) Put(ctx context.Context, path string, data map[string]any) (int, error) {
	var out struct{ Data struct{ Version int } }
	err := kv.c.do(ctx, http.MethodPost, kv.mount+"/data/"+path, "", map[string]any{"data": data}, &out)
	return out.Data.Version, err
}

// Delete soft-deletes a secret's current version; earlier versions are kept.
func (kv KV) Delete(ctx context.Context, path string) error {
	err := kv.c.do(ctx, http.MethodDelete, kv.mount+"/data/"+path, "", nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// Metadata reads a secret's metadata.
func (kv KV) Metadata(ctx context.Context, path string) (Metadata, error) {
	var out struct{ Data Metadata }
	err := kv.c.do(ctx, http.MethodGet, kv.mount+"/metadata/"+path, "", nil, &out)
	return out.Data, err
}

// SetCustomMetadata replaces a secret's custom metadata, such as who wrote it.
func (kv KV) SetCustomMetadata(ctx context.Context, path string, custom map[string]string) error {
	return kv.c.do(ctx, http.MethodPost, kv.mount+"/metadata/"+path, "", map[string]any{"custom_metadata": custom}, nil)
}

// List returns the keys under prefix; folders end in "/". An empty folder
// lists nothing.
func (kv KV) List(ctx context.Context, prefix string) ([]string, error) {
	var out struct{ Data struct{ Keys []string } }
	err := kv.c.do(ctx, "LIST", kv.mount+"/metadata/"+strings.TrimSuffix(prefix, "/"), "", nil, &out)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return out.Data.Keys, err
}

// Walk lists every secret under prefix, recursively, as full paths.
func (kv KV) Walk(ctx context.Context, prefix string) ([]string, error) {
	keys, err := kv.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		p := strings.TrimSuffix(prefix, "/") + "/" + k
		if strings.HasSuffix(k, "/") {
			sub, err := kv.Walk(ctx, p)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
