package hello_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	liveimpl "github.com/SgtFlame/foundation/backend/features/live/services/liveservice/impl"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	"github.com/SgtFlame/foundation/backend/features/ui/hello"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/uihandlers"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
	uiimpl "github.com/SgtFlame/foundation/backend/features/ui/services/uiservice/impl"
)

// socket is an in-memory live connection.
type socket struct {
	in  chan []byte
	out chan []byte
}

func (s *socket) Read(ctx context.Context) (bool, []byte, error) {
	select {
	case m, ok := <-s.in:
		if !ok {
			return false, nil, errors.New("closed")
		}
		return false, m, nil
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
}

func (s *socket) Write(ctx context.Context, binary bool, msg []byte) error {
	select {
	case s.out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type client struct {
	t *testing.T
	s *socket
}

func (c *client) send(frame string) { c.s.in <- []byte(frame) }

// await reads frames until one contains all of want.
func (c *client) await(want ...string) string {
	c.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-c.s.out:
			ok := true
			for _, w := range want {
				ok = ok && strings.Contains(string(m), w)
			}
			if ok {
				return string(m)
			}
		case <-timeout:
			c.t.Fatalf("no frame with %v", want)
		}
	}
}

func TestTwoUsersSeeEachOthersIncrements(t *testing.T) {
	reg := extensions.NewRegistry()
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	hello.Contribute(reg)
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	var uiService uiservice.Service = uiimpl.New(uiimpl.WithRegistry(reg), uiimpl.WithBus(b), uiimpl.WithSubscriptions(subs), uiimpl.WithStore(ui.NewKVSessions(srv.JS())))
	uihandlers.ContributeLive(reg, func() uiservice.Service { return uiService })
	pres := presence.New(srv.JS(), nil)
	presence.Contribute(reg, func() *presence.Presence { return pres })
	liveService := liveimpl.New(liveimpl.WithBus(b), liveimpl.WithSubscriptions(subs), liveimpl.WithRegistry(reg), liveimpl.WithPresence(pres))

	connect := func(user uint64) *client {
		s := &socket{in: make(chan []byte, 16), out: make(chan []byte, 1024)}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go liveService.Serve(liveservice.ServeArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}, Socket: s})
		c := &client{t: t, s: s}
		c.await(`"type":"hello"`)
		return c
	}
	a, bb := connect(1), connect(2)
	a.send(`{"type":"ui.open","session":"sa","page":"hello/main"}`)
	bb.send(`{"type":"ui.open","session":"sb","page":"hello/main"}`)
	tree := a.await(`"type":"ui.tree"`, `"session":"sa"`)
	bb.await(`"type":"ui.tree"`, `"session":"sb"`)
	if strings.Contains(tree, "ui.error") {
		t.Fatalf("the page has invalid nodes: %s", tree)
	}

	a.send(`{"type":"ui.event","session":"sa","handler":"increment","path":["top","counter","row","increment"]}`)
	// Both see the counter's bound prop patched, the sender by the same path
	got := a.await(`"type":"ui.patch"`, `"session":"sa"`, `"text":1`)
	bb.await(`"type":"ui.patch"`, `"session":"sb"`, `"text":1`)
	var pf ui.PatchFrame
	_ = json.Unmarshal([]byte(got), &pf)
	if len(pf.Patches) != 1 || pf.Patches[0].Op != "props" {
		t.Fatalf("want one props patch, got %s", got)
	}

	// Commands straight over the connection, acknowledged with their sequence
	bb.send(`{"type":"command","id":"c1","command":"hello.connect","target":"main","args":{"source":"start","sourceHandle":"out","target":"greet","targetHandle":"in"}}`)
	bb.await(`"type":"ack"`, `"id":"c1"`)
	a.await(`"type":"ui.patch"`, `"edges"`, `start.out-greet.in`)
	bb.send(`{"type":"command","id":"c1","command":"hello.connect","target":"main","args":{"source":"start","sourceHandle":"out","target":"greet","targetHandle":"in"}}`)
	// Resent, it applies once: the edge is there, so nothing more is committed
	bb.await(`"type":"ack"`, `"id":"c1"`)
	bb.send(`{"type":"command","id":"c0","command":"hello.increment","target":"main"}`)
	bb.await(`"type":"ack"`, `"id":"c0"`)
	bb.send(`{"type":"command","id":"c0","command":"hello.increment","target":"main"}`)
	bb.await(`"type":"ack"`, `"id":"c0"`, `"duplicate":true`)
	bb.send(`{"type":"command","id":"c2","command":"hello.moveNode","target":"main","args":{"id":"nope","position":{"x":1,"y":2}}}`)
	bb.await(`"type":"rejected"`, `"id":"c2"`, `there is no node`)

	// A subscription and presence
	a.send(`{"type":"subscribe","sub":"p","query":"live.presence","params":{"object":"app.hello"}}`)
	a.await(`"type":"snapshot"`, `"sub":"p"`)
	bb.send(`{"type":"presence","object":"app.hello","state":{"selected":["greet"]}}`)
	a.await(`"type":"change"`, `"sub":"p"`, `greet`)

	// The view state of a session survives its connection
	a.send(`{"type":"ui.event","session":"sa","handler":"greet","path":["top","greet","form","greet"],"payload":{"name":"Ada"}}`)
	a.await(`Hello, Ada!`)
	c := connect(1)
	c.send(`{"type":"ui.open","session":"sa","page":"hello/main"}`)
	c.await(`"type":"ui.tree"`, `Hello, Ada!`)
}
