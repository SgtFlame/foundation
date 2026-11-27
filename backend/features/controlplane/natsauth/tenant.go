package natsauth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
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
