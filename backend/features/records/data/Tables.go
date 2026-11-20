package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/nats-io/nats.go/jetstream"
	"gorm.io/gorm"
)

// The relational projection (Records.md › Built in slice 52): an entity
// type whose storage is {projection: "sql"} also has its records in a
// table of the tenant database, which the query planner reads. The table
// is one more projection of the rec stream: a follower per type writes
// each record's row in stream order, and a rebuild replays the stream into
// a new table. Each row keeps the record as the KV projection has it
// (data), so a row is read without the KV store, and per field its value
// as compact JSON (v<n>), its sort token for a field with an order (t<n>)
// and, for a field that sums, its number (n<n>). A list-like field has a
// child table of its items. Queries filter, sort and page in SQL by the
// same tokens the KV indexes use, then check every row read against the
// filter and read rule in Go, so both paths give the same records.

// Streams are what the tables replay and follow (live/project.Projections).
type Streams interface {
	live.Store
	Stream(ctx context.Context, tenant uint64, area live.Area) (jetstream.Stream, error)
	Follow(ctx context.Context, tenant uint64, area live.Area, name string, subjects []string, fn func(live.Event) error) error
}

// Tables keeps the entity types' tables in one database. Make one per
// process with NewTables and put it in Deps.Tables.
type Tables struct {
	db       *gorm.DB
	streams  Streams
	instance string
	locks    sync.Map // tenant/entity → *sync.Mutex: one writer per table here
	schemas  sync.Map // table name and version → *tableSchema
	mu       sync.Mutex
	changed  chan struct{} // closed and replaced after each write
	// CatchUp bounds how long a read waits for its table to apply the
	// type's events committed before it (5 s when zero).
	CatchUp time.Duration
}

// NewTables migrates the tables' state and names the database.
func NewTables(gdb *gorm.DB, streams Streams) (*Tables, error) {
	if err := gdb.AutoMigrate(&records.ProjectedTable{}, &records.DatabaseInstance{}); err != nil {
		return nil, log.Wrapf(err, "cannot migrate the records tables")
	}
	var inst records.DatabaseInstance
	if err := gdb.Attrs(records.DatabaseInstance{ID: fmt.Sprint(features.NewTSID())}).FirstOrCreate(&inst).Error; err != nil {
		return nil, log.Wrapf(err, "cannot name the database")
	}
	return &Tables{db: gdb, streams: streams, instance: inst.ID, changed: make(chan struct{})}, nil
}

// sql reports whether a type's queries read its table.
func (d Deps) sql(e records.EntityType) bool { return d.Tables != nil && e.SQL() }

// Followers' names: one for the definitions and one per type, each named
// after the database (forge 07k question 2).
func (t *Tables) defsFollower() string { return "records.tables." + t.instance }
func (t *Tables) follower(entity string) string {
	return "records.sql." + t.instance + "." + entity
}

func recSubject(tenant uint64, entity string) string {
	return fmt.Sprintf("evt.%d.%s.%s.>", tenant, live.AreaRecords, entity)
}

