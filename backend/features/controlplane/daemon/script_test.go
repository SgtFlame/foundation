package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
)

// events gives a request's events on CTL in order.
func events(t *testing.T, e *env, request string) []ctl.Event {
	t.Helper()
	st, err := e.ctl.JS.Stream(context.Background(), ctl.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := st.Info(context.Background())
	var out []ctl.Event
	for seq := uint64(1); info != nil && seq <= info.State.LastSeq; seq++ {
		m, err := st.GetMsg(context.Background(), seq)
		if err != nil {
			continue
		}
		var ev ctl.Event
		_ = json.Unmarshal(m.Data, &ev)
		if ev.Request == request {
			out = append(out, ev)
		}
	}
	return out
}

func outcome(t *testing.T, e *env, request string) ctl.Event {
	t.Helper()
	var last ctl.Event
	until(t, request+" ends", func() bool {
		for _, ev := range events(t, e, request) {
			if ev.Kind == ctl.EventDone || ev.Kind == ctl.EventFailed {
				last = ev
				return true
			}
		}
		return false
	})
	return last
}

// TestScripts: only scripts whose bytes match their hash and whose
// signature is by a trusted key run, once per request id, with their
// output and exit code on CTL, killed past their timeout; a rotated key
// in desired state refuses scripts the old key signed.
func TestScripts(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	dir := t.TempDir()
	oldKey, err := ctl.LoadOrMakeSigningKey(filepath.Join(dir, "old.key"))
	if err != nil {
		t.Fatal(err)
	}
	newKey, _ := ctl.MakeSigningKey(filepath.Join(dir, "new.key"))
	h1 := ctl.Target{Kind: ctl.KindHost, ID: "h1"}
	c := e.enrolled(t, h1)
	c.ScriptKeys = []string{ctl.PublicKey(oldKey)}
	d := &Daemon{Config: c}
	if err := d.Connect(nil); err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = d.Run(rctx) }()

	good, err := e.ctl.PutScript(ctx, oldKey, "#!/usr/bin/env bash\nset -euo pipefail\nwho=$(sed -E 's/.*\"who\": *\"([^\"]*)\".*/\\1/' \"$1\")\necho \"hello $who\"\necho \"secret=${SECRET:-unset} home=${HOME:-unset}\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ctl.PutScript(ctx, oldKey, "echo no shebang\n"); err == nil {
		t.Fatal("kept a script without its header")
	}
	send := func(id string, a ScriptArgs) {
		t.Helper()
		raw, _ := json.Marshal(a)
		for try := 0; ; try++ {
			_, err := e.ctl.Send(ctx, h1, ctl.Request{ID: id, Verb: "script", Args: raw})
			if err == nil {
				return
			}
			if try > 50 {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	send("run1", ScriptArgs{Hash: good, Args: json.RawMessage(`{"who": "world"}`), Env: map[string]string{"SECRET": "s3"}})
	if ev := outcome(t, e, "run1"); ev.Kind != ctl.EventDone {
		t.Fatalf("good script: %+v", ev)
	}
	var lines []string
	for _, ev := range events(t, e, "run1") {
		if ev.Kind == ctl.EventProgress {
			lines = append(lines, ev.Message)
		}
	}
	if strings.Join(lines, "|") != "hello world|secret=s3 home=unset" {
		t.Fatalf("output %q", lines)
	}
	// The same request id again: not run again
	send("run1", ScriptArgs{Hash: good})
	time.Sleep(300 * time.Millisecond)
	n := 0
	for _, ev := range events(t, e, "run1") {
		if ev.Kind == ctl.EventAccepted {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("run %d times", n)
	}

	// Exit codes and timeouts
	failing, _ := e.ctl.PutScript(ctx, oldKey, "#!/usr/bin/env bash\nset -euo pipefail\necho before\nexit 3\n")
	send("fail1", ScriptArgs{Hash: failing})
	if ev := outcome(t, e, "fail1"); ev.Kind != ctl.EventFailed || !strings.Contains(ev.Message, "3") {
		t.Fatalf("failing script: %+v", ev)
	}
	slow, _ := e.ctl.PutScript(ctx, oldKey, "#!/usr/bin/env bash\nset -euo pipefail\nsleep 30\n")
	start := time.Now()
	send("slow1", ScriptArgs{Hash: slow, TimeoutSeconds: 1})
	if ev := outcome(t, e, "slow1"); ev.Kind != ctl.EventFailed || !strings.Contains(ev.Message, "killed") || time.Since(start) > 10*time.Second {
		t.Fatalf("slow script: %+v", ev)
	}

	// Unsigned, changed and unknown scripts are refused
	kv, _ := e.ctl.JS.KeyValue(ctx, ctl.ScriptsBucket)
	content := "#!/usr/bin/env bash\nset -euo pipefail\necho unsigned\n"
	unsigned := ctl.HashOf([]byte(content))
	raw, _ := json.Marshal(ctl.Script{Content: content, Signature: base64.StdEncoding.EncodeToString(make([]byte, 64))})
	_, _ = kv.Put(ctx, ctl.ScriptKey(unsigned), raw)
	e1, _ := kv.Get(ctx, ctl.ScriptKey(good))
	var s ctl.Script
	_ = json.Unmarshal(e1.Value(), &s)
	s.Content += "rm -rf /tmp/x\n"
	raw, _ = json.Marshal(s)
	_, _ = kv.Put(ctx, ctl.ScriptKey(failing), raw) // bytes no longer match the name
	for id, hash := range map[string]string{"unsigned": unsigned, "changed": failing, "unknown": "sha256:00"} {
		send(id, ScriptArgs{Hash: hash})
		if ev := outcome(t, e, id); ev.Kind != ctl.EventFailed {
			t.Fatalf("%s script ran: %+v", id, ev)
		}
	}

	// Rotation: desired state trusts only the new key
	if _, err := e.ctl.PutDesired(ctx, h1, json.RawMessage(`{"scriptKeys":["`+ctl.PublicKey(newKey)+`"]}`)); err != nil {
		t.Fatal(err)
	}
	send("old1", ScriptArgs{Hash: good})
	if ev := outcome(t, e, "old1"); ev.Kind != ctl.EventFailed || !strings.Contains(ev.Message, "trusted") {
		t.Fatalf("old key after rotation: %+v", ev)
	}
	resigned, _ := e.ctl.PutScript(ctx, newKey, "#!/usr/bin/env bash\nset -euo pipefail\necho new\n")
	send("new1", ScriptArgs{Hash: resigned})
	if ev := outcome(t, e, "new1"); ev.Kind != ctl.EventDone {
		t.Fatalf("new key: %+v", ev)
	}
}
