package jobs_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

var fast = jobs.Settings{MaxConcurrent: 4, MaxAttempts: 3, BackoffSeconds: 1, MaxBackoffSeconds: 1, DoneHours: 24}

// A job enqueued while no runner serves the tenant runs once a runner
// starts, once.
func TestJobRunsOnceAfterRestart(t *testing.T) {
	f := setup(t, fast)
	f.publishGraph("work", action("a", nil))
	j := f.enqueue(jobs.Request{ID: "first", Graph: "work", Inputs: json.RawMessage(`{"value":{"n":1}}`)})
	if j.Status != jobs.StatusQueued || j.As != features.LocalPrincipal().Ref() || j.Queue != jobs.DefaultQueue {
		t.Fatalf("enqueued: %+v", j)
	}
	// A runner that stops before the job's message reaches it
	r := f.start("one", time.Second)
	r.Close()
	f.hits.mu.Lock()
	early := len(f.hits.list)
	f.hits.mu.Unlock()

	f.start("two", time.Second)
	got := f.hits.wait(t, "a", 1, 5*time.Second)
	j = f.until("first", "done", 3*time.Second, status(jobs.StatusDone))
	time.Sleep(500 * time.Millisecond)
	if got = f.hits.named("a"); len(got) != 1 || early > 1 {
		t.Fatalf("ran %d times", len(got))
	}
	if got[0].Principal != features.LocalPrincipal() || got[0].Job != "first" || got[0].Attempt != 1 {
		t.Errorf("hit: %+v", got[0])
	}
	if v, _ := got[0].Value.(map[string]any); v["n"] != float64(1) {
		t.Errorf("inputs: %+v", got[0].Value)
	}
	if len(j.Runs) != 1 || j.Runs[0].Status != jobs.RunOK || j.Runs[0].RunID == 0 || j.Attempts != 1 {
		t.Errorf("runs: %+v", j)
	}

	// The same id again is dropped, now and after it ran
	f.enqueue(jobs.Request{ID: "first", Graph: "work"})
	time.Sleep(700 * time.Millisecond)
	if len(f.hits.named("a")) != 1 || f.job("first").Dispatch != 1 {
		t.Errorf("a duplicate ran: %+v", f.job("first"))
	}
}

// A runner killed mid-run leaves its job to another, which runs it again;
// the graph's command, keyed by the job's id, commits once.
func TestKilledMidRunRunsAgain(t *testing.T) {
	f := setup(t, fast)
	res := f.ok("entity.create", "note", map[string]any{"name": "Note", "fields": []map[string]any{{"name": "text", "type": "text"}}})
	res = f.ok("entity.publish", "note", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.note", res.Seq)
	_ = res
	// Create a note at the job's id, then hit (blocking the first time)
	g := map[string]any{"name": "Keyed", "kind": livenodes.KindAction,
		"nodes": []any{
			node("event", "event.call", nil),
			node("info", "jobs.info", nil),
			node("args", "data.expression", map[string]any{"expr": `{entity: "note", values: {text: "once"}}`, "type": "json"}),
			node("send", "live.sendCommand", map[string]any{"command": "record.create"}),
			node("hit", "test.hit", map[string]any{"name": "keyed", "block": true}),
		},
		"edges": edges("event.then -> send.then", "info.id -> send.target", "args.value -> send.args", "send.ok -> hit.then", "send.rejected -> hit.then"),
	}
	f.publishGraph("keyed", g)
	a := f.start("a", 700*time.Millisecond)
	f.enqueue(jobs.Request{ID: "k1", Graph: "keyed"})
	f.until("k1", "running", 5*time.Second, func(j jobs.Job) bool { return j.Status == jobs.StatusRunning && j.Runs[0].Runner == "a" })
	// The note commits, then the run blocks; the runner dies
	deadline := time.Now().Add(3 * time.Second)
	for f.note("k1") == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	a.Close()
	f.start("b", 700*time.Millisecond)
	j := f.until("k1", "done", 8*time.Second, status(jobs.StatusDone))
	if j.Attempts != 2 || len(j.Runs) != 2 || j.Runs[0].Status != jobs.RunLost || j.Runs[1].Runner != "b" {
		t.Errorf("runs: %+v", j.Runs)
	}
	if got := f.hits.named("keyed"); len(got) != 1 || got[0].Attempt != 2 {
		t.Errorf("hits: %+v", got)
	}
	var page map[string]any
	raw, _ := json.Marshal(map[string]any{"entity": "note"})
	v, _, fail := f.subs.Once(args(), "records.list", raw)
	if fail != nil {
		t.Fatal(fail.Message)
	}
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &page)
	if len(page) != 1 {
		t.Errorf("notes: %d, want 1", len(page))
	}
}