// Follow starts keeping the principal's tenant's tables: the follower of
// published entity types, and one per type that has a table here.
func (t *Tables) Follow(args features.ServiceArgs) error {
	tenant := args.Principal.TenantID
	subject := live.Target{Area: live.AreaDefinitions, Type: entities.TargetType, ID: "*"}.Subject(tenant, entities.EventPublished)
	err := t.streams.Follow(args.Context(), tenant, live.AreaDefinitions, t.defsFollower(), []string{subject}, func(e live.Event) error {
		return t.published(e)
	})
	if err != nil {
		return err
	}
	var states []records.ProjectedTable
	if err := t.db.Where("tenant_id = ?", tenant).Find(&states).Error; err != nil {
		return log.Wrap(err)
	}
	for _, st := range states {
		if err := t.follow(args.Context(), tenant, st.Entity); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tables) follow(ctx context.Context, tenant uint64, entity string) error {
	return t.streams.Follow(ctx, tenant, live.AreaRecords, t.follower(entity), []string{recSubject(tenant, entity)}, func(e live.Event) error {
		return t.apply(tenant, entity, e)
	})
}

// published makes, migrates, rebuilds or drops a type's table when it is
// published, from its current published version.
func (t *Tables) published(ev live.Event) error {
	e, ok, err := t.entity(ev.TenantID, ev.Target.ID)
	if err != nil {
		return err
	}
	// The projection may not have folded this event yet
	var pub entities.Publication
	if json.Unmarshal(ev.Data, &pub) == nil && (!ok || pub.Version > e.Version) {
		e, ok = pub.Entity, true
		e.ID, e.Version = ev.Target.ID, pub.Version
	}
	if !ok {
		return nil
	}
	if _, err := t.ensure(t.ctx(), ev.TenantID, e); err != nil {
		return err
	}
	if e.SQL() {
		return t.follow(t.ctx(), ev.TenantID, e.ID)
	}
	return nil
}

func (t *Tables) ctx() context.Context {
	if c, ok := t.streams.(interface{ Context() context.Context }); ok {
		return c.Context()
	}
	return context.Background()
}

// entity is a type's current published version.
func (t *Tables) entity(tenant uint64, id string) (records.EntityType, bool, error) {
	var e records.EntityType
	v, ok, err := t.streams.Get(t.ctx(), tenant, entities.PublishedProjector, entities.TargetType+"."+id)
	if err != nil || !ok || v.Value == nil {
		return e, false, err
	}
	if err := json.Unmarshal(v.Value, &e); err != nil {
		return e, false, err
	}
	e.ID = id
	return e, true, nil
}

func (t *Tables) lock(tenant uint64, entity string) func() {
	m, _ := t.locks.LoadOrStore(fmt.Sprintf("%d/%s", tenant, entity), &sync.Mutex{})
	m.(*sync.Mutex).Lock()
	return m.(*sync.Mutex).Unlock
}

func (t *Tables) state(db *gorm.DB, tenant uint64, entity string) (records.ProjectedTable, bool, error) {
	var sts []records.ProjectedTable
	if err := db.Where("tenant_id = ? AND entity = ?", tenant, entity).Limit(1).Find(&sts).Error; err != nil || len(sts) == 0 {
		return records.ProjectedTable{}, false, err
	}
	return sts[0], true, nil
}

// notify wakes the reads waiting for a table to catch up.
func (t *Tables) notify() {
	t.mu.Lock()
	close(t.changed)
	t.changed = make(chan struct{})
	t.mu.Unlock()
}

func (t *Tables) wait() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.changed
}

// tableSchema is a table's columns. Each visible field has a column number
// given once, so a field keeps its columns across versions; Kinds tell
// whether a version's field still fits them.
type tableSchema struct {
	Columns map[string]int    `json:"columns"`         // field id → column number
	Kinds   map[string]string `json:"kinds"`           // field id → what its columns hold
	Known   []string          `json:"known"`           // every field id of the version, hidden ones too
	Lists   map[string]bool   `json:"lists,omitempty"` // field ids with a child table
	Indexes []records.Index   `json:"indexes"`
	Next    int               `json:"next"`
	// set when loaded, from the version it was made for
	fields map[string]records.Field
	name   string
}

// kindOf is what decides a field's columns and their contents.
func kindOf(f records.Field) string {
	v := records.ValueField(f)
	k := map[string]any{"type": v.Type, "scale": v.Scale, "currency": v.Currency}
	if len(v.Options) > 0 {
		ids := []string{}
		for _, o := range v.Options {
			ids = append(ids, o.ID)
		}
		k["options"] = ids
	}
	if v.Relation != nil {
		k["many"] = v.Relation.Many
	}
	if v.Items != nil {
		k["items"] = kindOf(*v.Items)
	}
	b, _ := json.Marshal(k)
	return string(b)
}

// schemaFor is the columns of version e, keeping old's column numbers.
func schemaFor(e records.EntityType, old *tableSchema) *tableSchema {
	s := &tableSchema{Columns: map[string]int{}, Kinds: map[string]string{}, Lists: map[string]bool{}, Indexes: e.Indexes, Next: 1}
	if old != nil {
		s.Next = old.Next
	}
	for _, f := range e.Fields {
		s.Known = append(s.Known, f.ID)
		if f.Hidden {
			continue
		}
		n, ok := 0, false
		if old != nil {
			n, ok = old.Columns[f.ID]
		}
		if !ok {
			n = s.Next
			s.Next++
		}
		s.Columns[f.ID], s.Kinds[f.ID] = n, kindOf(f)
		if _, many := records.ItemOf(f); many {
			s.Lists[f.ID] = true
		}
	}
	s.load(e)
	return s
}

