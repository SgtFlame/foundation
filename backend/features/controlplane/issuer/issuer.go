// Package issuer is the credential issuer (forge 20h): for a serve unit
// that desired state places on an instance, it signs a NATS user for a
// key the instance's agent made, scoped by the unit's service set, and
// asks Vault for a response-wrapped, single-use SecretID of the set's
// AppRole; it revokes both when the unit is removed. Every process of
// the root tenant's serve runs it, with no leader: each request stands
// alone and every write is by expected revision or idempotent.
package issuer

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/secrets/vault"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("controlplane").Tags("issuer")

const (
	// IssuedBucket keeps every issue: <instance>.<unit>.<n>.
	IssuedBucket = "ctl_issued"
	// RevokeSubject revokes a unit's credentials at once.
	RevokeSubject = "ctl.issuer.revoke"
	queue         = "ctl.issuer"
)

// Set is what a service set's units may do: their NATS permissions in
// the services account and their Vault policy. 21f computes them from
// the services' manifests and 22g the policies; until then this table.
type Set struct {
	Permissions natsauth.Permissions
	Policy      string
}

// Sets are the service sets units may name ("" is all).
var Sets = map[string]Set{
	"all":    {Permissions: natsauth.Everything, Policy: policy},
	"web":    {Permissions: natsauth.Everything, Policy: policy},
	"runner": {Permissions: natsauth.Everything, Policy: policy},
}

const policy = `path "foundation/data/server/*" { capabilities = ["read"] }
path "foundation/data/config/*" { capabilities = ["read", "create", "update"] }
path "foundation/metadata/config/*" { capabilities = ["read", "list"] }
`

// RevokeRequest names a unit whose credentials are revoked now.
type RevokeRequest struct {
	Instance string `json:"instance"`
	Unit     string `json:"unit"`
}

