package cmd

import (
	"os"

	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
)

// EnvNatsKeys names the keys directory when --nats-keys is not given.
const EnvNatsKeys = "FOUNDATION_NATS_KEYS"

// tenantAccounts is how serve reaches each tenant's NATS account (forge
// 20a-3): with a deployment's keys directory (`cli nats init`), every
// tenant's streams and buckets live in its own account T<id>, reached by a
// user serve mints from the keys; without one, every tenant shares the
// account serve connected to (nil). Until the credential issuer (forge
// 20h) the keys stay in a directory only serve's user reads.
func tenantAccounts(o natsd.Options, keysDir string) (*natsd.PerTenant, error) {
	if keysDir == "" {
		keysDir = os.Getenv(EnvNatsKeys)
	}
	if keysDir == "" {
		return nil, nil
	}
	keys := natsauth.DirKeys(keysDir)
	d, err := natsauth.Load(keys)
	if err != nil {
		return nil, err
	}
	return natsd.NewPerTenant(o, natsauth.Minter(d, keys, "serve")), nil
}
