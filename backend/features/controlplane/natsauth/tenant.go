package natsauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
)

// TenantAccount is the name of a tenant's account (forge 20a-3): T<id>.
func TenantAccount(tenant uint64) string { return fmt.Sprintf("T%d", tenant) }

// TenantOf is the tenant a tenant account's name names, false for others.
func TenantOf(account string) (uint64, bool) {
	rest, ok := strings.CutPrefix(account, "T")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	return id, err == nil && id > 0
}

// AddTenant makes a tenant's account, with JetStream within the limits.
// Save it and Push it to a running server for it to take effect there.
func (d *Deployment) AddTenant(tenant uint64, limits Limits) (*Account, error) {
	if tenant == 0 {
		return nil, log.Code("bad_tenant").Errorf("tenant 0 has no account")
	}
	if a := d.Account(TenantAccount(tenant)); a != nil {
		return nil, log.Code("account_exists").Errorf("tenant %d already has an account", tenant)
	}
	return d.AddAccount(TenantAccount(tenant), limits)
}

// Minter mints, for serve, a user allowed everything in a tenant's
// account, without expiry; until the credential issuer (forge 20h) it is
// how a process holding the account signing keys reaches each tenant.
// A tenant whose account is not in the deployment yet is looked for again
// in keys, so an account added by `cli nats tenant add` while serve runs
// is found. The returned function has natsd.Creds's signature.
func Minter(d *Deployment, keys Keys, name string) func(ctx context.Context, tenant uint64) ([]byte, error) {
	var mu sync.Mutex
	return func(_ context.Context, tenant uint64) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		a := d.Account(TenantAccount(tenant))
		if a == nil && keys != nil {
			if again, err := Load(keys); err == nil {
				d = again
				a = d.Account(TenantAccount(tenant))
			}
		}
		if a == nil {
			return nil, log.Code("no_tenant_account").Errorf("tenant %d has no NATS account", tenant)
		}
		u, err := a.NewUser(fmt.Sprintf("%s-t%d", name, tenant), Everything, 0)
		if err != nil {
			return nil, err
		}
		return u.Creds, nil
	}
}

// EnvKeys names the keys directory when serve's --nats-keys is not given.
const EnvKeys = "FOUNDATION_NATS_KEYS"

// TenantAccounts is how serve reaches each tenant's account: with a
// deployment's keys directory (`cli nats init`; keysDir, else
// $FOUNDATION_NATS_KEYS) every tenant's streams and buckets live in its
// own account T<id>, reached at o's URL and CA by a user minted from the
// keys; without one it is nil and every tenant shares the account serve
// connected to. Until the credential issuer (forge 20h) the keys stay in
// a directory only serve's user reads.
func TenantAccounts(o natsd.Options, keysDir string) (*natsd.PerTenant, error) {
	if keysDir == "" {
		keysDir = os.Getenv(EnvKeys)
	}
	if keysDir == "" {
		return nil, nil
	}
	keys := DirKeys(keysDir)
	d, err := Load(keys)
	if err != nil {
		return nil, err
	}
	return natsd.NewPerTenant(o, Minter(d, keys, "serve")), nil
}

// AccountConn connects to one of the deployment's accounts (the control
// account, for serve's control plane followers) as a user minted from the
// keys in keysDir (else $FOUNDATION_NATS_KEYS); nil without keys, when the
// caller uses the connection it has.
func AccountConn(o natsd.Options, keysDir, account, name string) (*natsd.Server, error) {
	keysDir = KeysDir(keysDir)
	if keysDir == "" || o.URL == "" {
		return nil, nil
	}
	d, err := Load(DirKeys(keysDir))
	if err != nil {
		return nil, err
	}
	a := d.Account(account)
	if a == nil {
		return nil, log.Code("no_account").Errorf("the deployment has no account %s", account)
	}
	u, err := a.NewUser(name, Everything, 0)
	if err != nil {
		return nil, err
	}
	o.Dir, o.Creds, o.CredsData = "", "", u.Creds
	return natsd.Start(context.Background(), o)
}

// KeysDir is dir, else $FOUNDATION_NATS_KEYS.
func KeysDir(dir string) string {
	if dir == "" {
		return os.Getenv(EnvKeys)
	}
	return dir
}

// SysConn connects as the deployment's system user (<keys>/sys.creds),
// which pushes accounts.
func SysConn(o natsd.Options, keysDir, name string) (*nats.Conn, error) {
	o.Creds, o.CredsData = filepath.Join(KeysDir(keysDir), "sys.creds"), nil
	nc, err := nats.Connect(o.URL, natsd.ConnectOptions(o, name)...)
	return nc, log.Wrapf(err, "connect as the system user")
}

// ApplyTenant makes a tenant's account match its desired limits (forge
// 20e, the infraNatsAccount record): it reads the deployment from keys
// again, makes the account when the tenant has none, sets the limits,
// signs and saves it when they changed, and pushes it to every server
// through sys. The account's revocations are kept. A push is idempotent,
// so several processes applying one record converge; it reports whether
// the account changed.
func ApplyTenant(keys Keys, sys *nats.Conn, tenant uint64, l Limits) (bool, error) {
	d, err := Load(keys)
	if err != nil {
		return false, err
	}
	name := TenantAccount(tenant)
	a := d.Account(name)
	changed := false
	if a == nil {
		if a, err = d.AddTenant(tenant, l); err != nil {
			return false, err
		}
		changed = true
	} else {
		before, _ := json.Marshal(a.Claims.Limits)
		a.SetLimits(l)
		if after, _ := json.Marshal(a.Claims.Limits); string(after) != string(before) {
			if err := d.Operator.Sign(a); err != nil {
				return false, err
			}
			changed = true
		}
	}
	if changed {
		if err := d.SaveAccount(keys, name); err != nil {
			return false, err
		}
	}
	if sys != nil {
		if err := Push(sys, a); err != nil {
			return changed, err
		}
	}
	return changed, nil
}