func (s *tableSchema) load(e records.EntityType) {
	s.fields = map[string]records.Field{}
	for _, f := range e.Fields {
		if _, ok := s.Columns[f.ID]; ok {
			s.fields[f.ID] = f
		}
	}
}

// migrates reports whether a table built for old can follow new by adding
// columns: no field it has columns for holds anything else now, and no
// field new adds columns for had values before (a restored field).
func (old *tableSchema) migrates(new *tableSchema) bool {
	for id, k := range old.Kinds {
		if nk, ok := new.Kinds[id]; ok && nk != k {
			return false
		}
	}
	for id := range new.Kinds {
		if _, had := old.Kinds[id]; !had && slices.Contains(old.Known, id) {
			return false
		}
	}
	return true
}

// col is a field's columns. A meta field's are the row's own.
type col struct {
	v, t, n string // value, sort token, number; empty when it has none
	child   string // a list-like field's child table
	idCol   bool   // v holds the id as text, not JSON
	numeric string // n's SQL type: real or integer
}

var metaCols = map[string]col{
	MetaID:        {v: "id", t: "tid", idCol: true},
	MetaCreatedAt: {v: "vca", t: "tca"},
	MetaUpdatedAt: {v: "vua", t: "tua"},
}

func (s *tableSchema) col(id string) (col, bool) {
	if c, ok := metaCols[id]; ok {
		return c, true
	}
	n, ok := s.Columns[id]
	if !ok {
		return col{}, false
	}
	f := s.fields[id]
	c := col{v: fmt.Sprintf("v%d", n)}
	if records.Sortable(f) {
		c.t = fmt.Sprintf("t%d", n)
	}
	switch v := records.ValueField(f); {
	case v.Type == "number":
		c.n, c.numeric = fmt.Sprintf("n%d", n), "real"
	case v.Type == "decimal" || v.Type == "money" && v.Currency != "":
		c.n, c.numeric = fmt.Sprintf("n%d", n), "integer"
	}
	if s.Lists[id] {
		c.child = fmt.Sprintf("%s_l%d", s.name, n)
	}
	return c, true
}

// ids are the field ids with columns, in column order.
func (s *tableSchema) ids() []string {
	out := make([]string, 0, len(s.Columns))
	for id := range s.Columns {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b string) int { return s.Columns[a] - s.Columns[b] })
	return out
}

var (
	typString = reflect.TypeFor[string]()
	typText   = reflect.TypeFor[*string]()
	typInt    = reflect.TypeFor[int64]()
	typReal   = reflect.TypeFor[*float64]()
	typNum    = reflect.TypeFor[*int64]()
)

// model is the table's GORM model, built from its columns, which
// AutoMigrate makes or extends; indexes follow the declared ones.
func (s *tableSchema) model() any {
	idx := map[string][]string{} // column → index tags
	idx["ord"] = []string{fmt.Sprintf("index:%s_ord", s.name)}
	for k, x := range s.Indexes {
		p := 1
		for _, id := range x.Fields {
			c, ok := s.col(id)
			if !ok || c.t == "" || c.child != "" {
				break
			}
			idx[c.t] = append(idx[c.t], fmt.Sprintf("index:%s_x%d,priority:%d", s.name, k, p))
			p++
		}
		if p > 1 {
			idx["ord"] = append(idx["ord"], fmt.Sprintf("index:%s_x%d,priority:%d", s.name, k, p))
		}
	}
	tag := func(column, typ string) reflect.StructTag {
		parts := append([]string{"column:" + column, "type:" + typ}, idx[column]...)
		return reflect.StructTag(`gorm:"` + strings.Join(parts, ";") + `"`)
	}
	fields := []reflect.StructField{
		{Name: "ID", Type: typString, Tag: `gorm:"column:id;type:text;primaryKey"`},
		{Name: "Ord", Type: typString, Tag: tag("ord", "text")},
		{Name: "Seq", Type: typInt, Tag: `gorm:"column:seq"`},
		{Name: "Data", Type: typString, Tag: `gorm:"column:data;type:text"`},
		{Name: "Tid", Type: typText, Tag: tag("tid", "text")},
		{Name: "Vca", Type: typText, Tag: tag("vca", "text")},
		{Name: "Tca", Type: typText, Tag: tag("tca", "text")},
		{Name: "Vua", Type: typText, Tag: tag("vua", "text")},
		{Name: "Tua", Type: typText, Tag: tag("tua", "text")},
	}
	for _, id := range s.ids() {
		c, _ := s.col(id)
		n := s.Columns[id]
		fields = append(fields, reflect.StructField{Name: fmt.Sprintf("V%d", n), Type: typText, Tag: tag(c.v, "text")})
		if c.t != "" {
			fields = append(fields, reflect.StructField{Name: fmt.Sprintf("T%d", n), Type: typText, Tag: tag(c.t, "text")})
		}
		if c.numeric == "real" {
			fields = append(fields, reflect.StructField{Name: fmt.Sprintf("N%d", n), Type: typReal, Tag: tag(c.n, "real")})
		} else if c.numeric != "" {
			fields = append(fields, reflect.StructField{Name: fmt.Sprintf("N%d", n), Type: typNum, Tag: tag(c.n, "integer")})
		}
	}
	return reflect.New(reflect.StructOf(fields)).Interface()
}