// Issued is one issue, kept so it can be revoked.
type Issued struct {
	Instance  string     `json:"instance"`
	Unit      string     `json:"unit"`
	Set       string     `json:"set"`
	Account   string     `json:"account"`
	User      string     `json:"user"` // the user's public key
	Role      string     `json:"role"`
	Accessor  string     `json:"accessor"` // the SecretID's
	At        time.Time  `json:"at"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

// Issuer issues and revokes.
type Issuer struct {
	C *ctl.Control
	// D holds the services account's signing key (users) and the
	// operator's signing key (revocations); Sys is a system account
	// connection that pushes the account.
	D   *natsauth.Deployment
	Sys *nats.Conn
	// Vault is a client whose token may manage unit-* AppRoles; Mount
	// is the AppRole mount ("approle").
	Vault *vault.Client
	Mount string
	// UserTTL (30 days) and WrapTTL (60 s).
	UserTTL, WrapTTL time.Duration

	mu sync.Mutex // the account claims are edited in memory
}

func (i *Issuer) defaults() {
	if i.Mount == "" {
		i.Mount = "approle"
	}
	if i.UserTTL == 0 {
		i.UserTTL = 30 * 24 * time.Hour
	}
	if i.WrapTTL == 0 {
		i.WrapTTL = time.Minute
	}
}

func (i *Issuer) kv(ctx context.Context) (jetstream.KeyValue, error) {
	kv, err := i.C.JS.CreateOrUpdateKeyValue(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: IssuedBucket, Description: "Credentials issued to units, for revocation", History: 1, Storage: jetstream.FileStorage}))
	return kv, log.Wrap(err)
}

// Serve answers agents and revocations in a queue group shared by every
// process, and revokes what desired state no longer places, on every
// change of it.
func (i *Issuer) Serve(ctx context.Context) error {
	i.defaults()
	if _, err := i.kv(ctx); err != nil {
		return err
	}
	unit, err := i.C.NC.QueueSubscribe(ctl.IssuerSubject("*"), queue, func(m *nats.Msg) {
		raw, _ := json.Marshal(i.unit(ctx, strings.TrimPrefix(m.Subject, ctl.IssuerSubject("")), m.Data))
		_ = m.Respond(raw)
	})
	if err != nil {
		return log.Wrap(err)
	}
	revoke, err := i.C.NC.QueueSubscribe(RevokeSubject, queue, func(m *nats.Msg) {
		var r RevokeRequest
		reply := map[string]any{}
		if err := json.Unmarshal(m.Data, &r); err != nil || r.Instance == "" || r.Unit == "" {
			reply["error"] = "bad request"
		} else if n, err := i.Revoke(ctx, r.Instance, r.Unit); err != nil {
			reply["error"] = err.Error()
		} else {
			reply["revoked"] = n
		}
		raw, _ := json.Marshal(reply)
		_ = m.Respond(raw)
	})
	if err != nil {
		_ = unit.Unsubscribe()
		return log.Wrap(err)
	}
	go func() {
		<-ctx.Done()
		_ = unit.Unsubscribe()
		_ = revoke.Unsubscribe()
	}()
	go i.watch(ctx)
	return log.Wrap(i.C.NC.Flush())
}

// placed is the unit's service set when desired state places a serve unit
// of that name on the instance.
func (i *Issuer) placed(ctx context.Context, instance, unit string) (string, bool, error) {
	raw, ok, err := i.C.Desired(ctx, ctl.Target{Kind: ctl.KindInstance, ID: instance})
	if err != nil || !ok {
		return "", false, err
	}
	var doc struct {
		Units []struct {
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			ServiceSet string `json:"serviceSet"`
		} `json:"units"`
	}
	_ = json.Unmarshal(raw, &doc)
	for _, u := range doc.Units {
		if u.Name == unit && u.Kind == "serve" {
			set := u.ServiceSet
			if set == "" {
				set = "all"
			}
			return set, true, nil
		}
	}
	return "", false, nil
}

func (i *Issuer) unit(ctx context.Context, instance string, data []byte) ctl.UnitReply {
	var r ctl.UnitRequest
	if err := json.Unmarshal(data, &r); err != nil || r.Unit == "" || r.PublicKey == "" || instance == "" {
		return ctl.UnitReply{Error: "bad request"}
	}
	audit := log.With("instance", instance).With("unit", r.Unit)
	set, ok, err := i.placed(ctx, instance, r.Unit)
	if err != nil {
		return ctl.UnitReply{Error: "try again"}
	}
	s, known := Sets[set]
	if !ok || !known {
		audit.Warn(ctx, "issuer: refused credentials for unit %s on instance %s: not placed there", r.Unit, instance)
		return ctl.UnitReply{Error: "no serve unit " + r.Unit + " is placed on this instance"}
	}
	acc := i.D.Account(natsauth.App)
	if acc == nil {
		return ctl.UnitReply{Error: "no services account"}
	}
	name := "unit-" + instance + "-" + r.Unit
	token, err := acc.UserJWT(r.PublicKey, name, s.Permissions, i.UserTTL)
	if err != nil {
		return ctl.UnitReply{Error: "bad public key"}
	}
	role := "unit-" + set
	roleID, wrapped, err := i.secretID(ctx, role, s.Policy, instance, r.Unit)
	if err != nil {
		audit.Error(ctx, err, "issuer: cannot get a SecretID")
		return ctl.UnitReply{Error: "Vault is unavailable"}
	}
	rec := Issued{Instance: instance, Unit: r.Unit, Set: set, Account: natsauth.App, User: r.PublicKey, Role: role, Accessor: wrapped.WrappedAccessor, At: time.Now().UTC()}
	kv, err := i.kv(ctx)
	if err == nil {
		raw, _ := json.Marshal(rec)
		_, err = kv.Create(ctx, key(instance, r.Unit, strconv.FormatUint(features.NewTSID(), 10)), raw)
	}
	if err != nil {
		_ = i.Vault.DestroySecretID(ctx, i.Mount, role, wrapped.WrappedAccessor)
		return ctl.UnitReply{Error: "try again"}
	}
	audit.Info(ctx, "issuer: issued credentials for unit %s on instance %s (set %s)", r.Unit, instance, set)
	return ctl.UnitReply{Account: natsauth.App, JWT: token, RoleID: roleID, WrappedSecretID: wrapped.Token, ExpiresAt: rec.At.Add(i.UserTTL)}
}

// secretID makes the set's policy and AppRole (idempotent) and a wrapped
// SecretID of it.
func (i *Issuer) secretID(ctx context.Context, role, pol, instance, unit string) (string, vault.Wrapped, error) {
	if err := i.Vault.PutPolicy(ctx, role, pol); err != nil {
		return "", vault.Wrapped{}, err
	}
	if err := i.Vault.PutAppRole(ctx, i.Mount, role, []string{role}, time.Hour); err != nil {
		return "", vault.Wrapped{}, err
	}
	roleID, err := i.Vault.RoleID(ctx, i.Mount, role)
	if err != nil {
		return "", vault.Wrapped{}, err
	}
	w, err := i.Vault.WrappedSecretID(ctx, i.Mount, role, i.WrapTTL, map[string]string{"instance": instance, "unit": unit})
	return roleID, w, err
}

func key(instance, unit, n string) string { return instance + "." + unit + "." + n }

// all reads every issue.
func (i *Issuer) all(ctx context.Context) (map[string]Issued, map[string]uint64, error) {
	kv, err := i.kv(ctx)
	if err != nil {
		return nil, nil, err
	}
	keys, err := kv.ListKeys(ctx)
	if err != nil {
		return nil, nil, log.Wrap(err)
	}
	out, revs := map[string]Issued{}, map[string]uint64{}
	for k := range keys.Keys() {
		e, err := kv.Get(ctx, k)
		if err != nil {
			continue
		}
		var rec Issued
		if json.Unmarshal(e.Value(), &rec) == nil {
			out[k], revs[k] = rec, e.Revision()
		}
	}
	return out, revs, nil
}

// Revoke revokes every issue of a unit; it returns how many it revoked.
func (i *Issuer) Revoke(ctx context.Context, instance, unit string) (int, error) {
	recs, revs, err := i.all(ctx)
	if err != nil {
		return 0, err
	}
	var keys []string
	for k, r := range recs {
		if r.Instance == instance && r.Unit == unit && r.RevokedAt == nil {
			keys = append(keys, k)
		}
	}
	return len(keys), i.revoke(ctx, recs, revs, keys)
}

func (i *Issuer) revoke(ctx context.Context, recs map[string]Issued, revs map[string]uint64, keys []string) error {
	kv, err := i.kv(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, k := range keys {
		r := recs[k]
		now := time.Now().UTC()
		r.RevokedAt = &now
		raw, _ := json.Marshal(r)
		if _, err := kv.Update(ctx, k, raw, revs[k]); err != nil && !errors.Is(err, jetstream.ErrKeyExists) {
			errs = append(errs, log.Wrap(err)) // another process revoked it meanwhile: fine
		}
		if err := i.Vault.DestroySecretID(ctx, i.Mount, r.Role, r.Accessor); err != nil {
			errs = append(errs, err)
		}
		log.With("instance", r.Instance).With("unit", r.Unit).Info(ctx, "issuer: revoked unit %s on instance %s", r.Unit, r.Instance)
	}
	if err := i.PushRevocations(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// PushRevocations signs the services account again with every revoked
// user, read from ctl_issued, and pushes it: the list is rebuilt whole
// each time, so concurrent pushes converge.
func (i *Issuer) PushRevocations(ctx context.Context) error {
	recs, _, err := i.all(ctx)
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	acc := i.D.Account(natsauth.App)
	list := jwt.RevocationList{}
	keys := make([]string, 0, len(recs))
	for k := range recs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if r := recs[k]; r.RevokedAt != nil && r.Account == natsauth.App {
			list.Revoke(r.User, r.RevokedAt.Add(time.Second))
		}
	}
	acc.Claims.Revocations = list
	if err := i.D.Operator.Sign(acc); err != nil {
		return err
	}
	if i.Sys == nil {
		return nil
	}
	return natsauth.Push(i.Sys, acc)
}

// Sweep revokes every issue whose unit desired state no longer places
// with the same set.
func (i *Issuer) Sweep(ctx context.Context) error {
	recs, revs, err := i.all(ctx)
	if err != nil {
		return err
	}
	var keys []string
	for k, r := range recs {
		if r.RevokedAt != nil {
			continue
		}
		set, ok, err := i.placed(ctx, r.Instance, r.Unit)
		if err != nil {
			return err
		}
		if !ok || set != r.Set {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return i.revoke(ctx, recs, revs, keys)
}

// watch sweeps on every change of an instance's desired state.
func (i *Issuer) watch(ctx context.Context) {
	for ctx.Err() == nil {
		kv, err := i.C.KV(ctx, ctl.DesiredBucket)
		if err == nil {
			var w jetstream.KeyWatcher
			if w, err = kv.Watch(ctx, ctl.KindInstance+".*", jetstream.UpdatesOnly()); err == nil {
				for e := range w.Updates() {
					if e == nil {
						continue
					}
					if err := i.Sweep(ctx); err != nil {
						log.Error(ctx, err, "issuer: sweep failed")
					}
				}
				_ = w.Stop()
			}
		}
		if err != nil && ctx.Err() == nil {
			log.Error(ctx, err, "issuer: cannot watch desired state")
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}
