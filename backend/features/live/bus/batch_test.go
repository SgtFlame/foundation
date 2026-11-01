package bus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Accounts: test.transfer debits its target and credits another account it
// reads, two targets in one batch; test.deposit changes one account.

var area = live.AppArea("bank")

type account struct {
	Balance int    `json:"balance"`
	Last    string `json:"last"` // the command that last changed it
}

type amount struct {
	Amount int `json:"amount"`
}

func bank(t *testing.T, beforeCommit func(try int)) (*bus.Bus, *project.Projections, *natsd.Server) {
	reg := extensions.NewRegistry()
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: "bank.account"}, Title: "Accounts", Area: area, Types: []string{"account"},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var a account
			if f.State != nil {
				_ = json.Unmarshal(f.State, &a)
			}
			var m amount
			_ = json.Unmarshal(f.Event.Data, &m)
			if f.Event.Type == "debited" {
				m.Amount = -m.Amount
			}
			a.Balance += m.Amount
			a.Last = f.Event.CommandID
			return json.Marshal(a)
		},
	})
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: "bank.deposit"}, Title: "Deposit", Area: area, TargetType: "account",
		Projection: "bank.account", Action: "bank.write", Events: []string{"credited"},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var m amount
			_ = json.Unmarshal(d.Args, &m)
			return []live.NewEvent{{Type: "credited", Data: m}}, nil
		},
	})
	var tries atomic.Int32
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: "bank.transfer"}, Title: "Transfer", Area: area, TargetType: "account",
		Projection: "bank.account", Action: "bank.write", Events: []string{"debited", "credited"},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var a struct {
				To     string `json:"to"`
				Amount int    `json:"amount"`
			}
			_ = json.Unmarshal(d.Args, &a)
			var from account
			if d.State != nil {
				_ = json.Unmarshal(d.State, &from)
			}
			if from.Balance < a.Amount {
				return nil, []live.Problem{{Message: "not enough"}}
			}
			to := live.Target{Area: area, Type: "account", ID: a.To}
			if _, err := d.Read("bank.account", to); err != nil {
				return nil, []live.Problem{{Message: err.Error()}}
			}
			if beforeCommit != nil {
				beforeCommit(int(tries.Add(1)))
			}
			return []live.NewEvent{{Type: "debited", Data: amount{a.Amount}}, {Type: "credited", Data: amount{a.Amount}, Target: &to}}, nil
		},
	})
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	return bus.New(p), p, srv
}

func send(b *bus.Bus, id, typ, target string, args any) bus.Result {
	raw, _ := json.Marshal(args)
	return b.Execute(bus.Request{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}, ID: id, Type: typ, Target: target, Args: raw})
}

func balance(t *testing.T, p *project.Projections, id string, seq uint64) account {
	t.Helper()
	v, err := p.WaitFor(context.Background(), features.LocalTenantID, "bank.account", "account."+id, seq, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var a account
	_ = json.Unmarshal(v.Value, &a)
	return a
}

// events is every event in the bank's stream, in order.
func events(t *testing.T, srv *natsd.Server) []live.Event {
	t.Helper()
	s, err := srv.JS().Stream(context.Background(), live.StreamName(features.LocalTenantID, area))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := s.Info(context.Background())
	var out []live.Event
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
		m, err := s.GetMsg(context.Background(), seq)
		if err != nil {
			t.Fatal(err)
		}
		var e live.Event
		_ = json.Unmarshal(m.Data, &e)
		e.Seq = seq
		out = append(out, e)
	}
	return out
}

func ok(t *testing.T, res bus.Result) bus.Result {
	t.Helper()
	if res.Common.Code != 200 {
		t.Fatalf("command failed: %+v", res)
	}
	return res
}

func TestTransferCommitsOneBatch(t *testing.T) {
	b, p, srv := bank(t, nil)
	ok(t, send(b, "d1", "bank.deposit", "a", amount{10}))
	res := ok(t, send(b, "t1", "bank.transfer", "a", map[string]any{"to": "b", "amount": 4}))
	if a, bb := balance(t, p, "a", res.Seq-1), balance(t, p, "b", res.Seq); a.Balance != 6 || bb.Balance != 4 {
		t.Fatalf("balances %d and %d, want 6 and 4", a.Balance, bb.Balance)
	}
	evs := events(t, srv)
	if len(evs) != 3 {
		t.Fatalf("%d events, want 3", len(evs))
	}
	for i, e := range evs[1:] {
		if e.Batch == nil || e.Batch.Index != i+1 || e.Batch.Count != 2 || e.CommandID != "t1" {
			t.Fatalf("event %d is not in the batch: %+v", e.Seq, e)
		}
	}
	if evs[2].Target.ID != "b" || res.Seq != evs[2].Seq {
		t.Fatalf("the credit is %+v, the ack %d", evs[2], res.Seq)
	}
	// Resent, the batch is a duplicate and changes nothing
	again := ok(t, send(b, "t1", "bank.transfer", "a", map[string]any{"to": "b", "amount": 4}))
	if !again.Duplicate || again.Seq < res.Seq || len(events(t, srv)) != 3 {
		t.Fatalf("resent transfer: %+v, %d events", again, len(events(t, srv)))
	}
}