// childModel is a list-like field's table: one row per item, in order.
func childModel(name string) any {
	return reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "Rid", Type: typString, Tag: reflect.StructTag(`gorm:"column:rid;type:text;not null;index:` + name + `_rid"`)},
		{Name: "Pos", Type: typInt, Tag: `gorm:"column:pos"`},
		{Name: "V", Type: typText, Tag: reflect.StructTag(`gorm:"column:v;type:text;index:` + name + `_v;index:` + name + `_t,priority:2"`)},
		{Name: "T", Type: typText, Tag: reflect.StructTag(`gorm:"column:t;type:text;index:` + name + `_t,priority:1"`)},
	})).Interface()
}

// migrate makes or extends the table and its child tables.
func (s *tableSchema) migrate(tx *gorm.DB) error {
	if err := tx.Table(s.name).AutoMigrate(s.model()); err != nil {
		return log.Wrapf(err, "cannot migrate table %s", s.name)
	}
	for _, id := range s.ids() {
		if c, _ := s.col(id); c.child != "" {
			if err := tx.Table(c.child).AutoMigrate(childModel(c.child)); err != nil {
				return log.Wrapf(err, "cannot migrate table %s", c.child)
			}
		}
	}
	return nil
}

// drop removes a table and its child tables.
func dropTable(tx *gorm.DB, name string, s *tableSchema) error {
	if s != nil {
		for _, id := range s.ids() {
			if c, _ := s.col(id); c.child != "" {
				if err := tx.Migrator().DropTable(c.child); err != nil {
					return log.Wrap(err)
				}
			}
		}
	}
	return log.Wrap(tx.Migrator().DropTable(name))
}

// schema is a state's columns, for the version e it was built for.
func (t *Tables) schema(st records.ProjectedTable, e records.EntityType) (*tableSchema, error) {
	key := fmt.Sprintf("%s@%d", st.Name, st.Version)
	if s, ok := t.schemas.Load(key); ok {
		return s.(*tableSchema), nil
	}
	s, err := parseSchema(st)
	if err != nil {
		return nil, err
	}
	s.load(e)
	t.schemas.Store(key, s)
	return s, nil
}

// parseSchema is a state's columns without their fields: enough to extend
// or drop the table.
func parseSchema(st records.ProjectedTable) (*tableSchema, error) {
	s := &tableSchema{}
	if err := json.Unmarshal([]byte(st.Schema), s); err != nil {
		return nil, log.Wrapf(err, "cannot read the columns of %s", st.Name)
	}
	s.name = st.Name
	return s, nil
}

// ensure brings a type's table to its published version e: it makes it,
// adds the columns a new version needs, rebuilds it from the stream when
// a version changes what columns hold, and drops it when the type no
// longer has one. It returns the table's state.
func (t *Tables) ensure(ctx context.Context, tenant uint64, e records.EntityType) (records.ProjectedTable, error) {
	st, found, err := t.state(t.db, tenant, e.ID)
	if err != nil {
		return st, log.Wrap(err)
	}
	if found && e.SQL() && st.Version == e.Version {
		return st, nil
	}
	if !found && !e.SQL() {
		return st, nil
	}
	defer t.lock(tenant, e.ID)()
	if st, found, err = t.state(t.db, tenant, e.ID); err != nil {
		return st, log.Wrap(err)
	}
	if found && e.SQL() && st.Version >= e.Version {
		return st, nil
	}
	var old *tableSchema
	if found {
		if old, err = parseSchema(st); err != nil {
			return st, err
		}
	}
	if !e.SQL() {
		err := t.db.Transaction(func(tx *gorm.DB) error {
			if err := dropTable(tx, st.Name, old); err != nil {
				return err
			}
			return tx.Delete(&st).Error
		})
		log.With("entity", e.ID).Info(ctx, "dropped table %s: the type is no longer a large table", st.Name)
		return records.ProjectedTable{}, log.Wrap(err)
	}
	s := schemaFor(e, old)
	if found && old.migrates(s) {
		s.name = st.Name
		raw, _ := json.Marshal(s)
		st.Version, st.Schema = e.Version, string(raw)
		err := t.db.Transaction(func(tx *gorm.DB) error {
			if err := s.migrate(tx); err != nil {
				return err
			}
			return tx.Save(&st).Error
		})
		return st, log.Wrap(err)
	}
	return t.rebuild(ctx, tenant, e, s, st, found, old)
}

