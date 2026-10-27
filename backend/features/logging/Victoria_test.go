package logging

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeVictoria records the lines posted to /insert/jsonline, failing while fail is set.
type fakeVictoria struct {
	mu    sync.Mutex
	lines []map[string]any
	fail  atomic.Bool
}

func (f *fakeVictoria) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/insert/jsonline" || r.Header.Get("Content-Type") != "application/stream+json" ||
		r.URL.Query().Get("_stream_fields") != StreamFields {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if f.fail.Load() {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	scanner := bufio.NewScanner(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	for scanner.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &rec); err == nil {
			f.lines = append(f.lines, rec)
		}
	}
}

func (f *fakeVictoria) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.lines)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVictoriaStoresRecords(t *testing.T) {
	fake := &fakeVictoria{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	v := NewVictoria(VictoriaOptions{Instance: "test", FlushEvery: 20 * time.Millisecond, Errors: io.Discard})
	defer v.Close(context.Background())
	var local bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	if err := Setup(&local, "text", slog.LevelInfo, v.Handler()); err != nil {
		t.Fatal(err)
	}

	ctx := WithContext(context.Background(), Trace("t1").Tenant("1"))
	In("index").Info(ctx, "before a target")
	if v.Status().Queued != 0 {
		t.Error("queued without a target")
	}

	v.SetTarget(Target{URL: srv.URL + "/"})
	v.SetLevel(slog.LevelWarn)
	In("index").Info(ctx, "below the stored level")
	In("index").Warn(ctx, "stored")
	waitFor(t, "the record", func() bool { return fake.count() == 1 })

	rec := fake.lines[0]
	want := map[string]any{"_msg": "stored", "level": "warn", "app": "foundation", "instance": "test",
		"source": "server", "tenant.id": "1", "trace": "t1", "domain": "index"}
	for k, val := range want {
		if rec[k] != val {
			t.Errorf("%s = %v, want %v", k, rec[k], val)
		}
	}
	if !bytes.Contains(local.Bytes(), []byte("before a target")) || !bytes.Contains(local.Bytes(), []byte("below the stored level")) {
		t.Errorf("stderr lost records: %s", local.String())
	}
}

func TestVictoriaRetriesAndDrops(t *testing.T) {
	fake := &fakeVictoria{}
	fake.fail.Store(true)
	srv := httptest.NewServer(fake)
	defer srv.Close()

	v := NewVictoria(VictoriaOptions{QueueSize: 5, BatchSize: 2, FlushEvery: 20 * time.Millisecond, Errors: io.Discard})
	defer v.Close(context.Background())
	v.SetTarget(Target{URL: srv.URL})
	for i := range 8 {
		v.Enqueue([]byte(`{"_msg":"m` + string(rune('0'+i)) + `"}`))
	}
	waitFor(t, "a failed post", func() bool { return v.Status().LastError != "" })
	if s := v.Status(); s.Dropped != 3 || s.Queued != 5 {
		t.Errorf("dropped %d, queued %d; want 3 and 5", s.Dropped, s.Queued)
	}

	fake.fail.Store(false)
	waitFor(t, "the retry", func() bool { return fake.count() == 5 })
	if first := fake.lines[0]["_msg"]; first != "m3" {
		t.Errorf("first stored %v, want m3 (the oldest were dropped)", first)
	}
	waitFor(t, "a clear status", func() bool { s := v.Status(); return s.LastError == "" && s.Queued == 0 })
}

func TestVictoriaCloseFlushes(t *testing.T) {
	fake := &fakeVictoria{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	v := NewVictoria(VictoriaOptions{FlushEvery: time.Hour, Errors: io.Discard})
	v.SetTarget(Target{URL: srv.URL})
	v.Enqueue([]byte(`{"_msg":"last words"}` + "\n"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v.Close(ctx)
	if fake.count() != 1 {
		t.Errorf("stored %d records on close, want 1", fake.count())
	}
}

func TestVictoriaUserLevels(t *testing.T) {
	fake := &fakeVictoria{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	v := NewVictoria(VictoriaOptions{Instance: "test", FlushEvery: 20 * time.Millisecond, Errors: io.Discard})
	defer v.Close(context.Background())
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	if err := Setup(io.Discard, "text", slog.LevelInfo, v.Handler()); err != nil {
		t.Fatal(err)
	}
	v.SetTarget(Target{URL: srv.URL})
	v.SetLevel(slog.LevelInfo)
	v.SetUserLevels(map[string]UserLevel{
		"7": {Level: slog.LevelDebug, Until: time.Now().Add(time.Hour)},
		"8": {Level: slog.LevelDebug, Until: time.Now().Add(-time.Minute)},
	})

	watched := WithContext(context.Background(), Trace("t1").User("7"))
	expired := WithContext(context.Background(), Trace("t2").User("8"))
	other := WithContext(context.Background(), Trace("t3").User("9"))
	In("chat").Debug(other, "another user's debug")
	In("chat").Debug(expired, "an expired override")
	In("chat").Debug(watched, "the watched user's debug")
	In("chat").Info(other, "info")
	waitFor(t, "the records", func() bool { return fake.count() == 2 })
	time.Sleep(60 * time.Millisecond)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.lines) != 2 || fake.lines[0]["_msg"] != "the watched user's debug" || fake.lines[1]["_msg"] != "info" {
		t.Errorf("stored %v", fake.lines)
	}
	if lvl, ok := v.UserLevel("7"); !ok || lvl != slog.LevelDebug {
		t.Errorf("UserLevel(7) = %v, %v", lvl, ok)
	}
	if _, ok := v.UserLevel("8"); ok {
		t.Error("an expired override still applies")
	}
}
