package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// EphemeralQuery follows one ephemeral topic's signals (forge 26d-1).
const EphemeralQuery = "live.ephemeral"

// Ephemeral signals live this long unless the publisher says otherwise,
// and never longer than EphemeralMaxTTL.
const (
	EphemeralTTL    = 5 * time.Second
	EphemeralMaxTTL = 60 * time.Second
)

// EphemeralSubject is a signal's core NATS subject: never stored.
func EphemeralSubject(tenant uint64, app, topic, key string) string {
	return fmt.Sprintf("eph.%d.%s.%s.%s", tenant, app, topic, key)
}

// Signal is one ephemeral signal, as its subscribers see it: the latest
// from each sender, kept until its time to live passes.
type Signal struct {
	From    string    `json:"from"` // the sender's user id, or agent:<id>
	Value   any       `json:"value"`
	Expires time.Time `json:"expires"`
}

// conns gives the NATS connection of a tenant's account.
type conns interface {
	JetStreamFor(ctx context.Context, tenant uint64) (jetstream.JetStream, error)
}

func connOf(ctx context.Context, store any, tenant uint64) (*nats.Conn, error) {
	c, ok := store.(conns)
	if !ok {
		return nil, log.Code("no_nats").New("there is no NATS connection for ephemeral signals")
	}
	js, err := c.JetStreamFor(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return js.Conn(), nil
}

// ephemeralAccess checks that args' principal may read or write (write)
// the record a signal of an installed app's topic is keyed by, and gives
// the topic's subject.
func ephemeralAccess(args features.ServiceArgs, d Deps, app, topic, key string, write bool) (string, *features.ServiceResponse) {
	ctx := args.Context()
	in, ok := Running(ctx, d.Projections, args.Principal.TenantID, app)
	if !ok {
		return "", args.Fail(http.StatusNotFound, log.Code("app_not_installed").Errorf("the app %s is not installed", app))
	}
	t, ok := in.Version.Manifest.EphemeralTopic(topic)
	if !ok {
		return "", args.Fail(http.StatusNotFound, log.Code("topic_not_found").Errorf("the app %s has no ephemeral topic %q", app, topic))
	}
	if !live.ValidToken(key) {
		return "", args.Fail(http.StatusBadRequest, log.Code("bad_key").Errorf("%q is not a record id", key))
	}
	entity, read, wr, ok := t.Entity()
	if !ok {
		return "", args.Fail(http.StatusInternalServerError, log.Errorf("the topic %s of %s names no entity action", topic, app))
	}
	act := read
	if write {
		act = wr
	}
	p := args.Principal
	if err := authz.Check(ctx, p, "record."+act, records.Resource(p, entity)).Err("record."+act, entity); err != nil {
		return "", args.Fail(authz.Status, err)
	}
	e, problems := data.Entity(args, d.Projections, entity)
	if problems != nil {
		return "", args.Fail(http.StatusNotFound, log.Code("entity_not_found").Errorf("%s", problems[0].Message))
	}
	v, found, err := d.Projections.Get(ctx, p.TenantID, data.Projector, data.Key(entity, key))
	if err != nil {
		return "", args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	var r data.Record
	if found && v.Value != nil {
		_ = json.Unmarshal(v.Value, &r)
	}
	rule, _ := e.RuleFor(ctx, p, act)
	if r == nil || !records.Allows(rule, r, p) {
		return "", args.Fail(http.StatusNotFound, log.Code("record_not_found").Errorf("there is no %s %s", entity, key))
	}
	return EphemeralSubject(p.TenantID, app, topic, key), nil
}

// sender is who a signal comes from.
func sender(p features.Principal) string {
	if p.Kind == features.PrincipalAgent {
		return "agent:" + strconv.FormatUint(p.AgentID, 10)
	}
	return strconv.FormatUint(p.UserID, 10)
}

// PublishEphemeral sends one signal of an installed app's topic, keyed by
// a record, as args' principal, if it may write that record: Publish's
// work. Nothing is stored; whoever follows the topic now sees it until ttl
// passes (EphemeralTTL when 0).
func PublishEphemeral(args features.ServiceArgs, d Deps, app, topic, key string, value any, ttl time.Duration) *features.ServiceResponse {
	subject, fail := ephemeralAccess(args, d, app, topic, key, true)
	if fail != nil {
		return fail
	}
	if ttl <= 0 {
		ttl = EphemeralTTL
	}
	ttl = min(ttl, EphemeralMaxTTL)
	raw, _ := json.Marshal(Signal{From: sender(args.Principal), Value: value, Expires: time.Now().Add(ttl).UTC()})
	nc, err := connOf(args.Context(), d.Projections, args.Principal.TenantID)
	if err != nil {
		return args.Fail(http.StatusServiceUnavailable, err)
	}
	if err := nc.Publish(subject, raw); err != nil {
		return args.Fail(http.StatusServiceUnavailable, log.Wrap(err))
	}
	return nil
}

type ephemeralParams struct {
	App   string `json:"app"`
	Topic string `json:"topic"`
	Key   string `json:"key"`
}

func contributeEphemeral(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: EphemeralQuery, Source: "apps"}, Title: "Ephemeral signals",
		Description: "The latest ephemeral signal of an installed app's topic from each sender, keyed by a record (who is typing in a channel): {<sender>: {from, value, expires}}, a sender removed once its signal's time to live passes. Signals are never stored, so a new subscriber sees only those sent after it.",
		Params:      json.RawMessage(`{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"topic":{"type":"string","description":"One of its manifest's ephemeral topics"},"key":{"type":"string","description":"The record's id the signals are keyed by"}},"required":["app","topic","key"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"app":"nexus","topic":"typing","key":"0K1Z2Y3X4W5V6"}`)},
		Run: func(f live.Feed) error {
			var a ephemeralParams
			_ = json.Unmarshal(f.Params(), &a)
			args := f.Args()
			subject, fail := ephemeralAccess(args, deps(), a.App, a.Topic, a.Key, false)
			if fail != nil {
				return log.Code(fail.ErrorCode).Errorf("%s", fail.Message)
			}
			nc, err := connOf(f.Context(), f.Store(), args.Principal.TenantID)
			if err != nil {
				return err
			}
			got := make(chan Signal, 64)
			sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
				var s Signal
				if json.Unmarshal(m.Data, &s) == nil && s.From != "" {
					select {
					case got <- s:
					default: // a slow subscriber drops signals rather than blocking NATS
					}
				}
			})
			if err != nil {
				return log.Wrap(err)
			}
			defer func() { _ = sub.Unsubscribe() }()
			held := map[string]Signal{}
			f.Snapshot(map[string]Signal{}, live.Position{})
			tick := time.NewTicker(500 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-f.Context().Done():
					return nil
				case s := <-got:
					if limit := time.Now().Add(EphemeralMaxTTL); s.Expires.After(limit) {
						s.Expires = limit
					}
					held[s.From] = s
					raw, _ := json.Marshal(s)
					f.Change(live.Delta{Op: "upsert", Key: s.From, Value: raw}, live.Position{})
				case now := <-tick.C:
					for from, s := range held {
						if now.After(s.Expires) {
							delete(held, from)
							f.Change(live.Delta{Op: "remove", Key: from}, live.Position{})
						}
					}
				}
			}
		},
	})

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "live.publish", Source: "apps", Order: 580}, Title: "Publish", Category: "Commands", Effects: true,
		Kinds:       []string{"ui.handler", "server.action", "prefab.logic"},
		Description: "Sends an ephemeral signal of one of the app's topics (its manifest's ephemeral), keyed by a record, as the run's user: never stored, seen by whoever may read that record and follows the topic now, until its time to live passes. For typing and the like, never for data.",
		Example:     "Publish typing on the channel's id with value true, for 5 seconds",
		Examples:    cognition.ConfigExample("live.publish", `{"topic":"typing"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","title":"Topic","description":"One of the app's ephemeral topics, e.g. typing"},"app":{"type":"string","title":"App","description":"The app whose topic it is; the graph's own app when empty"}},"required":["topic"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("key", "Key", "string", "The id of the record the signal is about, e.g. a channel"),
				{ID: "value", Name: "Value", Type: cognition.Any, Optional: true, Description: "What the signal says; true when unconnected"},
				{ID: "ttl", Name: "Time to live", Type: "number", Optional: true, Description: "Seconds it lasts, 5 when unconnected, at most 60"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Topic, App string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			app := c.App
			if app == "" {
				app = strings.TrimPrefix(x.Owner, "app:")
			}
			if app == "" || app == x.Owner {
				return nil, "", fmt.Errorf("Publish needs an app: name one, or run in an app's own graph")
			}
			key, _ := cognition.Get[string](in, "key")
			var value any = true
			if v, ok := in["value"]; ok && v != nil {
				value = v
			}
			var ttl time.Duration
			if s, ok := in["ttl"].(float64); ok && s > 0 {
				ttl = time.Duration(s * float64(time.Second))
			}
			if fail := PublishEphemeral(x.ServiceArgs, deps(), app, c.Topic, key, value, ttl); fail != nil {
				return nil, "", fmt.Errorf("%s", fail.Message)
			}
			return nil, cognition.Then, nil
		},
	})

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "ui.subscribeEphemeral", Source: "apps", Order: 590}, Title: "Subscribe to signals", Category: "UI", Effects: true,
		Kinds:       []string{"prefab.logic"},
		Description: "Keeps one of the app's ephemeral topics, keyed by a record, at a path of the instance's state while it is mounted: {<sender>: {from, value, expires}}, the latest signal from each sender until its time to live passes (who is typing). Subscribing the same path again with another key replaces it.",
		Example:     "Subscribe to signals typing on props.channel into typing, then show the senders other than user.id",
		Examples:    cognition.ConfigExample("ui.subscribeEphemeral", `{"topic":"typing","path":"typing"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","title":"Topic","description":"One of the app's ephemeral topics"},"path":{"type":"string","title":"State path","description":"Where the signals are kept, e.g. typing"},"app":{"type":"string","title":"App","description":"The app whose topic it is; the graph's own app when empty"}},"required":["topic","path"]}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("key", "Key", "string", "The id of the record the signals are about")},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Topic, Path, App string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			h, ok := x.Host.(interface {
				Subscribe(path, query string, params any) error
			})
			if !ok {
				return nil, "", fmt.Errorf("Subscribe to signals runs only in a prefab's logic")
			}
			app := c.App
			if app == "" {
				app = strings.TrimPrefix(x.Owner, "app:")
			}
			if app == "" || app == x.Owner {
				return nil, "", fmt.Errorf("Subscribe to signals needs an app: name one, or run in an app's own graph")
			}
			key, _ := cognition.Get[string](in, "key")
			return nil, cognition.Then, h.Subscribe(c.Path, EphemeralQuery, map[string]any{"app": app, "topic": c.Topic, "key": key})
		},
	})
}
