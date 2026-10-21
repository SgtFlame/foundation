package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// Compaction thresholds for a room's update log.
const (
	compactEntries = 500
	compactBytes   = 1 << 20
)

// client is one connection to a room. Messages to it are queued on send and
// written by its own goroutine, so a slow client never blocks the room.
type client struct {
	conn      collabservice.Conn
	send      chan []byte
	cancel    context.CancelFunc
	clientIDs map[uint64]bool // Yjs client IDs this connection announced in awareness
}

func newClient(conn collabservice.Conn, cancel context.CancelFunc) *client {
	return &client{conn: conn, send: make(chan []byte, 256), cancel: cancel, clientIDs: map[uint64]bool{}}
}

func (c *client) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.send:
			if err := c.conn.Write(ctx, msg); err != nil {
				c.cancel()
				return
			}
		}
	}
}

// queue sends msg without blocking; a client too slow to keep up is dropped.
func (c *client) queue(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.cancel()
	}
}

// room is one open file. The server relays the Yjs updates it receives and
// keeps them as an opaque log for clients that join later; it never decodes
// the document. The writer (the oldest connection) seeds the document from the
// file and saves it back.
type room struct {
	mu         sync.Mutex
	id         uint64
	file       string // absolute path
	svc        *service
	clients    []*client // in join order
	writer     *client
	log        [][]byte
	logBytes   int
	seeded     bool // the writer has loaded the file into the document
	dirty      bool // updates arrived since the last save
	compacting bool
	awareness  map[uint64]awarenessEntry
	teardown   *time.Timer
}

func newRoom(svc *service, id uint64, file string) *room {
	return &room{id: id, file: file, svc: svc, awareness: map[uint64]awarenessEntry{}}
}

// join adds c to the room, asks it for its state and tells it its role.
func (r *room) join(c *client) *features.ServiceResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.teardown != nil {
		r.teardown.Stop()
		r.teardown = nil
	}
	r.clients = append(r.clients, c)
	c.queue(syncMessage(syncStep1, emptyStateVector))
	if len(r.awareness) > 0 {
		c.queue(awarenessMessage(encodeAwareness(r.awarenessEntries())))
	}
	if r.writer == nil {
		return r.elect(c)
	}
	c.queue(controlMessage(control{Op: "role", Ready: r.seeded}))
	return nil
}

// leave removes c, clears its awareness for everyone else and hands the
// writer role on. An empty room is dropped after the service's grace period.
func (r *room) leave(c *client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients = slices.DeleteFunc(r.clients, func(other *client) bool { return other == c })

	var gone []awarenessEntry
	for id := range c.clientIDs {
		if entry, ok := r.awareness[id]; ok {
			gone = append(gone, awarenessEntry{ClientID: id, Clock: entry.Clock + 1, State: "null"})
			delete(r.awareness, id)
		}
	}
	if len(gone) > 0 {
		r.broadcast(nil, awarenessMessage(encodeAwareness(gone)))
	}

	if r.writer == c {
		r.writer = nil
		r.compacting = false
		// A writer that left after sending the seed but before confirming it
		// still seeded the document; seeding it again would duplicate it.
		if !r.seeded && len(r.log) > 0 {
			r.markSeeded()
		}
		for len(r.clients) > 0 {
			if failed := r.elect(r.clients[0]); failed == nil {
				break
			}
			r.clients[0].cancel()
			r.clients = r.clients[1:]
		}
	}

	if len(r.clients) == 0 {
		r.teardown = time.AfterFunc(r.svc.gracePeriod, func() { r.svc.dropRoom(r) })
	}
}

// elect makes c the writer, sending it the file to seed the document with if
// that has not happened yet. Called with the lock held.
func (r *room) elect(c *client) *features.ServiceResponse {
	role := control{Op: "role", Writer: true, Ready: r.seeded, Dirty: r.dirty}
	if !r.seeded {
		content, err := os.ReadFile(r.file)
		if err != nil {
			return volumeservice.Fail(http.StatusInternalServerError, "cannot read %q: %s", r.svc.rel(r.file), err)
		}
		seed := string(content)
		role.Seed = &seed
	}
	r.writer = c
	c.queue(controlMessage(role))
	return nil
}

