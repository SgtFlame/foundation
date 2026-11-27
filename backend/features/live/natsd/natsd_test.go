package natsd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// TestNoRawStreamConfig fails on a stream, bucket or object store config
// made without Stream, KV or Object, so every one gets the deployment's
// replicas (forge 20a-1).
func TestNoRawStreamConfig(t *testing.T) {
	root, err := filepath.Abs("../../..") // backend/
	if err != nil {
		t.Fatal(err)
	}
	raw := regexp.MustCompile(`jetstream\.(StreamConfig|KeyValueConfig|ObjectStoreConfig)\{`)
	wrapped := regexp.MustCompile(`natsd\.(Stream|KV|Object)\($`)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "natsd" || d.Name() == "third_party" || d.Name() == "out" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(b)
		for _, m := range raw.FindAllStringIndex(s, -1) {
			if !wrapped.MatchString(s[:m[0]]) {
				line := strings.Count(s[:m[0]], "\n") + 1
				t.Errorf("%s:%d: wrap the config in natsd.Stream, natsd.KV or natsd.Object", path, line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestSharedJetStreamOnlyForServices: the shared (APP) connection's
// JetStream is used only by the connectors, whose state stays in the
// services account (forge 20a-3); everything a tenant owns gets its
// JetStream from Projections.JetStreamFor or natsd.For.
func TestSharedJetStreamOnlyForServices(t *testing.T) {
	root, err := filepath.Abs("../../..") // backend/
	if err != nil {
		t.Fatal(err)
	}
	shared := regexp.MustCompile(`Projections\.JetStream\(\)`)
	allowed := []string{"features/connectors/", "features/jobs/Triggers.go"} // te.Stream: CX_<tenant> is the connectors'
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, a := range allowed {
			if strings.HasPrefix(filepath.ToSlash(rel), a) {
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range shared.FindAllIndex(b, -1) {
			t.Errorf("%s:%d: use Projections.JetStreamFor(ctx, tenant): a tenant's streams are in its own account", rel, strings.Count(string(b[:m[0]]), "\n")+1)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlacementDefaults(t *testing.T) {
	defer SetDefault(Placement{Replicas: 1})
	SetDefault(Placement{Replicas: 3, Cluster: "east"})
	c := Stream(jetstream.StreamConfig{Name: "S"})
	if c.Replicas != 3 || c.Placement == nil || c.Placement.Cluster != "east" {
		t.Fatalf("stream config %+v", c)
	}
	if c := KV(jetstream.KeyValueConfig{Bucket: "b", Replicas: 1}); c.Replicas != 1 {
		t.Fatalf("an explicit replica count is kept: %+v", c)
	}
	SetDefault(Placement{})
	if c := Object(jetstream.ObjectStoreConfig{Bucket: "o"}); c.Replicas != 1 || c.Placement != nil {
		t.Fatalf("object config %+v", c)
	}
}

// TestClusterSurvivesANode: a three-node cluster keeps committing with
// expected sequences, as the bus does, while any one node is down, and a
// restarted node catches up.
func TestClusterSurvivesANode(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a three-node cluster")
	}
	defer SetDefault(Placement{Replicas: 1})
	SetDefault(Placement{Replicas: 3})
	c := StartTestCluster(t, 3)
	s := c.Connect()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st, err := s.JS().CreateOrUpdateStream(ctx, Stream(jetstream.StreamConfig{Name: "EVT", Subjects: []string{"evt.>"}}))
	if err != nil {
		t.Fatal(err)
	}
	if st.CachedInfo().Config.Replicas != 3 {
		t.Fatalf("replicas %d", st.CachedInfo().Config.Replicas)
	}
	kv, err := s.JS().CreateOrUpdateKeyValue(ctx, KV(jetstream.KeyValueConfig{Bucket: "prj"}))
	if err != nil {
		t.Fatal(err)
	}

	var last uint64
	publish := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			var ack *jetstream.PubAck
			var err error
			for try := 0; try < 50; try++ { // a leader election answers with a timeout, then a retry commits
				ack, err = s.JS().Publish(ctx, "evt.x", []byte(fmt.Sprint(last+1)),
					jetstream.WithExpectLastSequence(last), jetstream.WithMsgID(fmt.Sprintf("m%d", last+1)))
				if err == nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if err != nil {
				t.Fatalf("publish %d: %v", last+1, err)
			}
			last = ack.Sequence
			for try := 0; try < 50; try++ { // projectors retry a failed write the same way
				if _, err = kv.Put(ctx, "k", []byte(fmt.Sprint(last))); err == nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if err != nil {
				t.Fatalf("kv put: %v", err)
			}
		}
	}
	publish(5)

	leader := st.CachedInfo().Cluster.Leader
	for i := 0; i < c.Size(); i++ {
		if fmt.Sprintf("n%d", i) == leader {
			c.Stop(i)
			publish(5)
			c.Restart(i)
		}
	}
	c.Stop(2)
	publish(5)
	c.Restart(2)
	c.WaitReady()

	if last != 15 {
		t.Fatalf("last sequence %d, want 15", last)
	}
	e, err := kv.Get(ctx, "k")
	if err != nil || string(e.Value()) != "15" {
		t.Fatalf("kv %v %v", e, err)
	}
	// every node holds the stream again
	deadline := time.Now().Add(20 * time.Second)
	for {
		info, err := st.Info(ctx)
		if err == nil && info.Cluster != nil && len(info.Cluster.Replicas) == 2 && allCurrent(info.Cluster.Replicas) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("replicas did not catch up: %+v %v", info, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func allCurrent(peers []*jetstream.PeerInfo) bool {
	for _, p := range peers {
		if !p.Current {
			return false
		}
	}
	return true
}

// TestConnectOptions: credentials and a CA add their options.
func TestConnectOptions(t *testing.T) {
	if n := len(Options{}.connectOptions("t")); n != 2 {
		t.Fatalf("plain options %d", n)
	}
	if n := len(Options{Creds: "/x.creds", CA: "/ca.pem"}.connectOptions("t")); n != 4 {
		t.Fatalf("secure options %d", n)
	}
}

// TestSetReplicas: a stream and a KV bucket made with one replica on a
// cluster are raised to three.
func TestSetReplicas(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a three-node cluster")
	}
	c := StartTestCluster(t, 3)
	s := c.Connect()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.JS().CreateStream(ctx, jetstream.StreamConfig{Name: "A", Subjects: []string{"a.>"}, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JS().CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "b", Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	changed, err := SetReplicas(ctx, s.JS(), 3)
	if err != nil || len(changed) != 2 {
		t.Fatalf("changed %v, %v", changed, err)
	}
	st, _ := s.JS().Stream(ctx, "KV_b")
	if st.CachedInfo().Config.Replicas != 3 {
		t.Fatalf("bucket replicas %d", st.CachedInfo().Config.Replicas)
	}
	if again, _ := SetReplicas(ctx, s.JS(), 3); len(again) != 0 {
		t.Fatalf("a second run changed %v", again)
	}
}