func (f *fixture) note(id string) map[string]any {
	raw, _ := json.Marshal(map[string]any{"entity": "note", "id": id})
	v, _, fail := f.subs.Once(args(), "records.get", raw)
	if fail != nil || v == nil {
		return nil
	}
	m, _ := v.(map[string]any)
	if len(m) == 0 {
		return nil
	}
	return m
}

// A retryable failure is retried with backoff until the job is dead;
// Retry runs it again. A failure that is not retryable is dead at once.
func TestRetriesBackOffThenDead(t *testing.T) {
	f := setup(t, fast)
	fail := func(retryable bool) map[string]any {
		return map[string]any{"name": "Failing", "kind": livenodes.KindAction,
			"nodes": []any{
				node("event", "event.call", nil),
				node("hit", "test.hit", map[string]any{"name": fmt.Sprint("fail-", retryable)}),
				node("msg", "data.expression", map[string]any{"expr": `"the server is busy"`, "type": "string"}),
				node("fail", "jobs.fail", map[string]any{"retryable": retryable}),
			},
			"edges": edges("event.then -> hit.then", "hit.then -> fail.then", "msg.value -> fail.message"),
		}
	}
	f.publishGraph("flaky", fail(true))
	f.publishGraph("broken", fail(false))
	f.start("r", time.Second)
	f.enqueue(jobs.Request{ID: "flaky1", Graph: "flaky"})
	f.enqueue(jobs.Request{ID: "broken1", Graph: "broken"})

	j := f.until("broken1", "dead", 5*time.Second, status(jobs.StatusDead))
	if j.Attempts != 1 || j.Error == "" {
		t.Errorf("broken: %+v", j)
	}
	j = f.until("flaky1", "dead", 10*time.Second, status(jobs.StatusDead))
	got := f.hits.named("fail-true")
	if j.Attempts != 3 || len(got) != 3 || len(j.Runs) != 3 {
		t.Fatalf("flaky: %d attempts, %d hits", j.Attempts, len(got))
	}
	for i := 1; i < len(got); i++ {
		if gap := got[i].Start.Sub(got[i-1].Start); gap < 900*time.Millisecond {
			t.Errorf("retry %d came after %v, before its backoff", i, gap)
		}
	}
	if j.Runs[0].Status != jobs.RunFailed || j.Runs[0].Error == "" {
		t.Errorf("a failed run: %+v", j.Runs[0])
	}
	// Its trace says which node failed and why
	raw, _ := json.Marshal(map[string]any{"id": "flaky1", "runId": fmt.Sprint(j.Runs[0].RunID)})
	v, _, qfail := f.subs.Once(args(), "jobs.trace", raw)
	if qfail != nil {
		t.Fatalf("jobs.trace: %s", qfail.Message)
	}
	b, _ := json.Marshal(v)
	var trace struct {
		Run   struct{ Status, FailedNode string }
		Steps []struct{ NodeID, Error string }
	}
	_ = json.Unmarshal(b, &trace)
	if trace.Run.Status != "failed" || trace.Run.FailedNode != "fail" || len(trace.Steps) == 0 || trace.Steps[len(trace.Steps)-1].Error == "" {
		t.Errorf("trace: %s", b)
	}

	// Retry: the job runs again, its attempts counted afresh
	f.ok("job.retry", "flaky1", map[string]any{})
	f.hits.wait(t, "fail-true", 4, 5*time.Second)
	j = f.until("flaky1", "dead again", 10*time.Second, func(j jobs.Job) bool { return j.Status == jobs.StatusDead && len(j.Runs) == 6 })
	if j.By != "user:1" {
		t.Errorf("retried by %q", j.By)
	}
	// Only a dead or canceled job is retried
	if res := f.do("job.retry", "nope", map[string]any{}); res.Common.Code != 422 {
		t.Errorf("retrying no job: %d", res.Common.Code)
	}
}

