package impl

import (
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

// Delete soft-deletes a server secret's current version (Vault keeps the
// earlier ones), or clears an app-scope secret setting.
func (s *service) Delete(args secretsservice.DeleteArgs) secretsservice.DeleteResponse {
	if f := check(args.ServiceArgs, "secret.write"); f != nil {
		return secretsservice.DeleteResponse{Common: *f}
	}
	if strings.HasPrefix(args.Name, settingPrefix) {
		if _, f := s.setSetting(args.ServiceArgs, args.Name, ""); f != nil {
			return secretsservice.DeleteResponse{Common: *f}
		}
		return secretsservice.DeleteResponse{Common: features.OK()}
	}
	if f := serverOnly(args.ServiceArgs, args.Name); f != nil {
		return secretsservice.DeleteResponse{Common: *f}
	}
	kv, f := s.vault(args.ServiceArgs)
	if f != nil {
		return secretsservice.DeleteResponse{Common: *f}
	}
	if err := kv.Delete(args.Context(), args.Name); err != nil {
		return secretsservice.DeleteResponse{Common: *s.vaultFailed(args.ServiceArgs, err)}
	}
	s.audit(args.ServiceArgs, args.Name, secrets.AuditDelete, 0)
	return secretsservice.DeleteResponse{Common: features.OK()}
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }
