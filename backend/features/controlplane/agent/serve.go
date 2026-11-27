package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// serve units (forge 20d-2): an infraUnit of kind serve runs as
// axiom-serve@<unit>, from one template unit. Before each start the
// oneshot axiom-serve-creds@<unit> runs `axiomd unit-credentials <unit>`,
// which asks this agent over its local socket; the agent asks the issuer
// with its own identity and writes the unit's credentials to a tmpfs
// directory that systemd's LoadCredential= reads and ExecStartPost removes.

// ServeTemplate and CredsTemplate are the template units.
const ServeTemplate = `[Unit]
Description=Axiom serve %i
Requires=axiom-serve-creds@%i.service
After=axiom-serve-creds@%i.service network-online.target
Wants=network-online.target

[Service]
LoadCredential=nats.creds:/run/axiomd/units/%i/nats.creds
LoadCredential=nats-urls:/run/axiomd/units/%i/nats-urls
LoadCredential=vault-role-id:/run/axiomd/units/%i/vault-role-id
LoadCredential=vault-wrapped-secret-id:/run/axiomd/units/%i/vault-wrapped-secret-id
ExecStart=/usr/bin/foundation serve --instance %i --instance-config /etc/axiom/units/%i.json
ExecStartPost=+/bin/rm -rf /run/axiomd/units/%i
DynamicUser=yes
StateDirectory=axiom/%i
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

const CredsTemplate = `[Unit]
Description=Credentials for Axiom serve %i
PartOf=axiom-serve@%i.service

