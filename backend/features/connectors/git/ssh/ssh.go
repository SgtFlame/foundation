// Package ssh is the git.ssh connector type (forge 24f): a git repository
// reached over SSH with a private key, sharing git.https' in-memory fetch,
// commit and watch code (https.NewWith). The key may be protected by a
// passphrase, which is never stored: a person unlocks it for some minutes
// (git.key@1 unlock, a Secret operation sent only by connection.secretCall),
// and the decrypted key is kept in this process's memory alone, in a
// Keyring, until it expires or is locked. While locked, every call fails
// with auth, detail locked, which does not mark the connection failed, and
// watching finds nothing until it is unlocked again. The host is checked
// against the connection's knownHosts lines: never trusted on first use.
package ssh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gossh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/https"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// TypeID is the type's id.
const TypeID = "git.ssh"

// DefaultMinutes and MaxMinutes bound how long an unlock lasts.
const (
	DefaultMinutes = 15
	MaxMinutes     = 60
)

// SettingsSchema is a connection's settings; the private key is a secret.
const SettingsSchema = `{"type":"object","properties":{
  "url":{"type":"string","title":"Repository URL","description":"The repository's SSH clone URL, such as git@github.com:acme/site.git or ssh://git@gitea.example.com:2222/acme/site.git"},
  "privateKey":{"type":"secret","title":"Private key","description":"The OpenSSH private key, as ssh-keygen writes it; protect it with a passphrase, which is asked when the key is unlocked and never stored"},
  "knownHosts":{"type":"string","title":"Host keys","description":"The host's public key lines, as ssh-keyscan prints them (host ssh-ed25519 AAAA…); the host must present one of them"},
  "pollSeconds":{"type":"integer","minimum":15,"title":"Poll every","description":"Seconds between looks for pushes while the key is unlocked; 60 when left out"},
  "allowDefaultBranch":{"type":"boolean","title":"Allow commits to the default branch","description":"Let the git tools and nodes commit straight to the default branch; otherwise they commit to another branch"}},"required":["url","privateKey","knownHosts"]}`

// Type is git.ssh.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{git.RepoRef, git.KeyRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Services: []string{"files"},
		Meta: connectors.Meta{Title: "Git over SSH", Description: "Any git repository reachable over SSH with a private key, checked against the host's known keys: reads branches, trees and files by fetching shallowly into memory, and commits by pushing to a branch. A key protected by a passphrase is unlocked by a person for some minutes (git.key unlock); while it is locked every call fails with auth. Notices pushes by polling while unlocked. Has no pull requests.",
			Tags: []string{"git", "code", "ssh"}},
	}
}

// Contribute registers the type (git.key@1 is git.Contribute's).
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, git.Source, nil, []connectors.Type{Type()})
}

// Settings are a connection's settings, resolved.
type Settings struct {
	URL                string `json:"url"`
	PrivateKey         string `json:"privateKey"`
	KnownHosts         string `json:"knownHosts"`
	PollSeconds        int    `json:"pollSeconds"`
	AllowDefaultBranch bool   `json:"allowDefaultBranch"`
}

// Options configure the implementation.
type Options struct {
	Files  git.Files
	Limits https.Limits
	// Poll overrides pollSeconds, for tests.
	Poll time.Duration
	// Keys hold the unlocked keys; the process's Keys when nil.
	Keys *Keyring
}

// Keys are this process's unlocked keys.
var Keys = NewKeyring()

// Impl is git.ssh's adapter implementation.
func Impl(o Options) adapter.Impl {
	if o.Keys == nil {
		o.Keys = Keys
	}
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if len(conn.Settings) > 0 {
			if err := json.Unmarshal(conn.Settings, &st); err != nil {
				return nil, connectors.Errorf(connectors.CodeInvalid, "the connection's settings: %v", err)
			}
		}
		return New(conn.Tenant, conn.ID, st, o)
	}}
}

// Keyring holds unlocked keys in memory, each until it expires, by
// tenant, connection and the encrypted key's hash (so a new key is locked
// at once). It is a cache of a person's unlock and holds nothing that
// another process needs: a process that has not been unlocked answers
// locked. Nothing in it is ever written anywhere.
type Keyring struct {
	mu   sync.Mutex
	keys map[string]unlocked
	now  func() time.Time
}

type unlocked struct {
	signer  ssh.Signer
	expires time.Time
}

// NewKeyring makes an empty keyring.
func NewKeyring() *Keyring { return &Keyring{keys: map[string]unlocked{}, now: time.Now} }

// SetClock replaces the keyring's clock, for tests.
func (k *Keyring) SetClock(now func() time.Time) { k.mu.Lock(); k.now = now; k.mu.Unlock() }

func (k *Keyring) put(id string, s ssh.Signer, until time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[id] = unlocked{signer: s, expires: until}
}

// get gives the unlocked key and when it expires, dropping expired keys.
func (k *Keyring) get(id string) (ssh.Signer, time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	for i, u := range k.keys {
		if !now.Before(u.expires) {
			delete(k.keys, i)
		}
	}
	u, ok := k.keys[id]
	if !ok {
		return nil, time.Time{}
	}
	return u.signer, u.expires
}

func (k *Keyring) drop(id string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.keys, id)
}

// Session is one connection: git.https' session signing in with the key.
type Session struct {
	*https.Session
	key       []byte // the private key as stored, encrypted when it has a passphrase
	encrypted bool
	plain     ssh.Signer // a key without a passphrase
	user      string
	hosts     ssh.HostKeyCallback
	id        string
	keys      *Keyring
}

