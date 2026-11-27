package natsd

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Accounts gives the JetStream a tenant's streams and buckets live in
// (forge 20a-3). Embedded and in development every tenant shares one
// account (Single); a deployment gives each tenant its own NATS account,
// reached by its own connection (PerTenant), so an account boundary and
// not only a name prefix keeps tenants apart.
type Accounts interface {
	For(ctx context.Context, tenant uint64) (jetstream.JetStream, error)
}

// Single is one JetStream for every tenant.
type Single struct{ JS jetstream.JetStream }

func (s Single) For(context.Context, uint64) (jetstream.JetStream, error) { return s.JS, nil }

// Creds mints the creds file of a user in a tenant's account; it fails
// for a tenant that has no account yet.
type Creds func(ctx context.Context, tenant uint64) ([]byte, error)

// PerTenant connects to each tenant's account on first use with creds
// from Creds, and keeps the connection.
type PerTenant struct {
	opts  Options
	creds Creds
	mu    sync.Mutex
	conns map[uint64]*tenantConn
}

type tenantConn struct {
	nc *nats.Conn
	js jetstream.JetStream
}

// NewPerTenant makes per-tenant accounts reached at the options' URL and
// CA (its Creds field is ignored: each tenant's come from creds).
func NewPerTenant(opts Options, creds Creds) *PerTenant {
	return &PerTenant{opts: opts, creds: creds, conns: map[uint64]*tenantConn{}}
}

func (p *PerTenant) For(ctx context.Context, tenant uint64) (jetstream.JetStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.conns[tenant]; c != nil {
		return c.js, nil
	}
	creds, err := p.creds(ctx, tenant)
	if err != nil {
		return nil, log.Wrapf(err, "the credentials of tenant %d's account", tenant)
	}
	o := p.opts
	o.Creds = ""
	opts := append(o.connectOptions("foundation-tenant"), nats.UserJWTAndSeed(splitCreds(creds)))
	nc, err := nats.Connect(o.URL, opts...)
	if err != nil {
		return nil, log.Wrapf(err, "connect to tenant %d's account", tenant)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, log.Wrap(err)
	}
	p.conns[tenant] = &tenantConn{nc: nc, js: js}
	return js, nil
}

// Close drains every tenant connection.
func (p *PerTenant) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.conns {
		if err := c.nc.Drain(); err != nil {
			c.nc.Close()
		}
		delete(p.conns, id)
	}
}

// splitCreds reads the user JWT and seed out of a creds file's text.
func splitCreds(creds []byte) (string, string) {
	jwt, _ := nkeysJWT(creds)
	seed, _ := nkeysSeed(creds)
	return jwt, seed
}

// For is the tenant's JetStream through acc, or js when acc is nil (one
// account for every tenant, as in development).
func For(ctx context.Context, acc Accounts, js jetstream.JetStream, tenant uint64) (jetstream.JetStream, error) {
	if acc == nil {
		return js, nil
	}
	return acc.For(ctx, tenant)
}