// Rebuild drops a type's table and builds it again from the stream.
func (t *Tables) Rebuild(args features.ServiceArgs, entity string) error {
	tenant := args.Principal.TenantID
	e, ok, err := t.entity(tenant, entity)
	if err != nil || !ok || !e.SQL() {
		return err
	}
	defer t.lock(tenant, entity)()
	st, found, err := t.state(t.db, tenant, entity)
	if err != nil {
		return log.Wrap(err)
	}
	var old *tableSchema
	if found {
		if old, err = parseSchema(st); err != nil {
			return err
		}
	}
	_, err = t.rebuild(args.Context(), tenant, e, schemaFor(e, old), st, found, old)
	return err
}

// rebuild replays the type's events into a new table, then puts it in
// place of the old one in one transaction, so a read sees one or the other.
func (t *Tables) rebuild(ctx context.Context, tenant uint64, e records.EntityType, s *tableSchema, st records.ProjectedTable, found bool, old *tableSchema) (records.ProjectedTable, error) {
	start := time.Now()
	recs, last, err := t.replay(ctx, tenant, e.ID)
	if err != nil {
		return st, err
	}
	s.name = fmt.Sprintf("rec_%d_%d", tenant, features.NewTSID())
	s.load(e)
	raw, _ := json.Marshal(s)
	next := records.ProjectedTable{TenantID: tenant, Entity: e.ID, Name: s.name, Version: e.Version, Schema: string(raw), Seq: last}
	ids := make([]string, 0, len(recs))
	for id := range recs {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, compareIDs)
	err = t.db.Transaction(func(tx *gorm.DB) error {
		if err := s.migrate(tx); err != nil {
			return err
		}
		w := s.writer()
		for i := 0; i < len(ids); i += 200 {
			batch := []Record{}
			for _, id := range ids[i:min(i+200, len(ids))] {
				batch = append(batch, recs[id])
			}
			if err := w.insert(tx, batch); err != nil {
				return err
			}
		}
		if found {
			if err := dropTable(tx, st.Name, old); err != nil {
				return err
			}
		}
		return tx.Save(&next).Error
	})
	if err != nil {
		return st, log.Wrapf(err, "cannot rebuild the table of %s", e.ID)
	}
	t.schemas.Store(fmt.Sprintf("%s@%d", s.name, e.Version), s)
	t.notify()
	log.With("entity", e.ID).With("table", s.name).Info(ctx, "built the table of %s: %d records to event %d in %v", e.ID, len(ids), last, time.Since(start).Round(time.Millisecond))
	return next, nil
}

