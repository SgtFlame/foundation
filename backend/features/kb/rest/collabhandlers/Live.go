package collabhandlers

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// JoinFrame → collab.join: joins the file's room on a channel the browser
// chose; the room's y-websocket messages then travel as binary frames on
// that channel. LeaveFrame → collab.leave leaves it.
type JoinFrame struct {
	Channel uint32 `json:"channel"`
	FileID  uint64 `json:"fileId,string"`
}

type LeaveFrame struct {
	Channel uint32 `json:"channel"`
}

// ClosedFrame ← collab.closed: the room ended the channel. Code is 4000 +
// the HTTP status of a failure (4404: the room was dropped, open the file
// again), or 1000.
type ClosedFrame struct {
	Type    string `json:"type"` // collab.closed
	Channel uint32 `json:"channel"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// ContributeLive carries collaboration rooms over the live connection, each
// on its own channel, given the service once made. It replaces the room's
// own WebSocket (01-ServerUI question 8).
func ContributeLive(reg *extensions.Registry, service func() collabservice.Service) {
	live.Extensions.Register(reg, live.Extension{
		Descriptor: extensions.Descriptor{ID: "collab", Source: "kb"},
		Frames:     []string{"collab.join", "collab.leave"},
		Attach: func(c live.Conn) live.ConnHandler {
			return &collabConn{c: c, service: service(), rooms: map[uint32]context.CancelFunc{}}
		},
	})
}

type collabConn struct {
	c       live.Conn
	service collabservice.Service
	mu      sync.Mutex
	rooms   map[uint32]context.CancelFunc
}

func (cc *collabConn) Handle(frameType string, raw json.RawMessage) {
	switch frameType {
	case "collab.join":
		var f JoinFrame
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		cc.join(f)
	case "collab.leave":
		var f LeaveFrame
		if json.Unmarshal(raw, &f) == nil {
			cc.leave(f.Channel)
		}
	}
}

func (cc *collabConn) join(f JoinFrame) {
	args := cc.c.Args()
	if !kbservice.InTenant(args.Principal) {
		failed := args.Fail(authz.Status, kbservice.ErrOtherTenant())
		cc.c.Send(ClosedFrame{Type: "collab.closed", Channel: f.Channel, Code: 4000 + int(failed.Code), Message: failed.Message})
		return
	}
	ctx, cancel := context.WithCancel(args.Context())
	cc.mu.Lock()
	if old := cc.rooms[f.Channel]; old != nil {
		old()
	}
	cc.rooms[f.Channel] = cancel
	cc.mu.Unlock()
	ch := &channel{c: cc.c, id: f.Channel, in: make(chan []byte, 256)}
	cc.c.OnBinary(f.Channel, func(payload []byte) {
		msg := append([]byte(nil), payload...)
		select {
		case ch.in <- msg:
		default:
			cancel() // a client sending faster than the room reads is dropped
		}
	})
	args.Ctx = ctx
	go func() {
		defer func() {
			cc.c.CloseChannel(f.Channel)
			cc.mu.Lock()
			delete(cc.rooms, f.Channel)
			cc.mu.Unlock()
			cancel()
		}()
		resp := cc.service.Collaborate(collabservice.CollaborateArgs{ServiceArgs: args, FileID: f.FileID, Conn: ch})
		code, message := 1000, ""
		if resp.Common.Code >= 300 {
			code, message = 4000+int(resp.Common.Code), resp.Common.Message
		}
		if cc.c.Args().Context().Err() == nil {
			cc.c.Send(ClosedFrame{Type: "collab.closed", Channel: f.Channel, Code: code, Message: message})
		}
	}()
}

func (cc *collabConn) leave(channel uint32) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cancel := cc.rooms[channel]; cancel != nil {
		cancel()
	}
}

func (cc *collabConn) Close() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	for _, cancel := range cc.rooms {
		cancel()
	}
}

// channel adapts one live channel to collabservice.Conn.
type channel struct {
	c  live.Conn
	id uint32
	in chan []byte
}

func (ch *channel) Read(ctx context.Context) ([]byte, error) {
	select {
	case msg := <-ch.in:
		return msg, nil
	case <-ctx.Done():
		return nil, errors.New("channel " + strconv.FormatUint(uint64(ch.id), 10) + " closed")
	}
}

func (ch *channel) Write(ctx context.Context, msg []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ch.c.SendBinary(ch.id, msg)
	return nil
}