// A delayed job waits; canceled before it runs, it never does.
func TestDelayAndCancel(t *testing.T) {
	f := setup(t, fast)
	f.publishGraph("work", action("later", nil))
	f.start("r", time.Second)
	f.enqueue(jobs.Request{ID: "soon", Graph: "work", RunAt: time.Now().Add(1500 * time.Millisecond)})
	f.enqueue(jobs.Request{ID: "never", Graph: "work", RunAt: time.Now().Add(1500 * time.Millisecond)})
	f.ok("job.cancel", "never", map[string]any{})
	start := time.Now()
	got := f.hits.wait(t, "later", 1, 6*time.Second)
	if waited := time.Since(start); waited < time.Second {
		t.Errorf("the delayed job ran after %v", waited)
	}
	time.Sleep(time.Second)
	if got = f.hits.named("later"); len(got) != 1 || got[0].Job != "soon" {
		t.Errorf("hits: %+v", got)
	}
	if j := f.job("never"); j.Status != jobs.StatusCanceled || j.By != "user:1" {
		t.Errorf("canceled: %+v", j)
	}
	// A done job cannot be canceled
	if res := f.do("job.cancel", "soon", map[string]any{}); res.Common.Code != 422 || res.Problems[0].Code != "job_started" {
		t.Errorf("canceling a done job: %d %+v", res.Common.Code, res.Problems)
	}
	// Someone else's job is not yours to cancel
	other := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: features.LocalTenantID}
	j, err := f.runner.Enqueue(features.ServiceArgs{Ctx: args().Ctx, Principal: other}, jobs.Request{ID: "theirs", Graph: "work", RunAt: time.Now().Add(time.Hour)})
	if err != nil || j.As.UserID != 7 {
		t.Fatalf("their job: %+v %v", j, err)
	}
	// (no policy is installed in this test, so job.manage is held by all;
	// mayAct is tested through the principal check)
	if _, err := f.runner.Enqueue(args(), jobs.Request{ID: "spoof", Graph: "work", As: other.Ref()}); err == nil {
		t.Error("a user enqueued a job as someone else")
	}
}

// Jobs with one key never overlap and run in order; jobs without keys
// run side by side, on several runners.
func TestKeyedJobsNeverOverlap(t *testing.T) {
	f := setup(t, fast)
	f.publishGraph("slow", action("slow", map[string]any{"sleepMs": 150}))
	f.start("a", time.Second)
	f.start("b", time.Second)
	for i := range 5 {
		f.enqueue(jobs.Request{ID: fmt.Sprintf("k%d", i), Graph: "slow", Key: "account-1"})
	}
	got := f.hits.wait(t, "slow", 5, 10*time.Second)
	slices.SortFunc(got, func(a, b hit) int { return a.Start.Compare(b.Start) })
	for i := range got {
		if got[i].Job != fmt.Sprintf("k%d", i) {
			t.Errorf("run %d was %s", i, got[i].Job)
		}
		if i > 0 && got[i].Start.Before(got[i-1].End) {
			t.Errorf("%s started before %s ended", got[i].Job, got[i-1].Job)
		}
	}
	// Unkeyed jobs overlap
	for i := range 4 {
		f.enqueue(jobs.Request{ID: fmt.Sprintf("u%d", i), Graph: "slow"})
	}
	got = f.hits.wait(t, "slow", 9, 10*time.Second)[5:]
	slices.SortFunc(got, func(a, b hit) int { return a.Start.Compare(b.Start) })
	if !got[1].Start.Before(got[0].End) {
		t.Error("unkeyed jobs ran one at a time")
	}
}

// Many jobs over three runners each run once.
func TestManyRunnersRunEachOnce(t *testing.T) {
	f := setup(t, fast)
	f.publishGraph("work", action("many", map[string]any{"sleepMs": 20}))
	for _, n := range []string{"a", "b", "c"} {
		f.start(n, time.Second)
	}
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() { f.enqueue(jobs.Request{ID: fmt.Sprintf("m%d", i), Graph: "work"}) })
	}
	wg.Wait()
	f.hits.wait(t, "many", 30, 15*time.Second)
	time.Sleep(500 * time.Millisecond)
	got := f.hits.named("many")
	seen := map[string]int{}
	runners := map[string]bool{}
	for _, h := range got {
		seen[h.Job]++
	}
	for i := range 30 {
		id := fmt.Sprintf("m%d", i)
		if seen[id] != 1 {
			t.Errorf("%s ran %d times", id, seen[id])
		}
		j := f.until(id, "done", 3*time.Second, status(jobs.StatusDone))
		runners[j.Runs[0].Runner] = true
	}
	if len(runners) < 2 {
		t.Errorf("one runner ran everything: %v", runners)
	}
}

// A graph that does not exist is refused when enqueued.
func TestEnqueueChecksTheGraph(t *testing.T) {
	f := setup(t, fast)
	if _, err := f.runner.Enqueue(args(), jobs.Request{ID: "x", Graph: "missing"}); err == nil {
		t.Error("a job of no graph was enqueued")
	}
	_ = data.Projector
}
