package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
)

type whoArgs struct {
	features.ServiceArgs
	Hop   bool   // call the next service
	As    uint64 // narrow to this agent before the hop
	Token string
}

type whoResponse struct {
	Common   features.ServiceResponse
	Kind     features.PrincipalKind
	User     uint64
	Agent    uint64
	Reach    string
	MemoryOf uint64
	Parent   string
	Next     *whoResponse
}

func who(next *Client) func(whoArgs) whoResponse {
	return func(a whoArgs) whoResponse {
		r := whoResponse{Common: features.OK(), Kind: a.Principal.Kind, User: a.Principal.UserID, Agent: a.Principal.AgentID, Reach: a.Principal.Reach, MemoryOf: a.Principal.MemoryOf}
		if t, ok := TokenFrom(a.Context()); ok {
			r.Parent = t.Parent
		}
		if a.Hop {
			args := a.ServiceArgs
			if a.As != 0 {
				args.Principal = features.AgentPrincipal(args.Principal.TenantID, a.As, args.Principal.UserID)
				args.Principal.Reach = "task:1"
			}
			n := Call[whoResponse](next, "second", "Who", whoArgs{ServiceArgs: args})
			r.Next = &n
		}
		return r
	}
}

func TestCallTokens(t *testing.T) {
	ns := natsd.StartTest(t)
	keys, err := NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	entry := &Client{NC: ns.Conn(), Keys: keys}    // an entry point's process
	forwarder := &Client{NC: ns.Conn()}            // a data process: no key
	narrower := &Client{NC: ns.Conn(), Keys: keys} // a runner: may narrow
	first, err := Serve(ns.Conn(), "first", map[string]Handler{"Who": Unary(who(forwarder))}, VerifyWith(keys.Public()))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Stop()
	second, err := Serve(ns.Conn(), "second", map[string]Handler{"Who": Unary(who(nil))}, VerifyWith(keys.Public()))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Stop()
	third, err := Serve(ns.Conn(), "third", map[string]Handler{"Who": Unary(who(narrower))}, VerifyWith(keys.Public()))
	if err != nil {
		t.Fatal(err)
	}
	defer third.Stop()

	user := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: 1, MemoryOf: 7}
	r := Call[whoResponse](entry, "first", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Principal: user}, Hop: true})
	if r.Common.Code != http.StatusOK || r.User != 7 || r.MemoryOf != 7 || r.Next == nil || r.Next.User != 7 || r.Next.MemoryOf != 7 || r.Next.Parent != "" {
		t.Fatalf("forwarded %+v next %+v", r, r.Next)
	}
	// A process without the key cannot call for another principal.
	r = Call[whoResponse](entry, "first", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Principal: user}, Hop: true, As: 9})
	if r.Next == nil || r.Next.Common.Code != http.StatusForbidden || r.Next.Common.ErrorCode != CodePrincipalChanged {
		t.Fatalf("widened by a forwarder: %+v", r.Next)
	}
	// One with the key narrows, naming the parent.
	r = Call[whoResponse](entry, "third", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Principal: user}, Hop: true, As: 9})
	if r.Next == nil || r.Next.Kind != features.PrincipalAgent || r.Next.Agent != 9 || r.Next.Reach != "task:1" || r.Next.Parent == "" {
		t.Fatalf("narrowed %+v", r.Next)
	}

	// Refusals: no key at the server, a forged token, an expired token.
	other, _ := NewKeys()
	forged := &Client{NC: ns.Conn(), Keys: other}
	if r := Call[whoResponse](forged, "second", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Principal: user}}); r.Common.Code != http.StatusUnauthorized || r.Common.ErrorCode != CodeUntrusted {
		t.Fatalf("forged %+v", r.Common)
	}
	old, _ := keys.Mint(user, nil)
	old.Expires = time.Now().Add(-time.Minute).Unix()
	stale, _ := signed(keys, old)
	ctx := WithToken(context.Background(), stale)
	if r := Call[whoResponse](forwarder, "second", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: user}}); r.Common.Code != http.StatusUnauthorized || r.Common.ErrorCode != CodeExpired {
		t.Fatalf("expired %+v", r.Common)
	}
	// Plain principal headers are not trusted without TrustHeaders.
	if r := Call[whoResponse](forwarder, "second", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Principal: user}}); r.Common.Code != http.StatusUnauthorized {
		t.Fatalf("plain principal %+v", r.Common)
	}
	// A check decided by a role cannot travel.
	asCtx := authz.WithAs(context.Background(), authz.As{Name: "viewer"})
	if r := Call[whoResponse](entry, "second", "Who", whoArgs{ServiceArgs: features.ServiceArgs{Ctx: asCtx, Principal: user}}); r.Common.Code != http.StatusForbidden || r.Common.ErrorCode != CodeAsLocal {
		t.Fatalf("as %+v", r.Common)
	}
}

// signed re-signs t (with changed fields) with keys, for tests.
func signed(keys *Keys, t Token) (Token, error) {
	n, err := keys.Mint(t.PrincipalOf(), nil)
	if err != nil {
		return n, err
	}
	n.Expires = t.Expires
	return resign(keys, n)
}

func TestRotation(t *testing.T) {
	keys, _ := NewKeys()
	p := features.LocalPrincipal()
	before, _ := keys.Mint(p, nil)
	rotated, err := keys.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(before.Raw(), rotated.Public(), time.Now()); err != nil {
		t.Fatalf("a token of the previous key: %v", err)
	}
	again, _ := rotated.Rotate()
	if _, err := Verify(before.Raw(), again.Public(), time.Now()); err == nil {
		t.Fatal("a token two rotations old was accepted")
	}
	back, err := ParseKeys(rotated.Marshal())
	if err != nil || len(back.Previous) != 1 || !back.Current.Equal(rotated.Current) {
		t.Fatalf("parsed %+v %v", back, err)
	}
}