// handle processes one message from c.
func (r *room) handle(c *client, msg []byte) {
	d := &decoder{buf: msg}
	msgType := d.uint()
	if d.err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	switch msgType {
	case msgSync:
		subtype := d.uint()
		payload := d.bytes()
		if d.err != nil {
			return
		}
		if subtype == syncStep1 {
			r.sendLog(c)
			return
		}
		r.addUpdate(c, payload)
	case msgAwareness:
		update := d.bytes()
		entries, err := decodeAwareness(update)
		if err != nil {
			return
		}
		for _, entry := range entries {
			c.clientIDs[entry.ClientID] = true
			if current, ok := r.awareness[entry.ClientID]; !ok || entry.Clock >= current.Clock {
				if entry.State == "null" {
					delete(r.awareness, entry.ClientID)
				} else {
					r.awareness[entry.ClientID] = entry
				}
			}
		}
		r.broadcast(c, msg)
	case msgQueryAwareness:
		c.queue(awarenessMessage(encodeAwareness(r.awarenessEntries())))
	case msgControl:
		var ctl control
		if json.Unmarshal([]byte(d.string()), &ctl) != nil || d.err != nil || c != r.writer {
			return
		}
		switch ctl.Op {
		case "seeded":
			r.markSeeded()
		case "save":
			r.save(ctl.Markdown)
		}
	case msgCompacted:
		update := d.bytes()
		if d.err != nil || c != r.writer || !r.compacting {
			return
		}
		r.log = [][]byte{bytes.Clone(update)}
		r.logBytes = len(update)
		r.compacting = false
	}
}

// sendLog answers a sync step 1 with every update so far; Yjs updates are
// idempotent, so the client may already have some of them.
func (r *room) sendLog(c *client) {
	if len(r.log) == 0 {
		c.queue(syncMessage(syncStep2, emptyUpdate))
		return
	}
	c.queue(syncMessage(syncStep2, r.log[0]))
	for _, update := range r.log[1:] {
		c.queue(syncMessage(syncUpdate, update))
	}
}

func (r *room) addUpdate(from *client, update []byte) {
	if len(update) == 0 || bytes.Equal(update, emptyUpdate) {
		return
	}
	update = bytes.Clone(update)
	r.log = append(r.log, update)
	r.logBytes += len(update)
	r.dirty = true
	r.broadcast(from, syncMessage(syncUpdate, update))

	if !r.compacting && r.writer != nil && (len(r.log) > compactEntries || r.logBytes > compactBytes) {
		r.compacting = true
		r.writer.queue(controlMessage(control{Op: "compact"}))
	}
}

func (r *room) markSeeded() {
	r.seeded = true
	r.broadcast(nil, controlMessage(control{Op: "ready"}))
}

// save writes markdown to the file atomically and tells everyone the outcome.
func (r *room) save(markdown string) {
	saved := control{Op: "saved", Code: http.StatusOK, At: time.Now().UnixMilli()}
	tmp := filepath.Join(filepath.Dir(r.file), "."+filepath.Base(r.file)+".tmp")
	err := os.WriteFile(tmp, []byte(markdown), 0o644)
	if err == nil {
		err = os.Rename(tmp, r.file)
	}
	if err != nil {
		_ = os.Remove(tmp)
		saved.Code = http.StatusInternalServerError
		saved.Message = "cannot save " + r.svc.rel(r.file)
	} else {
		r.dirty = false
	}
	r.broadcast(nil, controlMessage(saved))
}

// broadcast queues msg for every client except skip.
func (r *room) broadcast(skip *client, msg []byte) {
	for _, c := range r.clients {
		if c != skip {
			c.queue(msg)
		}
	}
}

func (r *room) awarenessEntries() []awarenessEntry {
	entries := make([]awarenessEntry, 0, len(r.awareness))
	for _, entry := range r.awareness {
		entries = append(entries, entry)
	}
	return entries
}

// empty reports whether the room has no clients.
func (r *room) empty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients) == 0
}
