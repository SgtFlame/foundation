package activity

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/records"
	recordsdata "github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Deps are what the recorder works with.
type Deps struct {
	Projections *project.Projections
	Store       *Store
	// Config resolves the activity section as each person sees it; nil
	// uses the defaults
	Config config.Reader
	// Bus names commands by their titles, an installed app's too; nil
	// names them by their types
	Bus *bus.Bus
}

// The followers of the event streams: one shared durable consumer per
// tenant and area, so each event is recorded once whichever process
// takes it.
const (
	RecordsFollower     = "activity.rec"
	DefinitionsFollower = "activity.def"
)

// Recorder records people's gestures. It is a ui.Recorder, and it follows
// every tenant's record and definition events.
type Recorder struct {
	d func() Deps
}

// NewRecorder is a recorder over deps, bound late as serve.go's are.
func NewRecorder(d func() Deps) *Recorder { return &Recorder{d: d} }

var _ ui.Recorder = (*Recorder)(nil)

// Person reports whether a principal's gestures are recorded, and whose
// they are: a user's only, never an agent's, an app's or a service's.
func Person(p features.Principal) (uint64, bool) {
	if p.Kind != features.PrincipalUser || p.UserID == 0 || p.TenantID == 0 {
		return 0, false
	}
	return p.UserID, true
}

// personOf is the person an event was committed by, when it is one acting
// by hand: not an agent, an app, a service, nor the chat for them.
func personOf(e live.Event) (uint64, bool) {
	if e.UserID == 0 || e.AgentID != 0 || e.Actor != "" {
		return 0, false
	}
	return e.UserID, true
}

// SettingsOf is the activity section as a person resolves it.
func SettingsOf(ctx context.Context, d Deps, tenant, user uint64) Settings {
	if d.Config == nil {
		return Defaults
	}
	s, err := config.Read[Settings](d.Config, features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: tenant}}, SectionID)
	if err != nil {
		return Defaults
	}
	return s
}

// write records an entry for a person, unless they turned recording off,
// in which case what was kept is cleared instead.
func (r *Recorder) write(ctx context.Context, tenant, user uint64, e Entry) error {
	d := r.d()
	if d.Store == nil {
		return nil
	}
	set := SettingsOf(ctx, d, tenant, user)
	if !set.Record {
		return d.Store.Purge(ctx, tenant, user)
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	return d.Store.Append(ctx, tenant, user, e, set)
}

// Gesture records a person's event on a page (a ui.Recorder): in the
// background, since the session holds its lock.
func (r *Recorder) Gesture(args features.ServiceArgs, g ui.Gesture) {
	user, ok := Person(args.Principal)
	if !ok {
		return
	}
	e := Entry{ID: "ui-" + g.Request, Kind: KindUI, Request: g.Request, Time: g.Time, App: g.App, Page: g.Page, Title: g.Title,
		Handler: g.Handler, Event: g.Event, Path: g.Path, Node: g.Node, Label: g.Label}
	if g.Subject != nil {
		e.Subject = &Thing{Type: g.Subject.Type, ID: g.Subject.ID, Label: g.Subject.Label, Details: g.Subject.Details}
	}
	e.Summary = uiSummary(e)
	tenant := args.Principal.TenantID
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(args.Context()), 10*time.Second)
		defer cancel()
		if err := r.write(ctx, tenant, user, e); err != nil {
			log.Warn(ctx, "activity: cannot record a click of user %d: %v", user, err)
		}
	}()
}

// Focus records that a person focused a tab showing subject. id, when
// given, makes a resent focus a duplicate.
func (r *Recorder) Focus(args features.ServiceArgs, id string, subject Thing) error {
	user, ok := Person(args.Principal)
	if !ok {
		return nil
	}
	if id == "" {
		id = fmt.Sprintf("focus-%d", features.NewTSID())
	}
	e := Entry{ID: id, Kind: KindFocus, Request: id, Subject: &subject, Summary: focusSummary(subject)}
	return r.write(args.Context(), args.Principal.TenantID, user, e)
}

