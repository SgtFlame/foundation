package impl

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
	"github.com/SgtFlame/foundation/backend/features/secrets/vaultstore"
)

// maxValue is the longest secret accepted, in bytes.
const maxValue = 64 << 10

// Put writes a new version of a server secret, or sets an app-scope secret
// setting through configservice (so it is stored as every setting is, and
// applies at once).
func (s *service) Put(args secretsservice.PutArgs) secretsservice.PutResponse {
	if f := check(args.ServiceArgs, "secret.write"); f != nil {
		return secretsservice.PutResponse{Common: *f}
	}
	switch {
	case args.Value == "":
		return secretsservice.PutResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("empty_value").New("a secret cannot be empty; delete it instead"))}
	case len(args.Value) > maxValue:
		return secretsservice.PutResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("value_too_long").Errorf("a secret is at most %d KB", maxValue>>10))}
	}

	if strings.HasPrefix(args.Name, settingPrefix) {
		x, f := s.setSetting(args.ServiceArgs, args.Name, args.Value)
		if f != nil {
			return secretsservice.PutResponse{Common: *f}
		}
		return secretsservice.PutResponse{Common: features.OK(), Secret: x}
	}

	if f := serverOnly(args.ServiceArgs, args.Name); f != nil {
		return secretsservice.PutResponse{Common: *f}
	}
	kv, f := s.vault(args.ServiceArgs)
	if f != nil {
		return secretsservice.PutResponse{Common: *f}
	}
	version, err := kv.Put(args.Context(), args.Name, map[string]any{"value": args.Value})
	if err == nil {
		err = kv.SetCustomMetadata(args.Context(), args.Name, map[string]string{vaultstore.UpdatedBy: userRef(args.ServiceArgs)})
	}
	if err != nil {
		return secretsservice.PutResponse{Common: *s.vaultFailed(args.ServiceArgs, err)}
	}
	s.audit(args.ServiceArgs, args.Name, secrets.AuditPut, version)
	x := secretsservice.Secret{Name: args.Name, Kind: secretsservice.KindServer, Set: true}
	_ = s.describe(args.Context(), kv, &x)
	return secretsservice.PutResponse{Common: features.OK(), Secret: x}
}

// serverOnly refuses names that are neither a server secret nor
// changeable here.
func serverOnly(args features.ServiceArgs, name string) *features.ServiceResponse {
	switch {
	case !serverName.MatchString(name):
		return args.Fail(http.StatusBadRequest, log.Code("bad_name").Errorf("%q is not a secret name: use server/<name>, in lower case", name))
	case name == dbKey:
		return args.Fail(http.StatusConflict, log.Code("protected_secret").New("the database key is rotated with backend/scripts/secrets/rotate-db-key.sh"))
	}
	return nil
}

// setSetting sets (or, with value "", clears) an app-scope secret setting.
func (s *service) setSetting(args features.ServiceArgs, name, value string) (secretsservice.Secret, *features.ServiceResponse) {
	settings, f := s.settings(args)
	if f != nil {
		return secretsservice.Secret{}, f
	}
	i := slices.IndexFunc(settings, func(x setting) bool { return x.Name == name })
	if i < 0 {
		return secretsservice.Secret{}, args.Fail(http.StatusNotFound, log.Code("secret_not_found").Errorf("there is no secret setting %q", name))
	}
	x := settings[i]
	var leaf any = value
	if value == "" {
		leaf = nil
	}
	patch := map[string]any{}
	config.Set(patch, x.path, leaf)
	raw, _ := json.Marshal(patch)
	resp := s.config.Set(configservice.SetArgs{ServiceArgs: args, Section: x.section, Scope: configservice.ScopeApp, Patch: raw, Revision: x.revision})
	if resp.Common.Code != http.StatusOK {
		return secretsservice.Secret{}, &resp.Common
	}
	action := secrets.AuditPut
	if value == "" {
		action = secrets.AuditDelete
	}
	x.Set = value != ""
	if kv, f := s.vault(args); f == nil {
		_ = s.describe(args.Context(), kv, &x.Secret)
	}
	s.audit(args, name, action, x.Version)
	return x.Secret, nil
}

func userRef(args features.ServiceArgs) string {
	return "user:" + itoa(args.Principal.UserID)
}
