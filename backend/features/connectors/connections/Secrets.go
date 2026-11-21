package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go/jetstream"
)

// Secrets never travel in a command's arguments past the bus's Seal: a
// value sent in connection.create or connection.update is put in the
// SecretStore under a new version and replaced by {"$ref": <version>},
// which is what is decided, committed and logged. The SecretStore's
// placeholder (sealed ciphertext, or a Vault version) is kept in KV
// cx_secrets_<tenant> at <connection>.<field>.<version>, so any process
// can resolve it. Older versions stay, so undoing an update restores the
// secret it replaced; deleting the connection purges them.

// refKey is the reference a sealed secret takes in arguments and events.
const refKey = "$ref"

func secretRef(tenant uint64, connection, field, version string) config.SecretRef {
	return config.SecretRef{Scope: "tenant/" + strconv.FormatUint(tenant, 10), Section: "connections/" + connection, Path: field + "/" + version}
}

func secretsKV(ctx context.Context, js jetstream.JetStream, tenant uint64) (jetstream.KeyValue, error) {
	return connectors.KV(ctx, js, jetstream.KeyValueConfig{Bucket: connectors.SecretsBucket(tenant), History: 1,
		Description: "Connection secrets' SecretStore placeholders: <connection>.<field>.<version>, never values (forge 07c)"})
}

// secretArg is one secret in arguments: a value to seal, a reference to
// keep (an undo's), "" to keep the current one, or nil to clear it.
type secretArg struct {
	value   string
	ref     string
	clear   bool
	present bool
}

func readSecretArgs(raw json.RawMessage) (map[string]secretArg, error) {
	out := map[string]secretArg{}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("secrets must be an object of field → value")
	}
	for k, v := range m {
		if !live.ValidToken(k) {
			return nil, fmt.Errorf("%q is not a secret field", k)
		}
		var s string
		var ref map[string]string
		switch {
		case string(v) == "null":
			out[k] = secretArg{clear: true, present: true}
		case json.Unmarshal(v, &s) == nil:
			if s != "" {
				out[k] = secretArg{value: s, present: true}
			}
		case json.Unmarshal(v, &ref) == nil && ref[refKey] != "":
			if !live.ValidToken(ref[refKey]) {
				return nil, fmt.Errorf("secrets.%s names no version", k)
			}
			out[k] = secretArg{ref: ref[refKey], present: true}
		default:
			return nil, fmt.Errorf("secrets.%s must be a string", k)
		}
	}
	return out, nil
}

// seal is the commands' Seal: secret values go to the SecretStore and the
// arguments keep their references. The caller's access is checked first,
// so a refused command stores nothing.
func seal(deps func() Deps, create bool) func(s live.Sealing) (json.RawMessage, *live.Problem) {
	return func(s live.Sealing) (json.RawMessage, *live.Problem) {
		var a map[string]json.RawMessage
		if err := json.Unmarshal(s.Args, &a); err != nil {
			return nil, &live.Problem{Path: "args", Message: err.Error()}
		}
		raw, ok := a["secrets"]
		if !ok {
			return s.Args, nil
		}
		secrets, err := readSecretArgs(raw)
		if err != nil {
			return nil, &live.Problem{Path: "secrets", Message: err.Error()}
		}
		d := deps()
		ctx, tenant := s.Context(), s.Principal.TenantID
		if create {
			owner := OwnerTenant
			var o struct{ Owner, Type string }
			_ = json.Unmarshal(s.Args, &o)
			if o.Owner != OwnerTenant {
				owner = OwnerOf(s.Principal)
			}
			group := ""
			if d.Registry != nil && owner == OwnerTenant {
				if t, ok, _ := d.Registry.Type(ctx, tenant, o.Type); ok {
					group = t.Type.Group
				}
			}
			if err := Check(s.ServiceArgs, Connection{ID: s.Target.ID, Owner: owner, Group: group}, "manage"); err != nil && owner == OwnerTenant {
				return nil, &live.Problem{Code: "forbidden", Message: err.Error()}
			}
		} else {
			c, ok, err := Get(ctx, d.Projections, tenant, s.Target.ID)
			if err != nil {
				return nil, &live.Problem{Message: err.Error()}
			}
			if !ok {
				return nil, &live.Problem{Code: "connection_not_found", Message: fmt.Sprintf("there is no connection %s", s.Target.ID)}
			}
			if err := CheckAccess(s.ServiceArgs, c, "manage"); err != nil {
				return nil, &live.Problem{Code: "forbidden", Message: err.Error()}
			}
		}
		out := map[string]any{}
		for field, sa := range secrets {
			switch {
			case sa.clear:
				out[field] = nil
			case sa.ref != "":
				out[field] = map[string]string{refKey: sa.ref}
			case s.Dry:
				out[field] = map[string]string{refKey: "dry"}
			default:
				version := strconv.FormatUint(features.NewTSID(), 10)
				if err := putSecret(ctx, d, tenant, s.Target.ID, field, version, sa.value); err != nil {
					log.With("connection", s.Target.ID).Error(ctx, err, "a connection secret could not be stored")
					return nil, &live.Problem{Code: "secret_not_stored", Path: "secrets." + field, Message: "the secret could not be stored; try again"}
				}
				out[field] = map[string]string{refKey: version}
			}
		}
		a["secrets"], _ = json.Marshal(out)
		sealed, _ := json.Marshal(a)
		return sealed, nil
	}
}

