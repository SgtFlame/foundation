package data_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	collabimpl "github.com/SgtFlame/foundation/backend/features/kb/services/collabservice/impl"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// A record's markdown field edited in a collaboration room (forge 07l): the
// room seeds from the field and the writer's saves are record.update
// commands as the writer.

type chanConn struct {
	in  chan []byte
	out chan []byte
}

func (c *chanConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case m, ok := <-c.in:
		if !ok {
			return nil, context.Canceled
		}
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *chanConn) Write(_ context.Context, m []byte) error {
	c.out <- m
	return nil
}

func varUint(n uint64) []byte {
	var out []byte
	for n >= 0x80 {
		out = append(out, byte(n)|0x80)
		n >>= 7
	}
	return append(out, byte(n))
}

func control(v any) []byte {
	raw, _ := json.Marshal(v)
	return append(append(varUint(100), varUint(uint64(len(raw)))...), raw...)
}

// controlOf decodes a control message, or nil for another.
func controlOf(m []byte) map[string]any {
	if len(m) < 2 || m[0] != 100 {
		return nil
	}
	n, i := uint64(0), 1
	for shift := 0; ; shift += 7 {
		b := m[i]
		i++
		n |= uint64(b&0x7f) << shift
		if b < 0x80 {
			break
		}
	}
	var out map[string]any
	_ = json.Unmarshal(m[i:i+int(n)], &out)
	return out
}

func (c *chanConn) expect(t *testing.T, what string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-c.out:
			if ctl := controlOf(m); ctl != nil && ok(ctl) {
				return ctl
			}
		case <-timeout:
			t.Fatalf("no %s", what)
			return nil
		}
	}
}

func TestARecordFieldIsEditedInARoom(t *testing.T) {
	f := setup(t)
	data.ContributeCollab(f.reg, func() data.Deps { return *f.deps })
	f.ok("entity.create", "note", map[string]any{"name": "Note", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "body", "type": "markdown", "searchable": true},
	}})
	f.publish("note", 1)
	id := f.ok("record.create", "", map[string]any{"entity": "note", "values": map[string]any{"title": "Plan", "body": "Hello"}}).Target
	f.get("note", id, 0)

	svc := collabimpl.CreateService(collabimpl.WithGracePeriod(time.Minute), collabimpl.WithDocumentKinds(func(kind string) (collabservice.DocumentKind, bool) {
		for _, k := range collabservice.DocumentKinds.All(f.reg) {
			if k.ID == kind {
				return k, true
			}
		}
		return collabservice.DocumentKind{}, false
	}))
	open := func(ref string) collabservice.OpenFileResponse {
		return svc.OpenDoc(collabservice.OpenDocArgs{ServiceArgs: args(), Kind: "record", Ref: json.RawMessage(ref)})
	}
	ref := `{"entity":"note","id":"` + id + `","field":"body"}`
	first, again := open(ref), open(ref)
	if first.Common.Code != 200 || first.FileID == 0 || again.FileID != first.FileID || first.ReadOnly {
		t.Fatalf("OpenDoc: %+v, then %+v", first, again)
	}
	if r := open(`{"entity":"note","id":"` + id + `","field":"title"}`); r.Common.Code != 400 || !strings.Contains(r.Common.Message, "not markdown") {
		t.Errorf("a text field: %+v", r.Common)
	}
	if r := open(`{"entity":"note","id":"999","field":"body"}`); r.Common.Code != 404 {
		t.Errorf("a missing record: %+v", r.Common)
	}
	if r := svc.OpenDoc(collabservice.OpenDocArgs{ServiceArgs: args(), Kind: "nope", Ref: json.RawMessage(ref)}); r.Common.Code != 400 {
		t.Errorf("an unknown kind: %+v", r.Common)
	}

	// The first author is the writer and is seeded from the field
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &chanConn{in: make(chan []byte, 16), out: make(chan []byte, 256)}
	sa := args()
	sa.Ctx = ctx
	go svc.Collaborate(collabservice.CollaborateArgs{ServiceArgs: sa, FileID: first.FileID, Conn: a})
	role := a.expect(t, "the writer's role", func(c map[string]any) bool { return c["op"] == "role" })
	if role["writer"] != true || role["seed"] != "Hello" {
		t.Fatalf("role: %v", role)
	}
	a.in <- append(append(varUint(0), varUint(2)...), append(varUint(3), 1, 2, 3)...) // a Yjs update (opaque to the room)
	a.in <- control(map[string]any{"op": "seeded"})
	b := &chanConn{in: make(chan []byte, 16), out: make(chan []byte, 256)}
	go svc.Collaborate(collabservice.CollaborateArgs{ServiceArgs: sa, FileID: first.FileID, Conn: b})
	b.expect(t, "the second author's role", func(c map[string]any) bool { return c["op"] == "role" && c["writer"] != true })

	// The writer's save is a record.update; both hear it was saved
	a.in <- control(map[string]any{"op": "save", "markdown": "Hello **world**"})
	b.expect(t, "saved", func(c map[string]any) bool { return c["op"] == "saved" && c["code"] == 200.0 })
	r := f.get("note", id, 0)
	var body string
	_ = json.Unmarshal(r["body"], &body)
	for i := 0; body != "Hello **world**" && i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		r = f.get("note", id, 0)
		_ = json.Unmarshal(r["body"], &body)
	}
	if body != "Hello **world**" {
		t.Fatalf("the field holds %q", body)
	}
	// A save of the text the record holds commits nothing
	before := len(f.events(id))
	a.in <- control(map[string]any{"op": "save", "markdown": "Hello **world**"})
	b.expect(t, "saved again", func(c map[string]any) bool { return c["op"] == "saved" && c["code"] == 200.0 })
	if after := len(f.events(id)); after != before {
		t.Errorf("an unchanged save committed %d events", after-before)
	}
}