// replay folds every event of the type committed so far: the records as
// the KV projection has them, and the last event's sequence.
func (t *Tables) replay(ctx context.Context, tenant uint64, entity string) (map[string]Record, uint64, error) {
	recs := map[string]Record{}
	last, err := t.lastSeq(ctx, tenant, entity)
	if err != nil || last == 0 {
		return recs, last, err
	}
	stream, err := t.streams.Stream(ctx, tenant, live.AreaRecords)
	if err != nil {
		return nil, 0, err
	}
	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{FilterSubjects: []string{recSubject(tenant, entity)}})
	if err != nil {
		return nil, 0, log.Wrapf(err, "cannot replay the records of %s", entity)
	}
	for {
		batch, err := cons.Fetch(1000, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			return nil, 0, log.Wrapf(err, "cannot replay the records of %s", entity)
		}
		n := 0
		for msg := range batch.Messages() {
			n++
			meta, err := msg.Metadata()
			if err != nil {
				return nil, 0, log.Wrap(err)
			}
			var e live.Event
			if err := json.Unmarshal(msg.Data(), &e); err != nil {
				continue // as the projector does, an event it cannot read is skipped
			}
			e.Stream, e.Seq = meta.Stream, meta.Sequence.Stream
			r, err := Apply(recs[e.Target.ID], e)
			if err != nil {
				continue
			}
			if r == nil {
				delete(recs, e.Target.ID)
			} else {
				recs[e.Target.ID] = r
			}
			if e.Seq >= last {
				return recs, last, nil
			}
		}
		if err := batch.Error(); err != nil && !errors.Is(err, jetstream.ErrNoMessages) {
			return nil, 0, log.Wrap(err)
		}
		if n == 0 {
			return nil, 0, log.Code("replay_short").Errorf("the records of %s ended before event %d", entity, last)
		}
	}
}

// lastSeq is the sequence of the type's last event; 0 when it has none.
func (t *Tables) lastSeq(ctx context.Context, tenant uint64, entity string) (uint64, error) {
	stream, err := t.streams.Stream(ctx, tenant, live.AreaRecords)
	if err != nil {
		return 0, err
	}
	msg, err := stream.GetLastMsgForSubject(ctx, recSubject(tenant, entity))
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, log.Wrap(err)
	}
	return msg.Sequence, nil
}

// apply writes one event's change to its record's row: the follower of
// the type. An event at or below the table's Seq is already in it.
func (t *Tables) apply(tenant uint64, entity string, ev live.Event) error {
	e, ok, err := t.entity(tenant, entity)
	if err != nil {
		return err
	}
	if !ok || !e.SQL() {
		return nil
	}
	if _, err := t.ensure(t.ctx(), tenant, e); err != nil {
		return err
	}
	defer t.lock(tenant, entity)()
	err = t.db.Transaction(func(tx *gorm.DB) error {
		st, found, err := t.state(tx, tenant, entity)
		if err != nil || !found || ev.Seq <= st.Seq {
			return err
		}
		s, err := t.schema(st, e)
		if err != nil {
			return err
		}
		var data []string
		if err := tx.Raw(fmt.Sprintf(`SELECT data FROM %q WHERE id = ?`, st.Name), ev.Target.ID).Scan(&data).Error; err != nil {
			return err
		}
		var r Record
		if len(data) > 0 {
			_ = json.Unmarshal([]byte(data[0]), &r)
		}
		if r, err = Apply(r, ev); err != nil {
			return err
		}
		w := s.writer()
		if err := w.remove(tx, ev.Target.ID); err != nil {
			return err
		}
		if r != nil {
			if err := w.insert(tx, []Record{r}); err != nil {
				return err
			}
		}
		return tx.Model(&st).Update("seq", ev.Seq).Error
	})
	if err != nil {
		return log.Wrapf(err, "cannot write record %s of %s to its table", ev.Target.ID, entity)
	}
	t.notify()
	return nil
}

// writer writes rows of one table.
type writer struct {
	s       *tableSchema
	columns []string
	values  []func(r Record, id string) any
	lists   []listCol
}

type listCol struct {
	id    string
	child string
	item  records.Field
	sort  bool
}

func (s *tableSchema) writer() *writer {
	w := &writer{s: s}
	add := func(column string, fn func(r Record, id string) any) {
		w.columns = append(w.columns, column)
		w.values = append(w.values, fn)
	}
	add("id", func(_ Record, id string) any { return id })
	add("ord", func(_ Record, id string) any { return ordOf(id) })
	add("seq", func(r Record, _ string) any { return int64(r.Seq()) })
	add("data", func(r Record, _ string) any { b, _ := json.Marshal(r); return string(b) })
	add("tid", func(_ Record, id string) any { tk, _ := records.SortToken(metaFields[MetaID], raw(id)); return tk })
	for _, m := range []struct{ id, v, t string }{{MetaCreatedAt, "vca", "tca"}, {MetaUpdatedAt, "vua", "tua"}} {
		f := metaFields[m.id]
		add(m.v, func(r Record, _ string) any { return jsonText(r[m.id]) })
		add(m.t, func(r Record, _ string) any { return sortToken(f, r[m.id]) })
	}
	for _, id := range s.ids() {
		c, _ := s.col(id)
		f := s.fields[id]
		add(c.v, func(r Record, _ string) any { return jsonText(r[id]) })
		if c.t != "" {
			add(c.t, func(r Record, _ string) any { return sortToken(f, r[id]) })
		}
		if c.n != "" {
			add(c.n, func(r Record, _ string) any { return numberOf(f, r[id]) })
		}
		if c.child != "" {
			item, _ := records.ItemOf(f)
			w.lists = append(w.lists, listCol{id: id, child: c.child, item: item, sort: records.Sortable(item)})
		}
	}
	return w
}