func putSecret(ctx context.Context, d Deps, tenant uint64, connection, field, version, value string) error {
	if d.Secrets == nil {
		return errors.New("there is no secret store")
	}
	stored, err := d.Secrets.Put(ctx, secretRef(tenant, connection, field, version), value)
	if err != nil {
		return err
	}
	kv, err := secretsKV(ctx, d.Projections.JetStream(), tenant)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(stored)
	_, err = kv.Put(ctx, connection+"."+field+"."+version, raw)
	return err
}

// hasSecret reports whether a version of a secret is stored.
func hasSecret(ctx context.Context, d Deps, tenant uint64, connection, field, version string) bool {
	if version == "dry" {
		return true
	}
	kv, err := secretsKV(ctx, d.Projections.JetStream(), tenant)
	if err != nil {
		return false
	}
	_, err = kv.Get(ctx, connection+"."+field+"."+version)
	return err == nil
}

// secretValue reads a secret's value.
func secretValue(ctx context.Context, d Deps, tenant uint64, connection, field, version string) (string, error) {
	kv, err := secretsKV(ctx, d.Projections.JetStream(), tenant)
	if err != nil {
		return "", err
	}
	e, err := kv.Get(ctx, connection+"."+field+"."+version)
	if err != nil {
		return "", err
	}
	var stored map[string]any
	if err := json.Unmarshal(e.Value(), &stored); err != nil {
		return "", err
	}
	return d.Secrets.Get(ctx, secretRef(tenant, connection, field, version), stored)
}

// purgeSecrets forgets every version of a deleted connection's secrets.
func purgeSecrets(ctx context.Context, d Deps, tenant uint64, connection string) error {
	kv, err := secretsKV(ctx, d.Projections.JetStream(), tenant)
	if err != nil {
		return err
	}
	keys, err := kv.ListKeysFiltered(ctx, connection+".>")
	if err != nil {
		return err
	}
	for k := range keys.Keys() {
		rest := k[len(connection)+1:]
		if i := lastDot(rest); i > 0 && d.Secrets != nil {
			field, version := rest[:i], rest[i+1:]
			if err := d.Secrets.Delete(ctx, secretRef(tenant, connection, field, version)); err != nil {
				return err
			}
		}
		if err := kv.Purge(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// Settings are a connection's settings with its secrets in place, for an
// adapter opening a session: never for a client, a log or a tool.
func Settings(ctx context.Context, d Deps, tenant uint64, c Connection) (json.RawMessage, error) {
	out := map[string]any{}
	for k, v := range c.Settings {
		out[k] = v
	}
	for field, version := range c.Secrets {
		v, err := secretValue(ctx, d, tenant, c.ID, field, version)
		if err != nil {
			return nil, log.Wrapf(err, "the secret %s of connection %s cannot be read", field, c.ID)
		}
		out[field] = v
	}
	if err := resolveRefs(ctx, d, tenant, c.Type, out); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
