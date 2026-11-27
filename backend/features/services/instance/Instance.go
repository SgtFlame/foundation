// Package instance is what serve --instance reads (forge 21g-2, 21h): the
// unit's config file the instance agent renders from desired state, the
// credentials systemd hands the unit, and the readiness it reports.
package instance

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Config is /etc/axiom/units/<unit>.json, as the agent renders it from
// the unit's desired state (controlplane/agent's serveUnits).
type Config struct {
	Unit       string `json:"unit"`
	ServiceSet string `json:"serviceSet"`
	Version    string `json:"version"`
	// Flags are serve's flags by name ("log-format": "json"), so desired
	// state is the whole truth; a flag given on the command line wins.
	Flags map[string]string `json:"-"`
}

// Read reads a unit's config. Its config object holds flags: strings,
// numbers and booleans by flag name; a list is joined with commas.
func Read(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("the instance config cannot be read: %v", err)
	}
	var f struct {
		Config
		Raw map[string]any `json:"config"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return Config{}, fmt.Errorf("the instance config %s is not JSON: %v", path, err)
	}
	c := f.Config
	if c.ServiceSet == "" {
		if s, ok := f.Raw["serviceSet"].(string); ok {
			c.ServiceSet = s
		}
	}
	c.Flags = map[string]string{}
	for k, v := range f.Raw {
		if k == "serviceSet" {
			continue // the unit's own, read above when the agent put it at the top
		}
		switch x := v.(type) {
		case nil:
		case string:
			c.Flags[k] = x
		case bool, float64:
			c.Flags[k] = fmt.Sprint(x)
		case []any:
			var parts []string
			for _, p := range x {
				parts = append(parts, fmt.Sprint(p))
			}
			c.Flags[k] = strings.Join(parts, ",")
		default:
			return Config{}, fmt.Errorf("the instance config's %q is neither text, a number, a boolean nor a list", k)
		}
	}
	return c, nil
}

// Names are the config's flags in order, for applying them.
func (c Config) Names() []string {
	var out []string
	for k := range c.Flags {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Credentials are what systemd's LoadCredential= hands the unit in
// $CREDENTIALS_DIRECTORY (controlplane/agent's ServeTemplate).
type Credentials struct {
	// NatsCreds is the path of the unit's NATS user (nats.creds).
	NatsCreds string
	// NatsURLs are the NATS servers, comma-separated (nats-urls).
	NatsURLs string
	// VaultRoleID and VaultWrappedSecretID log the unit in to Vault once
	// (vault-role-id, vault-wrapped-secret-id): the SecretID is
	// response-wrapped and single-use, so each start has a new one.
	VaultRoleID          string
	VaultWrappedSecretID string
}

// ReadCredentials reads the credentials in dir ($CREDENTIALS_DIRECTORY);
// a missing file leaves its field empty.
func ReadCredentials(dir string) Credentials {
	if dir == "" {
		return Credentials{}
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	c := Credentials{NatsURLs: read("nats-urls"), VaultRoleID: read("vault-role-id"), VaultWrappedSecretID: read("vault-wrapped-secret-id")}
	if p := filepath.Join(dir, "nats.creds"); fileExists(p) {
		c.NatsCreds = p
	}
	return c
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Notify tells systemd the unit's state ("READY=1", "STOPPING=1",
// "STATUS=…") on $NOTIFY_SOCKET (Type=notify); without the socket it
// does nothing.
func Notify(state string) error {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return nil
	}
	if strings.HasPrefix(sock, "@") {
		sock = "\x00" + sock[1:] // an abstract socket
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}
