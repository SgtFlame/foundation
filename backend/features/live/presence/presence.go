// Package presence keeps who is in an object, and what each connection has
// selected there, in a KV bucket per tenant whose keys live 30 seconds
// unless refreshed (documentation/Live.md › Presence). The live.presence
// query follows one object.
package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go/jetstream"
)

var log = live.Log.Tags("presence")

// TTL is how long a connection's presence lasts without a refresh.
const TTL = 30 * time.Second

// Refresh is how often a live connection refreshes its presence.
const Refresh = 10 * time.Second

// QueryID is the presence query.
const QueryID = "live.presence"

// Colors are the users' colours, picked by user id.
var Colors = []string{"#e8590c", "#1c7ed6", "#2f9e44", "#ae3ec9", "#f08c00", "#0ca678", "#d6336c", "#5f3dc4"}

// ColorOf is a user's colour.
func ColorOf(userID uint64) string { return Colors[userID%uint64(len(Colors))] }

// Entry is one connection's presence in an object.
type Entry struct {
	Connection string `json:"connection"`
	UserID     uint64 `json:"userId,string"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	Actor      string `json:"actor,omitempty"` // who is there for the user, not by hand: the chat's Builder ("builder", forge 06g)
	// AgentName names an agent there for the user (forge 07j; Actor is
	// agent:<id>), drawn as its own avatar in its own colour.
	AgentName  string          `json:"agentName,omitempty"`
	AgentColor string          `json:"agentColor,omitempty"`
	State      json.RawMessage `json:"state,omitempty"` // selection, cursor, what it is editing
	At         time.Time       `json:"at"`
}

// Presence is the presence buckets.
type Presence struct {
	js       jetstream.JetStream
	userName func(uint64) string
	mu       sync.Mutex
	buckets  map[uint64]jetstream.KeyValue
}

// New makes the presence store; userName names a user for the others.
func New(js jetstream.JetStream, userName func(uint64) string) *Presence {
	if userName == nil {
		userName = func(id uint64) string { return fmt.Sprintf("User %d", id) }
	}
	return &Presence{js: js, userName: userName, buckets: map[uint64]jetstream.KeyValue{}}
}

func (p *Presence) bucket(ctx context.Context, tenant uint64) (jetstream.KeyValue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if kv := p.buckets[tenant]; kv != nil {
		return kv, nil
	}
	kv, err := p.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: fmt.Sprintf("presence_%d", tenant), Description: "Who is in each object", History: 1, TTL: TTL, Storage: jetstream.MemoryStorage,
	})
	if err != nil {
		return nil, log.Wrapf(err, "cannot make the presence bucket")
	}
	p.buckets[tenant] = kv
	return kv, nil
}

// owned holds the object type prefixes whose objects belong to one person:
// an object of type "<prefix>_<userId>" (a person's workspace, forge 16b:
// ws_<userId>.<workspaceId>) is seen and joined by that user alone.
var (
	ownedMu sync.RWMutex
	owned   = map[string]bool{}
)

// Owned makes the objects of type "<prefix>_<userId>" private to that user:
// only they may be present there or follow who is (forge 16b's windows on
// one workspace). It is called once, when a feature contributes.
func Owned(prefix string) {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	owned[prefix] = true
}

// Allowed reports whether a principal may be present in object and follow
// who is there: anyone may, but for an object one person owns (Owned),
// where only that person, as themselves, may.
func Allowed(principal features.Principal, object string) bool {
	typ, _, _ := strings.Cut(object, ".")
	prefix, user, ok := strings.Cut(typ, "_")
	if !ok {
		return true
	}
	ownedMu.RLock()
	private := owned[prefix]
	ownedMu.RUnlock()
	if !private {
		return true
	}
	return principal.Kind == features.PrincipalUser && strconv.FormatUint(principal.UserID, 10) == user
}

// ValidObject reports whether object is "<type>.<id>".
func ValidObject(object string) bool {
	typ, id, ok := strings.Cut(object, ".")
	return ok && live.ValidToken(typ) && live.ValidToken(id)
}

// Set puts (or refreshes) a connection's presence in object.
func (p *Presence) Set(ctx context.Context, principal features.Principal, object, conn string, state json.RawMessage) error {
	return p.SetAs(ctx, principal, object, conn, "", state)
}

// SetAs is Set for an actor working for the principal, such as the chat's
// Builder: conn is the actor's own (e.g. builder-<round>), never a
// browser's.
func (p *Presence) SetAs(ctx context.Context, principal features.Principal, object, conn, actor string, state json.RawMessage) error {
	if !ValidObject(object) || !live.ValidToken(conn) {
		return log.Code("invalid_object").Errorf("%q is not an object (<type>.<id>)", object)
	}
	if !Allowed(principal, object) {
		return log.Code("forbidden").Errorf("%s is another person's", object)
	}
	kv, err := p.bucket(ctx, principal.TenantID)
	if err != nil {
		return err
	}
	user := principal.UserID
	e := Entry{Connection: conn, Actor: actor, State: state, At: time.Now().UTC()}
	if principal.Kind == features.PrincipalAgent {
		// An agent is present for whom its run acts, as an actor of its own
		user, e.Actor = principal.OnBehalfOf, principal.String()
		e.AgentName, e.AgentColor = fmt.Sprintf("Agent %d", principal.AgentID), ColorOf(principal.AgentID)
		if a, ok, _ := authz.FindAgent(ctx, principal.TenantID, principal.AgentID); ok {
			e.AgentName = a.Name
		}
	}
	name := p.userName(user)
	if name == "" {
		name = fmt.Sprintf("User %d", user)
	}
	e.UserID, e.Name, e.Color = user, name, ColorOf(user)
	raw, _ := json.Marshal(e)
	_, err = kv.Put(ctx, object+"."+conn, raw)
	return log.Wrap(err)
}

// Clear removes a connection's presence in object.
func (p *Presence) Clear(ctx context.Context, tenant uint64, object, conn string) {
	kv, err := p.bucket(ctx, tenant)
	if err != nil {
		return
	}
	if err := kv.Delete(ctx, object+"."+conn); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		log.Error(ctx, err, "cannot clear presence in %s", object)
	}
}

// Here reports whether anyone is present in object, e.g. a graph open in
// an editor.
func (p *Presence) Here(ctx context.Context, tenant uint64, object string) bool {
	kv, err := p.bucket(ctx, tenant)
	if err != nil {
		return false
	}
	keys, err := kv.ListKeysFiltered(ctx, object+".*")
	if err != nil {
		return false
	}
	defer keys.Stop()
	for range keys.Keys() {
		return true
	}
	return false
}

// Contribute registers the live.presence query, given the store once made.
func Contribute(reg *extensions.Registry, presence func() *Presence) {
	live.Queries.Register(reg, live.Query{
		Descriptor:  extensions.Descriptor{ID: QueryID, Source: "live"},
		Title:       "Who is here",
		Description: "The connections present in one object (\"<type>.<id>\", e.g. \"app.hello\"), each with its user, colour and state (selection, cursor). Changes as people come, go and select.",
		Params:      json.RawMessage(`{"type":"object","properties":{"object":{"type":"string","description":"The object, \"<type>.<id>\""}},"required":["object"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"object":"app.hello"}`)},
		Run:         func(f live.Feed) error { return presence().run(f) },
	})
}

func (p *Presence) run(f live.Feed) error {
	var params struct{ Object string }
	_ = json.Unmarshal(f.Params(), &params)
	if !ValidObject(params.Object) {
		return log.Code("invalid_object").Errorf("%q is not an object (<type>.<id>)", params.Object)
	}
	if !Allowed(f.Args().Principal, params.Object) {
		return log.Code("forbidden").Errorf("%s is another person's", params.Object)
	}
	ctx := f.Context()
	kv, err := p.bucket(ctx, f.Args().Principal.TenantID)
	if err != nil {
		return err
	}
	w, err := kv.Watch(ctx, params.Object+".*")
	if err != nil {
		return log.Wrap(err)
	}
	defer func() { _ = w.Stop() }()
	// Expired keys send no update, so the list is pruned by age as well
	tick := time.NewTicker(Refresh / 2)
	defer tick.Stop()
	entries := map[string]Entry{}
	started := false
	send := func(snapshot bool) {
		list := make([]Entry, 0, len(entries))
		for _, e := range entries {
			list = append(list, e)
		}
		slices.SortFunc(list, func(a, b Entry) int { return strings.Compare(a.Connection, b.Connection) })
		raw, _ := json.Marshal(list)
		if snapshot {
			f.Snapshot(json.RawMessage(raw), live.Position{})
		} else {
			f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			pruned := false
			for k, e := range entries {
				if time.Since(e.At) > TTL {
					delete(entries, k)
					pruned = true
				}
			}
			if pruned && started {
				send(false)
			}
		case entry, ok := <-w.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				started = true
				send(true)
				continue
			}
			if entry.Operation() != jetstream.KeyValuePut {
				delete(entries, entry.Key())
			} else {
				var e Entry
				if json.Unmarshal(entry.Value(), &e) == nil && time.Since(e.At) <= TTL {
					entries[entry.Key()] = e
				}
			}
			if started {
				send(false)
			}
		}
	}
}