// Acceptance (forge 03h): a batch that fails its sequence check leaves
// nothing committed, and is decided again and committed whole.
func TestConflictingBatchCommitsNothingThenWhole(t *testing.T) {
	var b *bus.Bus
	b, p, srv := bank(t, func(try int) {
		if try == 1 { // another connection changes the account credited
			if res := send(b, "other", "bank.deposit", "b", amount{100}); res.Common.Code != 200 {
				t.Errorf("deposit failed: %+v", res)
			}
		}
	})
	ok(t, send(b, "d1", "bank.deposit", "a", amount{10}))
	res := ok(t, send(b, "t1", "bank.transfer", "a", map[string]any{"to": "b", "amount": 4}))
	evs := events(t, srv)
	var types []string
	for _, e := range evs {
		types = append(types, e.CommandID+":"+e.Type)
	}
	if fmt.Sprint(types) != "[d1:credited other:credited t1:debited t1:credited]" {
		t.Fatalf("the stream holds %v", types)
	}
	if a, bb := balance(t, p, "a", res.Seq-1), balance(t, p, "b", res.Seq); a.Balance != 6 || bb.Balance != 104 {
		t.Fatalf("balances %d and %d, want 6 and 104", a.Balance, bb.Balance)
	}
}

func TestBatchRefusedByOneSubjectStoresNothing(t *testing.T) {
	b, _, srv := bank(t, nil)
	ok(t, send(b, "d1", "bank.deposit", "a", amount{10}))
	ok(t, send(b, "d2", "bank.deposit", "b", amount{10}))
	msg := func(id string, seq uint64) *nats.Msg {
		m := nats.NewMsg(fmt.Sprintf("evt.%d.%s.account.%s.credited", features.LocalTenantID, area, id))
		m.Data = []byte(`{"type":"credited","target":{"area":"app_bank","type":"account","id":"` + id + `"},"data":{"amount":1},"tenantId":"1"}`)
		m.Header.Set(jetstream.ExpectedLastSubjSeqHeader, fmt.Sprint(seq))
		m.Header.Set(jetstream.ExpectedLastSubjSeqSubjHeader, fmt.Sprintf("evt.%d.%s.account.%s.*", features.LocalTenantID, area, id))
		return m
	}
	// a is at 1, b at 2: the second message's expectation is stale
	_, err := project.PublishBatch(context.Background(), srv.Conn(), []*nats.Msg{msg("a", 1), msg("b", 1)})
	if !project.IsConflict(err) {
		t.Fatalf("got %v, want a wrong last sequence", err)
	}
	if n := len(events(t, srv)); n != 2 {
		t.Fatalf("%d events after a refused batch, want 2", n)
	}
	if _, err := project.PublishBatch(context.Background(), srv.Conn(), []*nats.Msg{msg("a", 1), msg("b", 2)}); err != nil {
		t.Fatal(err)
	}
	if n := len(events(t, srv)); n != 4 {
		t.Fatalf("%d events, want 4", n)
	}
}

// A subscriber to both accounts never sees one side of a transfer without
// the other: the projector writes both values as one batch, so when the
// first arrives the second is already there, and nothing comes between.
func TestSubscriberNeverSeesHalfATransfer(t *testing.T) {
	b, p, _ := bank(t, nil)
	balance(t, p, "a", ok(t, send(b, "d1", "bank.deposit", "a", amount{1000})).Seq)
	balance(t, p, "b", ok(t, send(b, "d2", "bank.deposit", "b", amount{1000})).Seq)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entries, err := p.Watch(ctx, features.LocalTenantID, "bank.account", "account.*")
	if err != nil {
		t.Fatal(err)
	}
	const n = 60
	seen := make(chan error, 1)
	go func() {
		started, count := false, 0
		var half *live.Entry
		for e := range entries {
			if e == nil {
				started = true
				continue
			}
			if !started {
				continue
			}
			var a account
			_ = json.Unmarshal(e.Value.Value, &a)
			if half == nil {
				half = e
				// The other side is stored already
				other := "account.a"
				if e.Key == other {
					other = "account.b"
				}
				v, _, err := p.Get(context.Background(), features.LocalTenantID, "bank.account", other)
				var o account
				_ = json.Unmarshal(v.Value, &o)
				if err != nil || o.Last != a.Last {
					seen <- fmt.Errorf("saw %s from %s while %s was from %s", e.Key, a.Last, other, o.Last)
					return
				}
				continue
			}
			var h account
			_ = json.Unmarshal(half.Value.Value, &h)
			if h.Last != a.Last || half.Key == e.Key {
				seen <- fmt.Errorf("%s from %s came between %s's sides", e.Key, a.Last, h.Last)
				return
			}
			half = nil
			if count++; count == n {
				seen <- nil
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			from, to := "a", "b"
			if i%2 == 1 {
				from, to = to, from
			}
			if res := send(b, fmt.Sprintf("t%d", i), "bank.transfer", from, map[string]any{"to": to, "amount": 1}); res.Common.Code != 200 {
				t.Errorf("transfer failed: %+v", res)
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-seen:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the subscriber did not see every transfer")
	}
}

// Streams and buckets made before atomic batches are upgraded when first
// used.
func TestBatchOnStreamsMadeBefore(t *testing.T) {
	b, p, srv := bank(t, nil)
	ctx := context.Background()
	tenant := features.LocalTenantID
	if _, err := srv.JS().CreateStream(ctx, jetstream.StreamConfig{Name: live.StreamName(tenant, area), Subjects: []string{live.StreamSubjects(tenant, area)}, Duplicates: project.Duplicates}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.JS().CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: live.Bucket(tenant, "bank.account"), History: 1}); err != nil {
		t.Fatal(err)
	}
	ok(t, send(b, "d1", "bank.deposit", "a", amount{10}))
	res := ok(t, send(b, "t1", "bank.transfer", "a", map[string]any{"to": "b", "amount": 4}))
	if a, bb := balance(t, p, "a", res.Seq-1), balance(t, p, "b", res.Seq); a.Balance != 6 || bb.Balance != 4 {
		t.Fatalf("balances %d and %d, want 6 and 4", a.Balance, bb.Balance)
	}
}
