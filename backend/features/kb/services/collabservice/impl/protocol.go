package impl

import (
	"encoding/json"
	"errors"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

// Message types of the y-websocket protocol, plus our own control messages.
const (
	msgSync           = 0
	msgAwareness      = 1
	msgQueryAwareness = 3
	msgControl        = 100 // varString JSON control
	msgCompacted      = 101 // varUint8Array full-state update from the writer
)

// Sync message subtypes.
const (
	syncStep1  = 0
	syncStep2  = 1
	syncUpdate = 2
)

// emptyUpdate is a Yjs update with no structs and an empty delete set.
var emptyUpdate = []byte{0, 0}

// emptyStateVector asks the client for its whole document.
var emptyStateVector = []byte{0}

var errTruncated = errors.New("collab: truncated message")

// control is a JSON control message. Op is one of role, seeded, ready, save,
// saved, compact, edit (to the writer: apply Edits to the document) and
// edited (the writer's answer to the edit with the same ID).
type control struct {
	Op       string               `json:"op"`
	Writer   bool                 `json:"writer,omitempty"`
	Ready    bool                 `json:"ready,omitempty"`
	Dirty    bool                 `json:"dirty,omitempty"` // role: the new writer must save right away
	Seed     *string              `json:"seed,omitempty"`
	Markdown string               `json:"markdown,omitempty"`
	Code     int                  `json:"code,omitempty"`
	Message  string               `json:"message,omitempty"`
	At       int64                `json:"at,omitempty"` // Unix milliseconds
	ID       string               `json:"id,omitempty"` // edit and edited
	Edits    []collabservice.Edit `json:"edits,omitempty"`
}

// decoder reads lib0-encoded values.
type decoder struct {
	buf []byte
	err error
}

func (d *decoder) uint() uint64 {
	var n uint64
	for shift := 0; ; shift += 7 {
		if len(d.buf) == 0 || shift > 63 {
			d.err = errTruncated
			return 0
		}
		b := d.buf[0]
		d.buf = d.buf[1:]
		n |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return n
		}
	}
}

func (d *decoder) bytes() []byte {
	n := d.uint()
	if d.err != nil {
		return nil
	}
	if uint64(len(d.buf)) < n {
		d.err = errTruncated
		return nil
	}
	b := d.buf[:n]
	d.buf = d.buf[n:]
	return b
}

func (d *decoder) string() string {
	return string(d.bytes())
}

// encoder writes lib0-encoded values.
type encoder struct {
	buf []byte
}

func (e *encoder) uint(n uint64) *encoder {
	for n >= 0x80 {
		e.buf = append(e.buf, byte(n)|0x80)
		n >>= 7
	}
	e.buf = append(e.buf, byte(n))
	return e
}

func (e *encoder) bytes(b []byte) *encoder {
	e.uint(uint64(len(b)))
	e.buf = append(e.buf, b...)
	return e
}

func (e *encoder) string(s string) *encoder {
	return e.bytes([]byte(s))
}

func syncMessage(subtype uint64, payload []byte) []byte {
	e := &encoder{}
	return e.uint(msgSync).uint(subtype).bytes(payload).buf
}

func awarenessMessage(update []byte) []byte {
	e := &encoder{}
	return e.uint(msgAwareness).bytes(update).buf
}

func controlMessage(c control) []byte {
	b, _ := json.Marshal(c)
	e := &encoder{}
	return e.uint(msgControl).string(string(b)).buf
}

// awarenessEntry is one client's state in an awareness update; State is the
// JSON text, "null" once the client has gone.
type awarenessEntry struct {
	ClientID uint64
	Clock    uint64
	State    string
}

func decodeAwareness(update []byte) ([]awarenessEntry, error) {
	d := &decoder{buf: update}
	n := d.uint()
	var entries []awarenessEntry
	for i := uint64(0); i < n && d.err == nil; i++ {
		entries = append(entries, awarenessEntry{ClientID: d.uint(), Clock: d.uint(), State: d.string()})
	}
	return entries, d.err
}

func encodeAwareness(entries []awarenessEntry) []byte {
	e := &encoder{}
	e.uint(uint64(len(entries)))
	for _, entry := range entries {
		e.uint(entry.ClientID).uint(entry.Clock).string(entry.State)
	}
	return e.buf
}
