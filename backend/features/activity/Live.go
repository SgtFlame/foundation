package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go/jetstream"
)

// Resource is a person's own activity.
func Resource(p features.Principal) string {
	return authz.Tenant(p) + "/" + authz.Resource("user", p.UserID) + "/activity"
}

// Contribute registers activity.mine and the activity.focus,
// activity.dismiss and activity.clear calls. They are calls, not
// commands: a command commits to an area stream kept forever, and none
// of this is kept; and none is a tool, since no one but the person (no
// agent, no chat round) reads or changes their history.
func Contribute(reg *extensions.Registry, r *Recorder) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "activity.mine", Source: "activity"}, Title: "My recent activity",
		Description: "The caller's own recent gestures, newest last: {recording, gestures: [{id, start, time, app, subject {type, id, label, details}, summary, repeats, dismissed, entries: [{id, kind (focus|ui|command|op), request, time, app, subject, page, title, handler, event, path, node, label, command, commandTitle, entity, entityName, target, recordTitle, change, fields: [{id, name, value, omitted}], summary}]}]}. after keeps the gestures that ended after a time, limit the newest (50), sendable leaves out the dismissed. Only a person's own history: it takes no one else's.",
		Params:      json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"after":{"type":"string","description":"An RFC 3339 time"},"limit":{"type":"integer","minimum":1,"maximum":1000},"sendable":{"type":"boolean","description":"Leave out the gestures the caller dismissed"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"sendable":true}`)},
		Action:      "activity.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run:         func(f live.Feed) error { return mine(f, r) },
	})
	call := func(id, title, description string, run func(c live.CallRun, user uint64) (any, *features.ServiceResponse)) {
		live.Calls.Register(reg, live.Call{
			Descriptor: extensions.Descriptor{ID: id, Source: "activity"}, Title: title, Description: description + " Ends with one result event.",
			Run: func(c live.CallRun) *features.ServiceResponse {
				user, ok := Person(c.Principal)
				if !ok {
					return c.Fail(http.StatusForbidden, log.Code("people_only").New("only a person has recent activity"))
				}
				if err := authz.Check(c.Context(), c.Principal, "activity.write", Resource(c.Principal)).Err("activity.write", Resource(c.Principal)); err != nil {
					return c.Fail(authz.Status, err)
				}
				res, fail := run(c, user)
				if fail != nil {
					return fail
				}
				c.Emit("result", res)
				return nil
			},
		})
	}
	call("activity.focus", "Record a focused tab", "Records that the caller focused a tab showing subject {type, id, label, details}; id, when given, makes a resent one a duplicate.",
		func(c live.CallRun, _ uint64) (any, *features.ServiceResponse) {
			var a struct {
				ID      string `json:"id"`
				Subject Thing  `json:"subject"`
			}
			if err := strict(c.Args, &a); err != nil || a.Subject.Type == "" || a.Subject.ID == "" {
				return nil, c.Fail(http.StatusBadRequest, log.Code("invalid_args").New("activity.focus takes {id?, subject: {type, id, label?, details?}}"))
			}
			if a.ID != "" && !live.ValidToken(a.ID) {
				return nil, c.Fail(http.StatusBadRequest, log.Code("invalid_args").Errorf("%q is not a valid id", a.ID))
			}
			a.Subject.Label = cut(a.Subject.Label, 120)
			if err := r.Focus(c.ServiceArgs, a.ID, a.Subject); err != nil {
				return nil, c.Fail(http.StatusInternalServerError, err)
			}
			return map[string]bool{"ok": true}, nil
		})
	call("activity.dismiss", "Dismiss gestures", "Marks the caller's gestures {ids} so they are never sent to a model; activity.mine with sendable leaves them out.",
		func(c live.CallRun, user uint64) (any, *features.ServiceResponse) {
			var a struct {
				IDs []string `json:"ids"`
			}
			if err := strict(c.Args, &a); err != nil || len(a.IDs) == 0 {
				return nil, c.Fail(http.StatusBadRequest, log.Code("invalid_args").New("activity.dismiss takes {ids: [gesture ids]}"))
			}
			slices.Sort(a.IDs)
			e := Entry{ID: "dismiss-" + strconv.FormatUint(features.NewTSID(), 36), Kind: KindDismiss, IDs: slices.Compact(a.IDs)}
			if err := r.write(c.Context(), c.Principal.TenantID, user, e); err != nil {
				return nil, c.Fail(http.StatusInternalServerError, err)
			}
			return map[string]bool{"ok": true}, nil
		})
	call("activity.clear", "Clear my recent activity", "Deletes everything kept of the caller's recent activity.",
		func(c live.CallRun, user uint64) (any, *features.ServiceResponse) {
			if err := r.Clear(c.Context(), c.Principal.TenantID, user); err != nil {
				return nil, c.Fail(http.StatusInternalServerError, err)
			}
			return map[string]bool{"ok": true}, nil
		})
}

// strict decodes arguments, refusing fields it does not know: a crafted
// user or principal is refused rather than ignored.
func strict(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Clear deletes a person's history, and tells their open views so: a
// clear entry ends what they show (unless recording is off, when nothing
// is written).
func (r *Recorder) Clear(ctx context.Context, tenant, user uint64) error {
	d := r.d()
	if d.Store == nil {
		return nil
	}
	if err := d.Store.Purge(ctx, tenant, user); err != nil {
		return err
	}
	set := SettingsOf(ctx, d, tenant, user)
	if !set.Record {
		return nil
	}
	return d.Store.Append(ctx, tenant, user, Entry{ID: "clear-" + strconv.FormatUint(features.NewTSID(), 36), Kind: KindClear, Time: time.Now().UTC()}, set)
}

// regroup is how often an open activity.mine looks again at what has
// expired and at the person's settings.
var regroup = time.Minute

// mine runs activity.mine for the caller alone.
func mine(f live.Feed, r *Recorder) error {
	args := f.Args()
	user, ok := Person(args.Principal)
	if !ok {
		return log.Code("people_only").New("only a person has recent activity")
	}
	var q Params
	if err := strict(f.Params(), &q); err != nil {
		return log.Code("invalid_params").Errorf("activity.mine takes {after?, limit?, sendable?}: %v", err)
	}
	d := r.d()
	if d.Store == nil {
		f.Snapshot(Activity{Gestures: []Gesture{}}, live.Position{})
		<-f.Context().Done()
		return nil
	}
	ctx, tenant := f.Context(), args.Principal.TenantID
	var mu sync.Mutex
	var entries []Entry
	wake := make(chan struct{}, 1)
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	caught := false
	failed := make(chan error, 1)
	go func() {
		failed <- d.Store.Follow(ctx, tenant, user, func(e Entry) {
			mu.Lock()
			entries = append(entries, e)
			mu.Unlock()
			signal()
		}, func() {
			mu.Lock()
			caught = true
			mu.Unlock()
			signal()
		})
	}()
	tick := time.NewTicker(regroup)
	defer tick.Stop()
	var last []byte
	sent := false
	send := func() {
		mu.Lock()
		if !caught {
			mu.Unlock()
			return
		}
		all := slices.Clone(entries)
		mu.Unlock()
		a := Group(all, SettingsOf(ctx, d, tenant, user), time.Now().UTC(), q)
		raw, _ := json.Marshal(a)
		if sent && bytes.Equal(raw, last) {
			return
		}
		last = raw
		if !sent {
			sent = true
			f.Snapshot(a, live.Position{})
			return
		}
		f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failed:
			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				err = log.New("the activity stream ended")
			}
			return err
		case <-wake:
			send()
		case <-tick.C:
			send()
		}
	}
}

// Read is a person's gestures now, for Go callers (the chat, in 15d) and
// the developer tools.
func Read(ctx context.Context, d Deps, tenant, user uint64, q Params) (Activity, error) {
	set := SettingsOf(ctx, d, tenant, user)
	if d.Store == nil || !set.Record {
		return Activity{Recording: set.Record, Gestures: []Gesture{}}, nil
	}
	entries, err := d.Store.Read(ctx, tenant, user)
	if err != nil {
		return Activity{}, err
	}
	return Group(entries, set, time.Now().UTC(), q), nil
}

// WatchSettings clears the history of everyone whose recording a change
// to the activity section turned off, until ctx ends. Any process that
// stored the change does it; a purge done twice is harmless.
func WatchSettings(ctx context.Context, changes *config.Changes, d func() Deps) {
	if changes == nil {
		return
	}
	unsubscribe := changes.Subscribe(func(c config.Changed) {
		if c.Section != SectionID {
			return
		}
		go func() {
			if err := purgeOff(context.WithoutCancel(ctx), d(), c.Scope); err != nil {
				log.Warn(ctx, "activity: cannot clear after a settings change at %s: %v", c.Scope, err)
			}
		}()
	})
	go func() {
		<-ctx.Done()
		unsubscribe()
	}()
}

// purgeOff purges the people under scope (user:<id>, tenant:<id>, or
// everyone) whose recording is off.
func purgeOff(ctx context.Context, d Deps, scope string) error {
	if d.Store == nil {
		return nil
	}
	kind, id, _ := strings.Cut(scope, ":")
	only, _ := strconv.ParseUint(id, 10, 64)
	names := d.Store.js.StreamNames(ctx, jetstream.WithStreamListSubject("act.>"))
	for name := range names.Name() {
		var tenant uint64
		if _, err := fmt.Sscanf(name, "ACT_%d", &tenant); err != nil || kind == "tenant" && tenant != only {
			continue
		}
		people, err := d.Store.People(ctx, tenant)
		if err != nil {
			return err
		}
		for _, user := range people {
			if kind == "user" && user != only {
				continue
			}
			if !SettingsOf(ctx, d, tenant, user).Record {
				if err := d.Store.Purge(ctx, tenant, user); err != nil {
					return err
				}
			}
		}
	}
	return names.Err()
}
