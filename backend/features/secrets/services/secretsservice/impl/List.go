package impl

import (
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

// List describes the server's secrets in Vault and the app scope's secret
// settings, never their values.
func (s *service) List(args secretsservice.ListArgs) secretsservice.ListResponse {
	if f := check(args.ServiceArgs, "secret.list"); f != nil {
		return secretsservice.ListResponse{Common: *f}
	}
	var out []secretsservice.Secret
	kv, noVault := s.vault(args.ServiceArgs)
	if noVault == nil {
		names, err := kv.Walk(args.Context(), secrets.ServerPrefix)
		if err != nil {
			return secretsservice.ListResponse{Common: *s.vaultFailed(args.ServiceArgs, err)}
		}
		for _, name := range names {
			x := secretsservice.Secret{Name: name, Kind: secretsservice.KindServer, Protected: name == dbKey}
			if err := s.describe(args.Context(), kv, &x); err != nil {
				return secretsservice.ListResponse{Common: *s.vaultFailed(args.ServiceArgs, err)}
			}
			out = append(out, x)
		}
	}
	settings, f := s.settings(args.ServiceArgs)
	if f != nil {
		return secretsservice.ListResponse{Common: *f}
	}
	for _, x := range settings {
		if noVault == nil && x.Set {
			if err := s.describe(args.Context(), kv, &x.Secret); err != nil {
				return secretsservice.ListResponse{Common: *s.vaultFailed(args.ServiceArgs, err)}
			}
		}
		out = append(out, x.Secret)
	}
	out = slices.DeleteFunc(out, func(x secretsservice.Secret) bool { return !strings.HasPrefix(x.Name, args.Prefix) })
	return secretsservice.ListResponse{Common: features.OK(), Secrets: out}
}
