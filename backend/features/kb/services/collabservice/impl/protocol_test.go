package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

func TestEncodingRoundTrip(t *testing.T) {
	for _, n := range []uint64{0, 1, 127, 128, 300, 1 << 40} {
		d := &decoder{buf: (&encoder{}).uint(n).buf}
		if got := d.uint(); got != n || d.err != nil {
			t.Errorf("uint %d: got %d, %v", n, got, d.err)
		}
	}

	entries := []awarenessEntry{{ClientID: 1 << 31, Clock: 7, State: `{"user":{"name":"a"}}`}, {ClientID: 2, Clock: 1, State: "null"}}
	got, err := decodeAwareness(encodeAwareness(entries))
	if err != nil || len(got) != 2 || got[0] != entries[0] || got[1] != entries[1] {
		t.Errorf("awareness: got %v, %v", got, err)
	}

	if _, err := decodeAwareness([]byte{1, 5}); err == nil {
		t.Error("truncated awareness decoded")
	}
}

// fakeConn feeds messages to the service and collects what it writes.
type fakeConn struct {
	in  chan []byte
	out chan []byte
}

func newFakeConn() *fakeConn {
	return &fakeConn{in: make(chan []byte, 16), out: make(chan []byte, 64)}
}

func (f *fakeConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case msg, ok := <-f.in:
		if !ok {
			return nil, errors.New("closed")
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeConn) Write(_ context.Context, msg []byte) error {
	f.out <- msg
	return nil
}

// expect reads messages until one matches, failing after a timeout.
func (f *fakeConn) expect(t *testing.T, what string, match func(msgType uint64, d *decoder) bool) {
	t.Helper()
	timeout := time.After(time.Second)
	for {
		select {
		case msg := <-f.out:
			d := &decoder{buf: msg}
			if match(d.uint(), d) {
				return
			}
		case <-timeout:
			t.Fatalf("no %s", what)
		}
	}
}

func (f *fakeConn) expectControl(t *testing.T, what string, match func(control) bool) {
	t.Helper()
	f.expect(t, what, func(msgType uint64, d *decoder) bool {
		var ctl control
		return msgType == msgControl && json.Unmarshal([]byte(d.string()), &ctl) == nil && match(ctl)
	})
}

func (f *fakeConn) expectSync(t *testing.T, what string, subtype uint64, payload []byte) {
	t.Helper()
	f.expect(t, what, func(msgType uint64, d *decoder) bool {
		return msgType == msgSync && d.uint() == subtype && bytes.Equal(d.bytes(), payload)
	})
}

func TestRoomSeedRelaySaveHandover(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "vol")
	file := filepath.Join(volume, "note.md")
	if err := os.MkdirAll(volume, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volume, volumeservice.MarkerFile), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	events := volumeservice.NewFileEvents()
	saved := make(chan volumeservice.FileEvent, 1)
	events.Subscribe(func(e volumeservice.FileEvent) { saved <- e })
	s := newService(WithRoot(root), WithGracePeriod(time.Hour), WithFileEvents(events))
	open := func() uint64 {
		path := kbservice.NewVolumePath().Folder("vol", "note")
		resp := s.OpenFile(collabservice.OpenFileArgs{Path: *path, Type: "markdown"})
		if resp.Common.Code != 200 {
			t.Fatalf("OpenFile: %d %s", resp.Common.Code, resp.Common.Message)
		}
		return resp.FileID
	}
	id := open()
	if again := open(); again != id {
		t.Fatalf("second OpenFile gave room %d, want %d", again, id)
	}

	connect := func() *fakeConn {
		conn := newFakeConn()
		go s.Collaborate(collabservice.CollaborateArgs{Ctx: context.Background(), FileID: id, Conn: conn})
		return conn
	}

	// The first client is the writer and seeds the document
	a := connect()
	a.expectSync(t, "step 1 for a", syncStep1, emptyStateVector)
	a.expectControl(t, "writer role with seed", func(c control) bool {
		return c.Op == "role" && c.Writer && c.Seed != nil && *c.Seed == "hello"
	})
	a.in <- syncMessage(syncUpdate, []byte{1, 2, 3})
	a.in <- controlMessage(control{Op: "seeded"})
	a.expectControl(t, "ready", func(c control) bool { return c.Op == "ready" })

	// A later client gets the log and later updates
	b := connect()
	b.expectControl(t, "reader role", func(c control) bool { return c.Op == "role" && !c.Writer && c.Ready })
	b.in <- syncMessage(syncStep1, emptyStateVector)
	b.expectSync(t, "step 2 with the seed", syncStep2, []byte{1, 2, 3})
	a.in <- syncMessage(syncUpdate, []byte{4, 5})
	b.expectSync(t, "relayed update", syncUpdate, []byte{4, 5})

	// Saving writes the file and tells everyone
	a.in <- controlMessage(control{Op: "save", Markdown: "hello world"})
	b.expectControl(t, "saved", func(c control) bool { return c.Op == "saved" && c.Code == 200 })
	if content, _ := os.ReadFile(file); string(content) != "hello world" {
		t.Fatalf("file holds %q", content)
	}
	select {
	case e := <-saved:
		if e.Op != volumeservice.FileChanged || e.File != file {
			t.Fatalf("save published %+v", e)
		}
	default:
		t.Fatal("save published no file event")
	}

	// Only the writer may save
	b.in <- controlMessage(control{Op: "save", Markdown: "nope"})

	// When the writer leaves, the next client takes over
	close(a.in)
	b.expectControl(t, "handover", func(c control) bool { return c.Op == "role" && c.Writer && c.Ready && c.Seed == nil })
	if content, _ := os.ReadFile(file); string(content) != "hello world" {
		t.Fatalf("a reader saved: file holds %q", content)
	}
}
