package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/samber/oops"
)

// capture sends records to a buffer as JSON for the rest of the test.
func capture(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	if err := Setup(&buf, "json", level); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestLogFields(t *testing.T) {
	buf := capture(t, slog.LevelInfo)
	ctx := WithContext(context.Background(), Trace("t1").User("7").Tenant("1"))

	In("index").Tags("reindex").With("files", 3).Info(ctx, "reindex of %d files", 3)
	In("index").Debug(ctx, "not stored below info")

	recs := records(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	rec := recs[0]
	want := map[string]any{
		"_msg": "reindex of 3 files", "level": "info", "domain": "index",
		"trace": "t1", "user.id": "7", "tenant.id": "1", "attr.files": float64(3),
	}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("%s = %v, want %v", k, rec[k], v)
		}
	}
	if _, ok := rec["_time"]; !ok {
		t.Error("no _time")
	}
	if tags, _ := rec["tags"].([]any); len(tags) != 1 || tags[0] != "reindex" {
		t.Errorf("tags = %v", rec["tags"])
	}
}

func TestErrorKeepsContext(t *testing.T) {
	buf := capture(t, slog.LevelDebug)
	ctx := WithContext(context.Background(), Trace("t2"))

	cause := errors.New("disk full")
	err := In("kb").Code("volume_write_failed").With("volumeId", 9).Public("Cannot save").Wrapf(cause, "writing marker")
	if o, ok := oops.AsOops(err); !ok || o.Code() != "volume_write_failed" || o.Public() != "Cannot save" {
		t.Fatalf("not an oops error with the builder's context: %#v", err)
	}
	if !errors.Is(err, cause) {
		t.Error("the cause is not wrapped")
	}

	Tags("serve").Error(ctx, err, "save failed")
	rec := records(t, buf)[0]
	want := map[string]any{
		"_msg": "save failed", "level": "error", "domain": "kb", "trace": "t2",
		"attr.volumeId": float64(9), "error": "writing marker: disk full",
		"error.code": "volume_write_failed", "error.domain": "kb", "error.cause": "disk full",
	}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("%s = %v, want %v", k, rec[k], v)
		}
	}
	if stack, _ := rec["error.stack"].(string); !strings.Contains(stack, "Builder_test.go") {
		t.Errorf("the stack does not start at the caller: %q", stack)
	}
}

func TestContextBuilderMakesErrors(t *testing.T) {
	ctx := WithContext(context.Background(), Trace("t3"))
	if TraceFrom(ctx) != "t3" {
		t.Errorf("TraceFrom = %q", TraceFrom(ctx))
	}
	o, _ := oops.AsOops(oops.FromContext(ctx).Errorf("x"))
	if o.Trace() != "t3" {
		t.Errorf("oops.FromContext trace = %q", o.Trace())
	}
	if id := NewTraceID(); len(id) != 32 {
		t.Errorf("trace ID %q", id)
	}
}
