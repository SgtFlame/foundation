package natsauth

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
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