// Start follows every tenant's record and definition events, as the
// tenants come, until ctx ends.
func (r *Recorder) Start(ctx context.Context, tenants func() ([]uint64, error)) {
	var mu sync.Mutex
	served := map[uint64]bool{}
	serve := func(tenant uint64) {
		mu.Lock()
		defer mu.Unlock()
		if served[tenant] {
			return
		}
		if err := r.Serve(ctx, tenant); err != nil {
			log.Error(ctx, err, "activity: cannot follow tenant %d", tenant)
			return
		}
		served[tenant] = true
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		ids, err := tenants()
		if err != nil && ctx.Err() == nil {
			log.Warn(ctx, "activity: cannot list the tenants: %v", err)
		}
		for _, t := range ids {
			serve(t)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Serve follows one tenant's record and definition events from now on:
// what came before a follower was first made is not anyone's recent
// activity.
func (r *Recorder) Serve(ctx context.Context, tenant uint64) error {
	p := r.d().Projections
	err := p.FollowNew(ctx, tenant, live.AreaRecords, RecordsFollower, []string{live.StreamSubjects(tenant, live.AreaRecords)}, func(e live.Event) error {
		return r.recordEvent(ctx, tenant, e)
	})
	if err != nil {
		return err
	}
	return p.FollowNew(ctx, tenant, live.AreaDefinitions, DefinitionsFollower, []string{live.StreamSubjects(tenant, live.AreaDefinitions)}, func(e live.Event) error {
		return r.opEvent(ctx, tenant, e)
	})
}

// recordEvent records a record event a person's command committed.
func (r *Recorder) recordEvent(ctx context.Context, tenant uint64, e live.Event) error {
	user, ok := personOf(e)
	if !ok || strings.HasPrefix(e.Target.Type, "_") {
		return nil // a counter's event rides in a create's batch
	}
	d := r.d()
	x := Entry{ID: fmt.Sprintf("evt-%s-%d", e.Stream, e.Seq), Kind: KindCommand, Request: e.RequestID, Time: e.Time, App: e.App,
		Command: e.Command, Entity: e.Target.Type, Target: e.Target.ID, Change: e.Type}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: tenant}}
	x.CommandTitle = r.commandTitle(args, d, e.Command)
	typ, hasType := publishedType(ctx, d, tenant, e.Target.Type)
	x.EntityName = cmp.Or(typ.Name, e.Target.Type)
	var ch recordsdata.Change
	_ = json.Unmarshal(e.Data, &ch)
	values := ch.Values
	if ch.Field != "" && len(ch.Items) > 0 {
		// addedTo, removedFrom: the field and its items
		raw, _ := json.Marshal(ch.Items)
		values = map[string]json.RawMessage{ch.Field: raw}
	}
	if hasType {
		x.Fields = fieldsOf(ctx, d, tenant, typ, values)
		x.RecordTitle = recordTitle(ctx, d, tenant, typ, e.Target.ID, ch)
	} else {
		for id := range values {
			x.Fields = append(x.Fields, Field{ID: id, Name: id, Omitted: true})
		}
		sortFields(x.Fields)
	}
	x.Summary = commandSummary(x)
	return r.write(ctx, tenant, user, x)
}

// opEvent records a person's definition change: an op's first event.
func (r *Recorder) opEvent(ctx context.Context, tenant uint64, e live.Event) error {
	user, ok := personOf(e)
	if !ok || e.Op == nil {
		return nil
	}
	d := r.d()
	x := Entry{ID: fmt.Sprintf("evt-%s-%d", e.Stream, e.Seq), Kind: KindOp, Request: e.RequestID, Time: e.Time, App: cmp.Or(e.Op.App, e.App),
		Command: e.Op.Command, Target: e.Op.Target, Entity: e.Target.Type}
	if d.Projections != nil {
		x.Summary = ops.SummaryOf(d.Projections.Registry(), e)
	}
	x.Summary = cmp.Or(x.Summary, e.Op.Command+" ("+e.Op.Target+")")
	return r.write(ctx, tenant, user, x)
}

// commandTitle is a command's title, as the person's tenant has it.
func (r *Recorder) commandTitle(args features.ServiceArgs, d Deps, typ string) string {
	if typ == "" {
		return ""
	}
	if d.Bus != nil {
		if c, ok := d.Bus.CommandFor(args, typ); ok && c.Title != "" {
			return c.Title
		}
	}
	return humanize(typ)
}

// humanize names a command by its type: mail.thread.label → Thread label.
func humanize(typ string) string {
	parts := strings.Split(typ, ".")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	var words []string
	for _, p := range parts {
		var w strings.Builder
		for i, c := range p {
			if i > 0 && unicode.IsUpper(c) {
				w.WriteByte(' ')
			}
			w.WriteRune(unicode.ToLower(c))
		}
		words = append(words, w.String())
	}
	s := strings.Join(words, " ")
	if s == "" {
		return typ
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + string(r[1:])
}

// publishedType is an entity type's last published version.
func publishedType(ctx context.Context, d Deps, tenant uint64, id string) (records.EntityType, bool) {
	var t records.EntityType
	if d.Projections == nil {
		return t, false
	}
	v, ok, err := d.Projections.Get(ctx, tenant, entities.PublishedProjector, entities.TargetType+"."+id)
	if err != nil || !ok || json.Unmarshal(v.Value, &t) != nil {
		return t, false
	}
	return t, true
}

// projected is a record as the records projection holds it.
func projected(ctx context.Context, d Deps, tenant uint64, entity, id string) recordsdata.Record {
	if d.Projections == nil {
		return nil
	}
	v, ok, err := d.Projections.Get(ctx, tenant, recordsdata.Projector, entity+"."+id)
	var rec recordsdata.Record
	if err != nil || !ok || json.Unmarshal(v.Value, &rec) != nil {
		return nil
	}
	return rec
}

// recordTitle is the record's title: from the event's values, its old
// values (a delete), else the record as projected.
func recordTitle(ctx context.Context, d Deps, tenant uint64, t records.EntityType, id string, ch recordsdata.Change) string {
	if t.TitleField == "" {
		return ""
	}
	for _, vals := range []map[string]json.RawMessage{ch.Values, ch.Old} {
		if raw, ok := vals[t.TitleField]; ok {
			if s := plain(raw); s != "" {
				return s
			}
		}
	}
	if rec := projected(ctx, d, tenant, t.ID, id); rec != nil {
		return plain(rec[t.TitleField])
	}
	return ""
}

// plain is a value as short text: a string, or a number or bool as
// written; "" for anything else.
func plain(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch v := v.(type) {
	case string:
		return cut(v, 80)
	case float64, bool:
		return string(raw)
	}
	return ""
}

// cut shortens text to n characters, on one line.
func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// shortText is how long a text value may be and still be copied.
const shortText = 80

// fieldsOf are the changed fields with the values the command holds, by
// their names; content is never copied (forge 15c: a reply records that a
// draft was sent, with its subject, not its body).
func fieldsOf(ctx context.Context, d Deps, tenant uint64, t records.EntityType, values map[string]json.RawMessage) []Field {
	var out []Field
	for id, raw := range values {
		f, ok := fieldByID(t, id)
		if !ok {
			continue // computed and metadata values are the decision's, not the command's
		}
		x := Field{ID: id, Name: f.Name}
		if v, keep := valueOf(ctx, d, tenant, f, raw); keep {
			x.Value = v
		} else {
			x.Omitted = true
		}
		out = append(out, x)
	}
	sortFields(out)
	return out
}

func fieldByID(t records.EntityType, id string) (records.Field, bool) {
	for _, f := range t.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return records.Field{}, false
}

// valueOf is a field's value as the history copies it: choices by their
// names, related records by their titles, short text as is; keep is false
// for content (long or formatted text, files, JSON, nested values).
func valueOf(ctx context.Context, d Deps, tenant uint64, f records.Field, raw json.RawMessage) (any, bool) {
	if string(raw) == "null" {
		return nil, true
	}
	switch f.Type {
	case "text":
		var s string
		if json.Unmarshal(raw, &s) != nil || len([]rune(s)) > shortText || strings.Contains(s, "\n") {
			return nil, false
		}
		return s, true
	case "number", "decimal", "money", "bool", "date", "dateTime", "duration", "user", "tags":
		var v any
		_ = json.Unmarshal(raw, &v)
		return v, true
	case "enum", "multiEnum":
		var ids []string
		var one string
		if json.Unmarshal(raw, &one) == nil {
			ids = []string{one}
		} else if json.Unmarshal(raw, &ids) != nil {
			return nil, false
		}
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			name := id
			for _, o := range f.Options {
				if o.ID == id {
					name = cmp.Or(o.Name, id)
				}
			}
			names = append(names, name)
		}
		if f.Type == "enum" && len(names) == 1 {
			return names[0], true
		}
		return names, true
	case "relation":
		var ids []string
		var one string
		if json.Unmarshal(raw, &one) == nil {
			ids = []string{one}
		} else if json.Unmarshal(raw, &ids) != nil {
			return nil, false
		}
		if f.Relation == nil || f.Relation.Entity == "user" {
			return ids, true
		}
		rel, ok := publishedType(ctx, d, tenant, f.Relation.Entity)
		titles := make([]string, 0, len(ids))
		for i, id := range ids {
			if i == 10 {
				titles = append(titles, fmt.Sprintf("and %d more", len(ids)-10))
				break
			}
			title := id
			if ok && rel.TitleField != "" {
				if rec := projected(ctx, d, tenant, rel.ID, id); rec != nil {
					title = cmp.Or(plain(rec[rel.TitleField]), id)
				}
			}
			titles = append(titles, title)
		}
		if one != "" {
			return titles[0], true
		}
		return titles, true
	}
	return nil, false
}