// insert adds rows for records that have none.
func (w *writer) insert(tx *gorm.DB, rs []Record) error {
	if len(rs) == 0 {
		return nil
	}
	quoted := make([]string, len(w.columns))
	for i, c := range w.columns {
		quoted[i] = fmt.Sprintf("%q", c)
	}
	one := "(" + strings.TrimSuffix(strings.Repeat("?,", len(w.columns)), ",") + ")"
	var rows []string
	var args []any
	type item struct {
		rid  string
		pos  int
		v, t any
	}
	children := map[string][]item{}
	for _, r := range rs {
		var id string
		_ = json.Unmarshal(r[MetaID], &id)
		rows = append(rows, one)
		for _, fn := range w.values {
			args = append(args, fn(r, id))
		}
		for _, l := range w.lists {
			var items []json.RawMessage
			_ = json.Unmarshal(r[l.id], &items)
			for i, it := range items {
				var tk any
				if l.sort {
					tk = sortToken(l.item, it)
				}
				children[l.child] = append(children[l.child], item{rid: id, pos: i, v: jsonText(it), t: tk})
			}
		}
	}
	q := fmt.Sprintf("INSERT INTO %q (%s) VALUES %s", w.s.name, strings.Join(quoted, ","), strings.Join(rows, ","))
	if err := tx.Exec(q, args...).Error; err != nil {
		return err
	}
	for child, items := range children {
		for i := 0; i < len(items); i += 500 {
			var rows []string
			var args []any
			for _, it := range items[i:min(i+500, len(items))] {
				rows = append(rows, "(?,?,?,?)")
				args = append(args, it.rid, it.pos, it.v, it.t)
			}
			if err := tx.Exec(fmt.Sprintf(`INSERT INTO %q (rid, pos, v, t) VALUES %s`, child, strings.Join(rows, ",")), args...).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// remove deletes a record's row and its items.
func (w *writer) remove(tx *gorm.DB, id string) error {
	for _, l := range w.lists {
		if err := tx.Exec(fmt.Sprintf(`DELETE FROM %q WHERE rid = ?`, l.child), id).Error; err != nil {
			return err
		}
	}
	return tx.Exec(fmt.Sprintf(`DELETE FROM %q WHERE id = ?`, w.s.name), id).Error
}

// ordOf orders record ids as compareIDs does: by length, then as text.
func ordOf(id string) string { return fmt.Sprintf("%04d%s", len(id), id) }

// jsonText is a value as compact JSON, as same compares values; nil for none.
func jsonText(v json.RawMessage) any {
	if records.IsNull(v) {
		return nil
	}
	var b bytes.Buffer
	if json.Compact(&b, v) != nil {
		return string(v)
	}
	return b.String()
}

// sortToken is the token a sort orders a value by (sorter.row).
func sortToken(f records.Field, v json.RawMessage) string {
	t, err := records.SortToken(f, v)
	if err != nil {
		return badToken
	}
	return t
}

// badToken is the token of a value its field no longer fits: it sorts last.
var badToken = records.Token([]byte{0xFF})

// numberOf is what a field sums: a number as itself, a decimal in units of
// its scale; nil for none or a value that does not fit.
func numberOf(f records.Field, v json.RawMessage) any {
	if records.IsNull(v) {
		return nil
	}
	vf := records.ValueField(f)
	if vf.Type == "number" {
		var n float64
		if json.Unmarshal(v, &n) != nil {
			return nil
		}
		return n
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return nil
	}
	q, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil
	}
	q.Mul(q, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scaleOf(vf))), nil)))
	if !q.IsInt() || !q.Num().IsInt64() {
		return nil
	}
	return q.Num().Int64()
}

// scaleOf is a decimal field's scale, as records.CheckValue keeps it.
func scaleOf(f records.Field) int {
	if f.Scale == nil {
		return 2
	}
	return *f.Scale
}