var scpLike = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9.-]+):([^/].*)$`)

// userOf checks a URL and gives the user to sign in as.
func userOf(u string) (string, error) {
	if m := scpLike.FindStringSubmatch(u); m != nil && !strings.Contains(u, "://") {
		return m[1], nil
	}
	if rest, ok := strings.CutPrefix(u, "ssh://"); ok {
		host, _, _ := strings.Cut(rest, "/")
		if user, h, ok := strings.Cut(host, "@"); ok && h != "" {
			return user, nil
		}
		if host != "" {
			return "git", nil
		}
	}
	return "", connectors.Errorf(connectors.CodeInvalid, "the repository URL must be ssh://user@host/path or user@host:path")
}

// New makes a session from settings.
func New(tenant uint64, connection string, st Settings, o Options) (*Session, error) {
	user, err := userOf(st.URL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(st.PrivateKey) == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the connection has no private key")
	}
	hosts, err := HostKeys(st.KnownHosts)
	if err != nil {
		return nil, err
	}
	if o.Keys == nil {
		o.Keys = Keys
	}
	sum := sha256.Sum256([]byte(st.PrivateKey))
	s := &Session{key: []byte(st.PrivateKey), user: user, hosts: hosts, keys: o.Keys,
		id: strconv.FormatUint(tenant, 10) + "/" + connection + "/" + hex.EncodeToString(sum[:])}
	signer, err := ssh.ParsePrivateKey(s.key)
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &missing):
		s.encrypted = true
	case err != nil:
		return nil, connectors.Errorf(connectors.CodeInvalid, "the private key cannot be read: paste it as ssh-keygen writes it")
	default:
		s.plain = signer
	}
	s.Session = https.NewWith(https.Settings{URL: st.URL, PollSeconds: st.PollSeconds, AllowDefaultBranch: st.AllowDefaultBranch},
		https.Transport{TypeID: TypeID, Auth: s.auth, CanWrite: true}, https.Options{Files: o.Files, Limits: o.Limits, Poll: o.Poll})
	return s, nil
}

// LockedMessage is what a call on a locked connection says.
const LockedMessage = "the connection's key is locked: unlock it first with its passphrase"

func (s *Session) signer() (ssh.Signer, time.Time) {
	if s.plain != nil {
		return s.plain, time.Time{}
	}
	return s.keys.get(s.id)
}

func (s *Session) auth() (transport.AuthMethod, error) {
	signer, _ := s.signer()
	if signer == nil {
		return nil, connectors.Locked(LockedMessage)
	}
	return &gossh.PublicKeys{User: s.user, Signer: signer, HostKeyCallbackHelper: gossh.HostKeyCallbackHelper{HostKeyCallback: s.hosts}}, nil
}

func (s *Session) status() map[string]any {
	signer, until := s.signer()
	out := map[string]any{"unlocked": signer != nil, "encrypted": s.encrypted}
	if signer != nil && !until.IsZero() {
		out["expiresAt"] = until.UTC().Format(time.RFC3339)
	}
	return out
}

// Call runs one operation: git.key@1's here, git.repo@1's in the shared
// session.
func (s *Session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	switch op {
	case "unlock":
		var a struct {
			Passphrase string `json:"passphrase"`
			Minutes    int    `json:"minutes"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "unlock takes a passphrase")
		}
		if !s.encrypted {
			return git.Result(s.status())
		}
		minutes := a.Minutes
		if minutes <= 0 {
			minutes = DefaultMinutes
		}
		minutes = min(minutes, MaxMinutes)
		pass := []byte(a.Passphrase)
		a.Passphrase = ""
		signer, err := ssh.ParsePrivateKeyWithPassphrase(s.key, pass)
		clear(pass)
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "the passphrase does not open the key")
		}
		s.keys.put(s.id, signer, s.keys.now().Add(time.Duration(minutes)*time.Minute))
		return git.Result(s.status())
	case "lock":
		s.keys.drop(s.id)
		return git.Result(s.status())
	case "keyStatus":
		return git.Result(s.status())
	}
	return s.Session.Call(ctx, op, args)
}

// HostKeys checks a host against known_hosts lines: "host[,host…] type
// key" (a host may be [host]:port, or a pattern with * and ?), or a bare
// "type key" line, trusted for whatever host the URL names. Hashed host
// names and markers (@revoked, @cert-authority) are refused.
func HostKeys(lines string) (ssh.HostKeyCallback, error) {
	type known struct {
		hosts []string
		key   []byte
	}
	var ks []known
	for i, line := range strings.Split(lines, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, _, opts, _, err := ssh.ParseAuthorizedKey([]byte(line)); err == nil && len(opts) == 0 {
			ks = append(ks, known{key: k.Marshal()})
			continue
		}
		marker, hosts, k, _, _, err := ssh.ParseKnownHosts([]byte(line))
		if err != nil || marker != "" {
			return nil, connectors.Errorf(connectors.CodeInvalid, "host key line %d cannot be read: use the lines ssh-keyscan prints", i+1)
		}
		for _, h := range hosts {
			if strings.HasPrefix(h, "|") {
				return nil, connectors.Errorf(connectors.CodeInvalid, "host key line %d has a hashed host name: use ssh-keyscan without -H", i+1)
			}
		}
		ks = append(ks, known{hosts: hosts, key: k.Marshal()})
	}
	if len(ks) == 0 {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the connection has no host keys: add the host's lines from ssh-keyscan")
	}
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		addr := knownhosts.Normalize(hostname)
		got := key.Marshal()
		for _, k := range ks {
			if string(k.key) != string(got) {
				continue
			}
			if len(k.hosts) == 0 {
				return nil
			}
			for _, h := range k.hosts {
				if ok, _ := path.Match(knownhosts.Normalize(h), addr); ok || knownhosts.Normalize(h) == addr {
					return nil
				}
			}
		}
		return connectors.Errorf(connectors.CodeAuth, "the host %s presented a key the connection does not know: check its host keys", addr)
	}, nil
}
