// Package live is the live core (documentation/Live.md): commands that
// commit events to JetStream, projectors that fold them into KV buckets,
// queries that are subscriptions, presence, and the one WebSocket per
// browser window that carries them. This package holds the types and the
// points features contribute to; natsd, bus, project, subscribe, presence
// and calls hold the machinery.
package live

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Log is the live core's logging and error builder.
var Log = logging.In("live")

// Commands is the live.command point: every command type the bus decides.
var Commands = extensions.NewPoint[Command]("live.command", "Commands: the only way objects change, each deciding events from the target's state")

// Projectors is the live.projector point: folds of event subjects into KV.
var Projectors = extensions.NewPoint[Projector]("live.projector", "Projectors: durable consumers that fold events into a KV projection")

// Queries is the live.query point: named reads that are subscriptions.
var Queries = extensions.NewPoint[Query]("live.query", "Queries: typed reads over projections, answered with a snapshot and then changes")

// Calls is the live.call point: requests answered with a stream of events.
var Calls = extensions.NewPoint[Call]("live.call", "Calls: requests over the live connection answered with a stream of events, such as a chat reply")

// Extensions is the live.extension point: frame handlers attached to every
// live connection, such as UI sessions and collaboration rooms.
var Extensions = extensions.NewPoint[Extension]("live.extension", "Live connection extensions: handle their own frames on every connection, such as ui.* and collab.*")

// Area names a stream: "def" (definitions), "rec" (records) or "app_<appId>".
type Area string

const (
	AreaDefinitions Area = "def"
	AreaRecords     Area = "rec"
)

// AppArea is the area of one application's own aggregates.
func AppArea(appID string) Area { return Area("app_" + appID) }

var token = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidToken reports whether s can be one subject token or key part.
func ValidToken(s string) bool { return token.MatchString(s) }

// StreamName is the JetStream stream of a tenant's area.
func StreamName(tenant uint64, area Area) string {
	return fmt.Sprintf("EVT_%d_%s", tenant, area)
}

// StreamSubjects is what a tenant's area stream holds.
func StreamSubjects(tenant uint64, area Area) string {
	return fmt.Sprintf("evt.%d.%s.>", tenant, area)
}

// Target is the object a command changes: its area, type and id.
type Target struct {
	Area Area   `json:"area"`
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Key is the target's key in its projection, "<type>.<id>".
func (t Target) Key() string { return t.Type + "." + t.ID }

// Subject is the subject of one event on the target.
func (t Target) Subject(tenant uint64, event string) string {
	return fmt.Sprintf("evt.%d.%s.%s.%s.%s", tenant, t.Area, t.Type, t.ID, event)
}

// Subjects is every event subject of the target.
func (t Target) Subjects(tenant uint64) string { return t.Subject(tenant, "*") }

// TargetOf reads the target back from an event subject.
func TargetOf(subject string) (tenant uint64, t Target, event string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != "evt" {
		return 0, Target{}, "", false
	}
	if _, err := fmt.Sscan(parts[1], &tenant); err != nil {
		return 0, Target{}, "", false
	}
	return tenant, Target{Area: Area(parts[2]), Type: parts[3], ID: parts[4]}, parts[5], true
}

// Event is a committed fact, as stored in the stream.
type Event struct {
	Type      string          `json:"type"` // the event name, the subject's last token
	Target    Target          `json:"target"`
	Data      json.RawMessage `json:"data,omitempty"`
	CommandID string          `json:"commandId"`
	RequestID string          `json:"requestId,omitempty"`
	UserID    uint64          `json:"userId,string,omitempty"`
	TenantID  uint64          `json:"tenantId,string"`
	Time      time.Time       `json:"time"`
	// Set when read back from the stream.
	Stream string `json:"-"`
	Seq    uint64 `json:"-"`
}

// NewEvent is what a command's Decide returns.
type NewEvent struct {
	Type string
	Data any
}

// Problem is why a command was rejected, in a form a model can act on.
type Problem struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// Decision is what a command decides on: the target's current projected
// state (nil when it has none) and the arguments.
type Decision struct {
	features.ServiceArgs
	Target Target
	State  json.RawMessage
	Args   json.RawMessage
}

// Command is one command type. Its Descriptor's ID is the type, e.g.
// "hello.increment". Every command is also a tool (effect write).
type Command struct {
	extensions.Descriptor
	Title       string
	Description string            // for a model: what it does and when to use it
	Args        json.RawMessage   // the arguments' JSON Schema, an object
	Examples    []json.RawMessage // example arguments
	Events      []string          // the events it can commit
	Area        Area              // the target's area
	TargetType  string            // the target's type
	Projection  string            // the projector whose value is the target's state
	// Action is the authz action it needs, checked on Resource (the
	// principal's tenant when nil).
	Action   string
	Resource func(p features.Principal, target Target) string
	// Decide returns the events to commit, or problems. It may run again
	// when another command committed on the target in between.
	Decide func(d Decision) ([]NewEvent, []Problem)
	// Report, when set, is what the command's tool returns after a commit
	// at seq, beside the sequence: a graph draft's problems, for example,
	// so a model sees what its change left to fix.
	Report func(args features.ServiceArgs, target Target, seq uint64) any
}

// Position is how far a subscription has seen, as a sequence per stream.
type Position map[string]uint64

// Merge is p raised to every position in q.
func (p Position) Merge(q Position) Position {
	out := Position{}
	for k, v := range p {
		out[k] = v
	}
	for k, v := range q {
		if v > out[k] {
			out[k] = v
		}
	}
	return out
}

// Reached reports whether p has seen seq on stream.
func (p Position) Reached(stream string, seq uint64) bool { return p[stream] >= seq }

// CallRun is one running call.
type CallRun struct {
	features.ServiceArgs // Ctx is cancelled by the client's cancel frame
	Args                 json.RawMessage
	// Emit sends one event of the call's stream.
	Emit func(event string, data any)
}

// Call is a request answered with a stream of events; Run returns when the
// stream ends, with a failure's response or nil.
type Call struct {
	extensions.Descriptor
	Title       string
	Description string
	Run         func(c CallRun) *features.ServiceResponse
}

// Conn is what a live connection offers its extensions.
type Conn interface {
	ID() string
	Args() features.ServiceArgs // the connection's principal and context
	Send(frame any)
	// SendBinary sends a binary frame on a channel the extension opened.
	SendBinary(channel uint32, payload []byte)
	// OnBinary routes the connection's binary frames on channel to fn.
	OnBinary(channel uint32, fn func(payload []byte))
	CloseChannel(channel uint32)
}

// Extension handles its own frame types on every connection. Attach is
// called once per connection; the handler gets the raw frame of each type
// it lists, and Close when the connection ends.
type Extension struct {
	extensions.Descriptor
	Frames []string
	Attach func(c Conn) ConnHandler
}

// ConnHandler is an extension's state on one connection.
type ConnHandler interface {
	Handle(frameType string, raw json.RawMessage)
	Close()
}