[Service]
Type=oneshot
ExecStart=/usr/bin/axiomd unit-credentials %i
`

// SystemdName is a serve unit's systemd unit.
func SystemdName(unit string) string { return "axiom-serve@" + unit }

// RenewBefore is how long before its user expires a serve unit is
// restarted for new credentials (part question 1 of 20h).
var RenewBefore = 3 * 24 * time.Hour

// serveUnits turns each serve unit into its systemd unit with its
// instance config file, and adds the template units.
func serveUnits(units []Unit) []Unit {
	out := make([]Unit, 0, len(units)+1)
	some := false
	for _, u := range units {
		if u.Kind != "serve" {
			out = append(out, u)
			continue
		}
		some = true
		cfg, _ := json.MarshalIndent(map[string]any{"unit": u.Name, "serviceSet": serviceSet(u), "version": u.Version, "config": u.Config}, "", "  ")
		s := u
		s.Name = SystemdName(u.Name)
		s.Files = append([]File{{Path: "/etc/axiom/units/" + u.Name + ".json", Content: string(cfg), Mode: "0644"}}, u.Files...)
		out = append(out, s)
	}
	if some {
		out = append(out, Unit{Name: "", Kind: "template", Files: []File{
			{Path: "/etc/systemd/system/axiom-serve@.service", Content: ServeTemplate},
			{Path: "/etc/systemd/system/axiom-serve-creds@.service", Content: CredsTemplate},
		}})
	}
	return out
}

func serviceSet(u Unit) string {
	var c struct {
		ServiceSet string `json:"serviceSet"`
	}
	_ = json.Unmarshal(u.Config, &c)
	if u.ServiceSet != "" {
		return u.ServiceSet
	}
	return c.ServiceSet
}

func (a *Agent) runDir(unit string) string { return a.path(filepath.Join("/run/axiomd/units", unit)) }

type issued struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Set       string    `json:"set"`
}

func (a *Agent) issuedFile() string { return filepath.Join(a.State, "units.json") }

func (a *Agent) issuedUnits() map[string]issued {
	m := map[string]issued{}
	if raw, err := os.ReadFile(a.issuedFile()); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// Credentials asks the issuer for a unit's credentials, with a key made
// here, and writes them for systemd: nats.creds, nats-urls,
// vault-role-id and vault-wrapped-secret-id, each 0400 in a 0700
// directory. Every start asks again: a SecretID is used once.
func (a *Agent) Credentials(ctx context.Context, unit string) error {
	if a.Control == nil || a.Instance == "" {
		return log.Code("not_connected").Errorf("the agent is not connected")
	}
	c := a.Control()
	if c == nil {
		return log.Code("not_connected").Errorf("the agent is not connected")
	}
	kp, err := nkeys.CreateUser()
	if err != nil {
		return log.Wrap(err)
	}
	pub, _ := kp.PublicKey()
	rep, err := c.AskIssuer(ctx, a.Instance, ctl.UnitRequest{Unit: unit, PublicKey: pub})
	if err != nil {
		return err
	}
	seed, _ := kp.Seed()
	creds, err := jwt.FormatUserConfig(rep.JWT, seed)
	if err != nil {
		return log.Wrap(err)
	}
	dir := a.runDir(unit)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return log.Wrap(err)
	}
	for name, v := range map[string][]byte{
		"nats.creds": creds, "nats-urls": []byte(strings.Join(a.NatsURLs, ",")),
		"vault-role-id": []byte(rep.RoleID), "vault-wrapped-secret-id": []byte(rep.WrappedSecretID),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), v, 0o400); err != nil {
			return log.Wrap(err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.issuedUnits()
	m[unit] = issued{ExpiresAt: rep.ExpiresAt}
	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.MkdirAll(a.State, 0o700)
	return log.Wrap(os.WriteFile(a.issuedFile(), raw, 0o600))
}

// renew restarts serve units whose user expires soon, one per service set
// in a pass, so a set never loses every unit at once.
func (a *Agent) renew(ctx context.Context, units []Unit) error {
	a.mu.Lock()
	m := a.issuedUnits()
	a.mu.Unlock()
	sets := map[string]bool{}
	for _, u := range units {
		if u.Kind != "serve" || u.Status == "stopped" {
			continue
		}
		set := serviceSet(u)
		is, ok := m[u.Name]
		if !ok || sets[set] || time.Until(is.ExpiresAt) > RenewBefore {
			continue
		}
		sets[set] = true
		if _, err := a.run(ctx, "systemctl", "restart", SystemdName(u.Name)); err != nil {
			return err
		}
	}
	return nil
}

// forget removes what the agent kept for serve units no longer wanted;
// the issuer revokes their credentials when desired state drops them.
func (a *Agent) forget(units []Unit) {
	want := map[string]bool{}
	for _, u := range units {
		if u.Kind == "serve" {
			want[u.Name] = true
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.issuedUnits()
	changed := false
	for name := range m {
		if !want[name] {
			delete(m, name)
			_ = os.RemoveAll(a.runDir(name))
			changed = true
		}
	}
	if changed {
		raw, _ := json.MarshalIndent(m, "", "  ")
		_ = os.WriteFile(a.issuedFile(), raw, 0o600)
	}
}

// LocalSocket is where `axiomd unit-credentials` reaches the agent.
const LocalSocket = "/run/axiomd/agent.sock"

// ServeLocal answers `axiomd unit-credentials` on a Unix socket only
// root reaches: one line naming a unit, answered "ok" or "error: …".
func (a *Agent) ServeLocal(ctx context.Context, socket string) error {
	if socket == "" {
		socket = LocalSocket
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return log.Wrap(err)
	}
	_ = os.Remove(socket)
	l, err := net.Listen("unix", socket)
	if err != nil {
		return log.Wrap(err)
	}
	_ = os.Chmod(socket, 0o600)
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				line, err := bufio.NewReader(conn).ReadString('\n')
				unit := strings.TrimSpace(line)
				if err != nil || unit == "" || strings.ContainsAny(unit, "/.") {
					_, _ = conn.Write([]byte("error: bad request\n"))
					return
				}
				if err := a.Credentials(ctx, unit); err != nil {
					_, _ = conn.Write([]byte("error: " + err.Error() + "\n"))
					return
				}
				_, _ = conn.Write([]byte("ok\n"))
			}()
		}
	}()
	return nil
}

// AskLocal is `axiomd unit-credentials`: it asks the running agent.
func AskLocal(socket, unit string) error {
	if socket == "" {
		socket = LocalSocket
	}
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return log.Wrapf(err, "reach the agent")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := conn.Write([]byte(unit + "\n")); err != nil {
		return log.Wrap(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return log.Wrap(err)
	}
	if line = strings.TrimSpace(line); line != "ok" {
		return log.Code("no_credentials").Errorf("%s", strings.TrimPrefix(line, "error: "))
	}
	return nil
}
