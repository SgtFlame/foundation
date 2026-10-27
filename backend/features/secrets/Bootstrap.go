// Package secrets keeps the server's secrets in HashiCorp Vault
// (documentation/Secrets.md). With a Vault address, serve starts
// sealed: it serves only the bootstrap routes until an operator hands it a
// response-wrapped AppRole SecretID (Secrets question 1), then logs in, reads
// its bootstrap bundle (<mount>/server/*) and finishes starting.
package secrets

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
)

var log = logging.In("secrets")

// State is where the server is in starting up.
type State string

const (
	Sealed   State = "sealed"   // waiting for a token
	Starting State = "starting" // unsealed (or no Vault), opening databases and services
	Ready    State = "ready"
	Degraded State = "degraded" // running on cached secrets: the token could not be renewed, or Vault is unreachable
)

// VaultOptions say where the server's secrets are.
type VaultOptions struct {
	Addr      string // e.g. http://127.0.0.1:8200
	Mount     string // the KV v2 mount, "foundation" by default
	AuthMount string // the AppRole auth mount, "approle" by default
	Role      string // the AppRole, "foundation" by default
	RoleID    string // when set, the only RoleID an unseal may use
}

// ServerPrefix is the folder of the bootstrap bundle in the KV mount.
const ServerPrefix = "server/"

// Bootstrap is the sealed start. Without Vault it only reports starting,
// then ready.
type Bootstrap struct {
	opts   VaultOptions
	client *vault.Client // nil without Vault

	mu       sync.Mutex
	state    State
	expires  time.Time // the token's
	problem  string    // why the state is degraded
	bundle   map[string]string
	unsealed chan struct{}
	wake     chan struct{} // tells the renewer something failed
	renewing bool
}

// NewBootstrap starts sealed when opts names a Vault, and otherwise starting.
func NewBootstrap(opts VaultOptions) (*Bootstrap, error) {
	b := &Bootstrap{opts: opts, state: Starting, unsealed: make(chan struct{}), wake: make(chan struct{}, 1)}
	if opts.Addr == "" {
		close(b.unsealed)
		return b, nil
	}
	if b.opts.Mount == "" {
		b.opts.Mount = "foundation"
	}
	if b.opts.AuthMount == "" {
		b.opts.AuthMount = "approle"
	}
	if b.opts.Role == "" {
		b.opts.Role = "foundation"
	}
	client, err := vault.New(opts.Addr, "")
	if err != nil {
		return nil, err
	}
	b.client, b.state = client, Sealed
	return b, nil
}

// Vault tells whether secrets come from Vault.
func (b *Bootstrap) Vault() bool { return b.client != nil }

// KV is the server's KV mount; only with Vault, once unsealed.
func (b *Bootstrap) KV() vault.KV { return b.client.KV(b.opts.Mount) }

// Status is the bootstrap state as an administrator sees it.
type Status struct {
	State        State
	Vault        bool
	Addr, Mount  string
	TokenExpires time.Time // zero without Vault or while sealed
	Problem      string
}

func (b *Bootstrap) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Status{State: b.state, Vault: b.client != nil, TokenExpires: b.expires, Problem: b.problem}
	if b.client != nil {
		s.Addr, s.Mount = b.client.Addr(), b.opts.Mount
	}
	return s
}

// State is the bootstrap state alone, which anyone may see.
func (b *Bootstrap) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// UnsealError is a refused unseal, with the status to answer.
type UnsealError struct {
	Status int
	Err    error
}

func (e *UnsealError) Error() string { return e.Err.Error() }
func (e *UnsealError) Unwrap() error { return e.Err }

func refuse(status int, err error) error { return &UnsealError{status, err} }

