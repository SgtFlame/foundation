package connections

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/hooks"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
)

// Hook registration (forge 26b-2): a connection whose type receives
// callbacks has its hook registered with the service for the callback
// events triggers listen to, by its type's Registrar over cx.register,
// whenever those events or the public URL change, and removed when no
// trigger listens. Every process's Reconcile may do it: the lock
// hook.<connection> in cx_locks_<tenant> and a second look after taking it
// make it once, and a failure is tried again on the next pass.

// hookKept is the hook secret's value.
type hookKept struct {
	Registration *connectors.HookRegistration `json:"registration,omitempty"`
	Secret       string                       `json:"secret,omitempty"`
}

// publicBase is where services reach this deployment.
func publicBase(ctx context.Context, d Deps, tenant uint64) string {
	if d.Config != nil {
		args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
		if l, err := config.Read[hooks.Limits](d.Config, args, hooks.SectionID); err == nil && l.PublicBaseURL != "" {
			return strings.TrimRight(l.PublicBaseURL, "/")
		}
	}
	return strings.TrimRight(d.PublicBase, "/")
}

// sameHook reports whether two states say the same, whenever they were
// made.
func sameHook(a, b *HookState) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, y := *a, *b
	x.At, y.At = time.Time{}, time.Time{}
	return x.URL == y.URL && slices.Equal(x.Events, y.Events) && x.Registered == y.Registered && x.Manual == y.Manual && x.Error == y.Error
}

// syncHook makes c's registration what events (the listened callback
// events' subscription names, sorted) need.
func syncHook(ctx context.Context, d Deps, tenant uint64, c Connection, events []string) error {
	var want *HookState
	if len(events) > 0 {
		h, err := hooks.Ensure(ctx, d.Projections.JetStream(), tenant, c.ID, c.Type)
		if err != nil {
			return err
		}
		if base := publicBase(ctx, d, tenant); base == "" {
			want = &HookState{Events: events, Error: "no public URL: set Settings › System › Connectors › Connector hooks › Public base URL"}
		} else {
			want = &HookState{URL: h.URL(base), Events: events}
		}
	}
	if done(c.Hook, want) {
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := connectors.Lock(lctx, d.Projections.JetStream(), tenant, "hook."+c.ID, "reconcile-"+strconv.FormatUint(features.NewTSID(), 10))
	if err != nil {
		return err
	}
	defer release()
	if now, ok, err := Get(ctx, d.Projections, tenant, c.ID); err != nil || !ok {
		return err
	} else {
		c = now
	}
	if done(c.Hook, want) {
		return nil
	}
	kept := readHook(ctx, d, tenant, c)
	if want == nil || want.URL == "" { // remove what the service holds
		if kept.Registration != nil {
			if rep := askRegister(ctx, d, tenant, c, connectors.RegisterRequest{Op: connectors.HookUnregister, Registration: kept.Registration}); rep.Error != nil && rep.Error.Code != connectors.CodeNotFound {
				log.With("connection", c.ID).Warn(ctx, "the hook of connection %s could not be removed: %s", c.ID, rep.Error.Message)
				return nil // tried again on the next pass
			}
		}
		return setHook(ctx, d, tenant, c, want, kept.Registration != nil, nil)
	}
	req := connectors.RegisterRequest{Op: connectors.HookRegister, URL: want.URL, Events: want.Events}
	if kept.Registration != nil {
		req.Op, req.Registration = connectors.HookUpdate, kept.Registration
	}
	rep := askRegister(ctx, d, tenant, c, req)
	switch {
	case rep.Error != nil && rep.Error.Code == connectors.CodeNotSupported:
		want.Manual = true // a person pastes the URL into the service
		return setHook(ctx, d, tenant, c, want, false, nil)
	case rep.Error != nil:
		want.Error = rep.Error.Code + ": " + rep.Error.Message
		if sameHook(c.Hook, want) {
			return nil
		}
		return setHook(ctx, d, tenant, c, want, false, nil)
	}
	want.Registered = true
	secret := rep.Secret
	if secret == "" {
		secret = kept.Secret // an update that keeps the signing secret
	}
	return setHook(ctx, d, tenant, c, want, false, &hookKept{Registration: rep.Registration, Secret: secret})
}

// done reports whether nothing is left to do: the state is what is
// wanted, or a registration or manual hook for the same URL and events.
func done(have, want *HookState) bool {
	if want == nil {
		return have == nil
	}
	if have == nil || have.URL != want.URL || !slices.Equal(have.Events, want.Events) {
		return false
	}
	return have.Registered || have.Manual || (want.URL == "" && have.Error == want.Error)
}

func readHook(ctx context.Context, d Deps, tenant uint64, c Connection) hookKept {
	var k hookKept
	if v, ok := c.Secrets[HookSecret]; ok {
		if raw, err := secretValue(ctx, d, tenant, c.ID, HookSecret, v); err == nil {
			_ = json.Unmarshal([]byte(raw), &k)
		}
	}
	return k
}

func askRegister(ctx context.Context, d Deps, tenant uint64, c Connection, req connectors.RegisterRequest) connectors.RegisterReply {
	if d.Dispatcher == nil || d.Dispatcher.NC == nil {
		return connectors.RegisterReply{Error: connectors.Errorf(connectors.CodeUnavailable, "no connector bus")}
	}
	raw, _ := json.Marshal(req)
	rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	m, err := d.Dispatcher.NC.RequestWithContext(rctx, connectors.RegisterSubject(tenant, c.Type, c.ID), raw)
	if err != nil {
		return connectors.RegisterReply{Error: connectors.Errorf(connectors.CodeUnavailable, "no adapter registered the hook of connection %s", c.Name)}
	}
	var rep connectors.RegisterReply
	if err := json.Unmarshal(m.Data, &rep); err != nil {
		return connectors.RegisterReply{Error: connectors.Errorf(connectors.CodeUnavailable, "the registration's answer is not readable")}
	}
	return rep
}

// setHook commits a hook state as the tenant's service principal: with
// kept, the registration as the hook secret; clear removes the secret.
func setHook(ctx context.Context, d Deps, tenant uint64, c Connection, state *HookState, clear bool, kept *hookKept) error {
	a := map[string]any{"hook": state}
	switch {
	case kept != nil:
		raw, _ := json.Marshal(kept)
		a["secrets"] = map[string]any{HookSecret: string(raw)}
	case clear || state == nil:
		if _, ok := c.Secrets[HookSecret]; ok {
			a["secrets"] = map[string]any{HookSecret: nil}
		}
	}
	body, _ := json.Marshal(a)
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
	res := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "hook-" + strconv.FormatUint(features.NewTSID(), 10), Type: "connection.setHook", Target: c.ID, Args: body})
	if res.Common.Code >= 300 {
		return log.Code("hook_not_kept").Errorf("the hook state of connection %s could not be kept: %s", c.ID, res.Common.Message)
	}
	_, err := d.Projections.WaitFor(ctx, tenant, Projector, TargetType+"."+c.ID, res.Seq, 10*time.Second)
	return err
}
