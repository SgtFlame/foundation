package notifications_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/notifications"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

type fixture struct {
	t    *testing.T
	deps data.Deps
	subs *subscribe.Subscriptions
	b    *bus.Bus
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	var entityDeps entities.Deps
	var deps data.Deps
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return deps })
	notifications.ContributeAccess(reg)
	notifications.Contribute(reg, func() notifications.Deps { return deps })
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	b := bus.New(p)
	deps = data.Deps{Projections: p, Bus: b}
	entityDeps = entities.Deps{Projections: p, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, deps) }}
	return &fixture{t: t, deps: deps, subs: subscribe.New(p, reg), b: b}
}

func user(id uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: features.LocalTenantID}}
}

func (f *fixture) once(as features.ServiceArgs, query string, params any, into any) {
	f.t.Helper()
	raw, _ := json.Marshal(params)
	v, _, fail := f.subs.Once(as, query, raw)
	if fail != nil {
		f.t.Fatalf("%s: %s", query, fail.Message)
	}
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, into)
}

// mine is a user's notifications' titles, newest first.
func (f *fixture) mine(id uint64, unread bool) []string {
	f.t.Helper()
	var res struct {
		Records []struct {
			Title string `json:"title"`
			Read  bool   `json:"read"`
		} `json:"records"`
	}
	f.once(user(id), notifications.MineQuery, map[string]any{"unread": unread}, &res)
	out := []string{}
	for _, r := range res.Records {
		out = append(out, r.Title)
	}
	return out
}

func (f *fixture) count(id uint64) float64 {
	f.t.Helper()
	var res struct {
		Groups []struct {
			Values map[string]float64 `json:"values"`
		} `json:"groups"`
	}
	f.once(user(id), notifications.CountQuery, map[string]any{}, &res)
	if len(res.Groups) == 0 {
		return 0
	}
	return res.Groups[0].Values["count"]
}

func TestNotify(t *testing.T) {
	f := setup(t)
	app := features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(features.LocalTenantID, "nexus")}
	n := notifications.Notification{Recipients: []string{"7", "8", "7"}, Kind: "mention", Title: "Ana mentioned you in #design", Link: "workbench:app:nexus:channel/c1", Key: "nexus:m1:mention"}
	ids, fail := notifications.Notify(app, f.deps, n)
	if fail != nil {
		t.Fatalf("notify: %s", fail.Message)
	}
	if len(ids) != 2 {
		t.Fatalf("notified %d, not the two recipients once each: %v", len(ids), ids)
	}
	// A retry (the same key) notifies no one again
	again, fail := notifications.Notify(app, f.deps, n)
	if fail != nil || len(again) != 2 || again[0] != ids[0] || again[1] != ids[1] {
		t.Fatalf("a retry made other notifications: %v %v", again, fail)
	}
	// Another key notifies again
	if _, fail := notifications.Notify(app, f.deps, notifications.Notification{Recipients: []string{"7"}, Kind: "review", Title: "A draft waits for your review", Key: "mail:d1:review"}); fail != nil {
		t.Fatal(fail.Message)
	}
	time.Sleep(50 * time.Millisecond)
	if got := f.mine(7, false); len(got) != 2 || got[0] != "A draft waits for your review" {
		t.Errorf("7's notifications: %v", got)
	}
	if got := f.mine(8, false); len(got) != 1 {
		t.Errorf("8's notifications: %v", got)
	}
	if got := f.mine(9, false); len(got) != 0 {
		t.Errorf("someone else reads notifications not theirs: %v", got)
	}
	if c := f.count(7); c != 2 {
		t.Errorf("7's unread count: %v", c)
	}

	// Nobody but the recipient changes one
	raw, _ := json.Marshal(map[string]any{"entity": notifications.Entity, "set": map[string]any{"read": true}})
	if res := f.b.Execute(bus.Request{ServiceArgs: user(8), ID: "x1", Type: "record.update", Target: ids[0], Args: raw}); res.Common.Code < 300 {
		t.Error("8 marked 7's notification read")
	}
	// Nor makes one by hand
	raw, _ = json.Marshal(map[string]any{"entity": notifications.Entity, "values": map[string]any{"recipient": "7", "title": "fake"}})
	if res := f.b.Execute(bus.Request{ServiceArgs: user(8), ID: "x2", Type: "record.create", Target: "fake1", Args: raw}); res.Common.Code < 300 {
		t.Error("a person made a notification for someone else")
	}

	// Mark all read: 7's count follows, 8's does not
	if n, fail := notifications.MarkAllRead(user(7), f.deps); fail != nil || n != 2 {
		t.Fatalf("mark all read: %d %v", n, fail)
	}
	if c := f.count(7); c != 0 {
		t.Errorf("7's unread count after marking all read: %v", c)
	}
	if got := f.mine(7, true); len(got) != 0 {
		t.Errorf("7's unread after marking all read: %v", got)
	}
	if c := f.count(8); c != 1 {
		t.Errorf("8's unread count: %v", c)
	}

	// Anonymous principals send nothing
	anon := features.ServiceArgs{Ctx: context.Background(), Principal: features.AnonymousPrincipal()}
	if _, fail := notifications.Notify(anon, f.deps, notifications.Notification{Recipients: []string{"7"}, Title: "x"}); fail == nil {
		t.Error("an anonymous principal notified")
	}
}