// Unseal spends a response-wrapped SecretID: it checks that the token wraps
// a SecretID of the server's AppRole, unwraps it and logs in. The first
// unseal reads the bootstrap bundle and lets the server finish starting; a
// later one, while degraded, replaces the token.
func (b *Bootstrap) Unseal(ctx context.Context, wrapToken, roleID string) error {
	if b.client == nil {
		return refuse(http.StatusConflict, log.Code("vault_not_configured").New("the server does not use Vault"))
	}
	b.mu.Lock()
	state := b.state
	b.mu.Unlock()
	if state == Ready || state == Starting {
		return refuse(http.StatusConflict, log.Code("already_unsealed").Errorf("the server is %s", state))
	}
	switch {
	case b.opts.RoleID != "" && roleID != "" && roleID != b.opts.RoleID:
		return refuse(http.StatusBadRequest, log.Code("wrong_role").New("this is not the server's AppRole"))
	case b.opts.RoleID != "":
		roleID = b.opts.RoleID
	case roleID == "":
		return refuse(http.StatusBadRequest, log.Code("role_id_required").New("give the AppRole's role ID, or start serve with --vault-role-id"))
	case strings.TrimSpace(wrapToken) == "":
		return refuse(http.StatusBadRequest, log.Code("token_required").New("give a wrapping token"))
	}

	// Refuse a token that wraps anything but this role's SecretID before
	// spending it
	want := "auth/" + b.opts.AuthMount + "/role/" + b.opts.Role + "/secret-id"
	info, err := b.client.LookupWrapping(ctx, wrapToken)
	if err != nil {
		return refuse(http.StatusBadRequest, log.Code("bad_token").Wrapf(err, "the token is not a live wrapping token"))
	}
	if info.CreationPath != want {
		return refuse(http.StatusBadRequest, log.Code("bad_token").With("creationPath", info.CreationPath).Errorf("the token does not wrap a SecretID of the %s role", b.opts.Role))
	}
	secretID, err := b.client.UnwrapSecretID(ctx, wrapToken)
	if err != nil {
		return refuse(http.StatusBadRequest, log.Code("bad_token").Wrapf(err, "the token cannot be unwrapped"))
	}
	auth, err := b.client.LoginAppRole(ctx, b.opts.AuthMount, roleID, secretID)
	if err != nil {
		return refuse(http.StatusBadGateway, log.Code("vault_login_failed").Wrapf(err, "cannot log in to Vault"))
	}

	if state == Sealed {
		bundle, err := b.readBundle(ctx)
		if err != nil {
			return refuse(http.StatusBadGateway, log.Code("vault_read_failed").Wrapf(err, "cannot read the bootstrap bundle"))
		}
		b.mu.Lock()
		if b.state != Sealed { // another unseal won
			b.mu.Unlock()
			return refuse(http.StatusConflict, log.Code("already_unsealed").Errorf("the server is %s", b.state))
		}
		b.bundle, b.state, b.expires = bundle, Starting, time.Now().Add(auth.TTL)
		b.mu.Unlock()
		close(b.unsealed)
		log.Info(ctx, "unsealed: logged in to Vault at %s, %d bootstrap secrets", b.client.Addr(), len(bundle))
	} else {
		b.mu.Lock()
		b.state, b.problem, b.expires = Ready, "", time.Now().Add(auth.TTL)
		b.mu.Unlock()
		log.Info(ctx, "unsealed again: the server has a new Vault token")
	}
	b.startRenewing(auth)
	return nil
}

// readBundle reads every secret under server/ by its name there.
func (b *Bootstrap) readBundle(ctx context.Context) (map[string]string, error) {
	kv := b.KV()
	paths, err := kv.Walk(ctx, ServerPrefix)
	if err != nil {
		return nil, err
	}
	bundle := map[string]string{}
	for _, p := range paths {
		s, err := kv.Get(ctx, p)
		if errors.Is(err, vault.ErrNotFound) {
			continue // deleted
		}
		if err != nil {
			return nil, err
		}
		if v, ok := s.Data["value"].(string); ok {
			bundle[strings.TrimPrefix(p, ServerPrefix)] = v
		}
	}
	return bundle, nil
}

// Wait blocks until the server is unsealed (at once without Vault) and
// returns the bootstrap bundle by name, e.g. "db-key".
func (b *Bootstrap) Wait(ctx context.Context) (map[string]string, error) {
	select {
	case <-b.unsealed:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bundle, nil
}

// Started marks the end of startup.
func (b *Bootstrap) Started() {
	b.mu.Lock()
	if b.state == Starting {
		b.state = Ready
	}
	b.mu.Unlock()
}

// Lost reports a failed Vault request: the server keeps running on what it
// has cached, degraded, until the token renews again or an operator
// unseals with a new one (Secrets question 6).
func (b *Bootstrap) Lost(ctx context.Context, err error) {
	b.mu.Lock()
	changed := b.state == Ready
	if changed {
		b.state, b.problem = Degraded, err.Error()
	}
	b.mu.Unlock()
	if changed {
		log.Error(ctx, err, "Vault failed: running on cached secrets until it is reachable or the server is unsealed again")
		select {
		case b.wake <- struct{}{}:
		default:
		}
	}
}

// startRenewing renews the (periodic) token before its TTL runs out, every
// 30 seconds while degraded, until the process ends.
func (b *Bootstrap) startRenewing(auth vault.Auth) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.renewing || !auth.Renewable {
		return
	}
	b.renewing = true
	go func() {
		ctx := context.Background()
		wait := auth.TTL / 2
		for {
			select {
			case <-time.After(max(wait, 30*time.Second)):
			case <-b.wake:
			}
			renewed, err := b.client.RenewSelf(ctx)
			b.mu.Lock()
			switch {
			case err != nil:
				wait = 30 * time.Second
				if b.state == Ready {
					b.state = Degraded
					log.Error(ctx, err, "cannot renew the Vault token: running on cached secrets")
				}
				if b.state == Degraded {
					b.problem = "cannot renew the Vault token: " + err.Error()
				}
			default:
				wait = renewed.TTL / 2
				b.expires = time.Now().Add(renewed.TTL)
				if b.state == Degraded {
					b.state, b.problem = Ready, ""
					log.Info(ctx, "Vault is reachable again")
				}
			}
			b.mu.Unlock()
		}
	}()
}
