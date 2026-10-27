package impl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/SgtFlame/foundation/backend/features/secrets/vaultstore"
	"gorm.io/gorm"
)

var log = logging.In("secrets")

type service struct {
	db       *gorm.DB
	boot     *secrets.Bootstrap
	config   configservice.Service
	userName func(id uint64) string
}

// check is every operation's: secret.list or secret.write on the app.
func check(args features.ServiceArgs, action string) *features.ServiceResponse {
	if err := authz.Check(args.Context(), args.Principal, action, "app").Err(action, "app"); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// dbKey is the bootstrap bundle's database key, rotated only by script.
const dbKey = secrets.ServerPrefix + "db-key"

// settingPrefix is the folder of the app scope's secret settings.
const settingPrefix = "config/app/"

var serverName = regexp.MustCompile(`^server(/[a-z0-9][a-z0-9._-]{0,63}){1,4}$`)

// vault is the server's KV mount, or a 409 while there is none.
func (s *service) vault(args features.ServiceArgs) (vault.KV, *features.ServiceResponse) {
	if s.boot == nil || !s.boot.Vault() {
		return vault.KV{}, args.Fail(http.StatusConflict, log.Code("vault_not_configured").
			Public("The server does not use Vault, so it has no server secrets").New("the server does not use Vault"))
	}
	if st := s.boot.State(); st == secrets.Sealed {
		return vault.KV{}, args.Fail(http.StatusServiceUnavailable, log.Code("server_sealed").New("the server is sealed"))
	}
	return s.boot.KV(), nil
}

// vaultFailed fails a Vault request with 502, telling the bootstrap.
func (s *service) vaultFailed(args features.ServiceArgs, err error) *features.ServiceResponse {
	s.boot.Lost(args.Context(), err)
	return args.Fail(http.StatusBadGateway, log.Code("vault_failed").Public("Vault did not answer").Wrapf(err, "vault"))
}

// setting is one app-scope secret setting.
type setting struct {
	secretsservice.Secret
	section, path string
	revision      int64
}

// settings lists every secret setting the app scope can hold, with whether
// it is set there.
func (s *service) settings(args features.ServiceArgs) ([]setting, *features.ServiceResponse) {
	sections := s.config.Sections(configservice.SectionsArgs{ServiceArgs: args})
	if sections.Common.Code != http.StatusOK {
		return nil, &sections.Common
	}
	var out []setting
	for _, sec := range sections.Sections {
		schema, err := config.ParseSchema(sec.Schema)
		if err != nil {
			continue
		}
		var paths []string
		schema.Leaves(func(path string, leaf *config.Schema) {
			if leaf.Type == "secret" && schema.AllowsScope(path, sec.Scopes, string(configservice.ScopeApp)) {
				paths = append(paths, path)
			}
		})
		if len(paths) == 0 {
			continue
		}
		view := s.config.Get(configservice.GetArgs{ServiceArgs: args, Section: sec.ID, Scope: configservice.ScopeApp})
		if view.Common.Code != http.StatusOK {
			return nil, &view.Common
		}
		for _, path := range paths {
			shown, _ := config.Get(view.Value, path)
			redacted, _ := shown.(map[string]any)
			source := view.Sources[path]
			x := setting{section: sec.ID, path: path, revision: view.Revision}
			x.Name = settingPrefix + sec.ID + "/" + path
			x.Kind = secretsservice.KindSetting
			x.Title = title(sec.Title, schema, path)
			x.Set = redacted["set"] == true && source == string(configservice.ScopeApp)
			if source == configservice.SourceEnv || source == configservice.SourceServer {
				x.Source = source
			}
			out = append(out, x)
		}
	}
	return out, nil
}

// title is a setting's section title and the titles down to its field,
// e.g. "Providers › Google Gemini › API key".
func title(section string, schema *config.Schema, path string) string {
	parts := []string{section}
	segments := strings.Split(path, ".")
	for i := range segments {
		t := segments[i]
		if at := schema.At(strings.Join(segments[:i+1], ".")); at != nil && at.Title != "" {
			t = at.Title
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " › ")
}

// describe fills a secret's version and who changed it from Vault.
func (s *service) describe(ctx context.Context, kv vault.KV, secret *secretsservice.Secret) error {
	meta, err := kv.Metadata(ctx, secret.Name)
	if errors.Is(err, vault.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	secret.Version, secret.UpdatedAt = meta.CurrentVersion, meta.UpdatedTime
	secret.UpdatedBy = s.who(meta.CustomMetadata[vaultstore.UpdatedBy])
	if secret.Kind == secretsservice.KindServer {
		secret.Set = !meta.Deleted()
	}
	return nil
}

// who names the writer recorded in a secret's metadata.
func (s *service) who(by string) string {
	kind, id, _ := strings.Cut(by, ":")
	switch kind {
	case "user":
		n, _ := strconv.ParseUint(id, 10, 64)
		if name := s.userName(n); name != "" {
			return name
		}
		return "user " + id
	case "operator":
		return fmt.Sprintf("%s (operator)", id)
	}
	return by
}

// audit records a change, never its value.
func (s *service) audit(args features.ServiceArgs, name, action string, version int) {
	if s.db == nil {
		return
	}
	a := secrets.SecretAudit{Name: name, Action: action, Version: version, UserID: args.Principal.UserID}
	if err := s.db.WithContext(args.Context()).Create(&a).Error; err != nil {
		log.Error(args.Context(), err, "cannot audit a change to %s", name)
	}
	log.With("secret", name, "action", action, "version", version).Info(args.Context(), "secret %s: %s", name, action)
}
