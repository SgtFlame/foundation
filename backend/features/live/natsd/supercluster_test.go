package natsd

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func connectJS(t *testing.T, url string, domain ...string) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	nc, err := nats.Connect(url, nats.MaxReconnects(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	var js jetstream.JetStream
	if len(domain) > 0 {
		js, err = jetstream.NewWithDomain(nc, domain[0])
	} else {
		js, err = jetstream.New(nc)
	}
	if err != nil {
		t.Fatal(err)
	}
	return nc, js
}

func retry(t *testing.T, what string, f func() error) {
	t.Helper()
	var err error
	for try := 0; try < 100; try++ {
		if err = f(); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s: %v", what, err)
}

// TestTenantPlacedInACluster: a tenant placed in cluster b has its
// streams made there by a client of cluster a, and Place moves them to
// cluster a keeping every message.
func TestTenantPlacedInACluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sc := StartTestSupercluster(t, 2, 3)
	defer SetTenantPlacements(nil)
	SetTenantPlacements(func(tenant uint64) (Placement, bool) {
		if tenant == 5 {
			return Placement{Cluster: "b"}, true
		}
		return Placement{}, false
	})
	_, js := connectJS(t, sc.Clusters[0].URL())
	var st jetstream.Stream
	retry(t, "make EVT_5_def", func() (err error) {
		st, err = js.CreateOrUpdateStream(ctx, Stream(jetstream.StreamConfig{Name: "EVT_5_def", Subjects: []string{"evt.5.def.>"}, Replicas: 3}))
		return err
	})
	if got := st.CachedInfo().Cluster.Name; got != "b" {
		t.Fatalf("EVT_5_def is in cluster %q, want b", got)
	}
	other, err := js.CreateOrUpdateStream(ctx, Stream(jetstream.StreamConfig{Name: "EVT_6_def", Subjects: []string{"evt.6.def.>"}}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		retry(t, "publish", func() error {
			_, err := js.Publish(ctx, fmt.Sprintf("evt.5.def.x.%d.made", i), []byte("{}"), jetstream.WithMsgID(fmt.Sprint(i)))
			return err
		})
	}
	moved, err := Place(ctx, js, 5, Placement{Cluster: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "EVT_5_def" {
		t.Fatalf("moved %v", moved)
	}
	info, err := st.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Cluster.Name != "a" || info.State.Msgs != 10 || info.State.LastSeq != 10 {
		t.Fatalf("after the move: cluster %q, %d messages, last %d", info.Cluster.Name, info.State.Msgs, info.State.LastSeq)
	}
	if again, err := Place(ctx, js, 5, Placement{Cluster: "a"}); err != nil || len(again) != 0 {
		t.Fatalf("a second move: %v %v", again, err)
	}
	if oi, _ := other.Info(ctx); oi.Config.Placement != nil {
		t.Fatalf("tenant 6's stream was placed: %+v", oi.Config.Placement)
	}
}

// TestSiteKeepsCommitting: an on-premise site, a single leaf node or a
// cluster of its own, commits to its own JetStream domain while its link
// to the hub is cut; the hub's requests to it fail cleanly then and
// succeed once the link is back.
func TestSiteKeepsCommitting(t *testing.T) {
	sc := StartTestSupercluster(t, 1, 3)
	for _, n := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d nodes", n), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			site := sc.StartTestSite(fmt.Sprintf("site%d", n), fmt.Sprintf("site%d", n), n)
			snc, sjs := connectJS(t, site.URL())
			subject := fmt.Sprintf("ctl.site%d.ping", n)
			if _, err := snc.Subscribe(subject, func(m *nats.Msg) { _ = m.Respond([]byte("pong")) }); err != nil {
				t.Fatal(err)
			}
			st, err := sjs.CreateOrUpdateStream(ctx, Stream(jetstream.StreamConfig{Name: "SITE", Subjects: []string{"site.>"}, Replicas: n}))
			if err != nil {
				t.Fatal(err)
			}
			hnc, _ := connectJS(t, sc.Hub().URL())
			retry(t, "the hub reaches the site", func() error {
				_, err := hnc.Request(subject, nil, time.Second)
				return err
			})
			// the hub reads the site's stream through its domain
			_, hjs := connectJS(t, sc.Hub().URL(), site.Domain)
			retry(t, "the hub reads the site's stream", func() error {
				_, err := hjs.Stream(ctx, "SITE")
				return err
			})

			site.Link.Cut()
			for i := 0; i < 5; i++ {
				if _, err := sjs.Publish(ctx, fmt.Sprintf("site.e.%d", i), []byte("{}")); err != nil {
					t.Fatalf("the site cannot commit with its link cut: %v", err)
				}
			}
			retry(t, "the hub's request fails cleanly", func() error {
				_, err := hnc.Request(subject, nil, 300*time.Millisecond)
				if errors.Is(err, nats.ErrNoResponders) || errors.Is(err, nats.ErrTimeout) {
					return nil
				}
				return fmt.Errorf("still answered: %v", err)
			})
			site.Link.Restore()
			retry(t, "the hub reaches the site again", func() error {
				_, err := hnc.Request(subject, nil, time.Second)
				return err
			})
			if info, err := st.Info(ctx); err != nil || info.State.Msgs != 5 {
				t.Fatalf("site stream: %+v %v", info, err)
			}
		})
	}
}

func TestTenantOfName(t *testing.T) {
	for name, want := range map[string]uint64{
		"EVT_5_def": 5, "PRJ_12_records_record": 12, "KV_cx_secrets_7": 7, "OBJ_FILES_3": 3,
		"JOBS_9": 9, "presence_4": 4, "ui_sessions_8": 8, "EVT_5_app_tasks2": 5, "cx_adapters": 0, "SITE": 0,
	} {
		got, ok := TenantOfName(name)
		if got != want || ok != (want != 0) {
			t.Errorf("TenantOfName(%s) = %d, %v; want %d", name, got, ok, want)
		}
	}
	p, err := ParsePlacements("5=east, 7=west")
	if err != nil || p[5].Cluster != "east" || p[7].Cluster != "west" {
		t.Fatalf("%v %v", p, err)
	}
	if _, err := ParsePlacements("5"); err == nil {
		t.Fatal("5 alone is not a placement")
	}
}
