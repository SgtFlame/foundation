package msgtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/messaging"
	"github.com/SgtFlame/foundation/backend/features/messaging/contracttest"
	"github.com/SgtFlame/foundation/backend/features/messaging/msgtest"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func ctxFor(contract string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: contract, Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: 1}}}
}

func open(t *testing.T, s *msgtest.Server, conn, settings string) adapter.Session {
	t.Helper()
	sess, err := s.Impl().Open(ctxFor(messaging.ContractRef), adapter.Connection{ID: conn, Type: msgtest.TypeID, Tenant: 1, Settings: json.RawMessage(settings)})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func caller(sess adapter.Session, contract string) contracttest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		c := ctxFor(contract)
		c.Operation = op
		return sess.Call(c, op, raw)
	}
}

func TestContractSuite(t *testing.T) {
	sess := open(t, msgtest.New(), "c1", `{"user":"u-tony","name":"Tony"}`)
	control := caller(sess, msgtest.ControlRef)
	contracttest.Run(t, caller(sess, messaging.ContractRef), contracttest.Options{Self: "u-tony", Expire: func() error {
		_, err := control("expireCursor", map[string]any{})
		return err
	}})
}

func TestContractsPassTheChecks(t *testing.T) {
	for _, c := range []connectors.Contract{messaging.Contract(), msgtest.ControlContract()} {
		if ps := connectors.CheckContract(c); len(ps) > 0 {
			t.Fatalf("%s: %v", c.Ref(), ps)
		}
	}
	if ps := connectors.CheckType(msgtest.Type(), func(ref string) (connectors.Contract, bool) {
		switch ref {
		case messaging.ContractRef:
			return messaging.Contract(), true
		case msgtest.ControlRef:
			return msgtest.ControlContract(), true
		}
		return connectors.Contract{}, false
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}

// Every chat.messaging operation maps to the same tool spec and vendor
// functions as its golden files (testdata/golden, -update to accept a
// deliberate change).
func TestToolShapes(t *testing.T) {
	c := messaging.Contract()
	for _, op := range c.Operations {
		spec, _ := json.Marshal(connectors.ToolSpec(c, op))
		golden(t, op.ID+".toolspec.json", spec)
		golden(t, op.ID+".mcp.json", connectors.MCPTool(c, op))
		for _, vendor := range []string{connectors.VendorGemini, connectors.VendorAnthropic, connectors.VendorOpenAI} {
			f, err := connectors.FunctionSpec(c, op, vendor)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, op.ID+"."+vendor+".json", f)
		}
		if name := connectors.WireName(connectors.ToolName(c.ID, op.ID)); len(name) > connectors.MaxToolName || strings.Contains(name, ".") {
			t.Errorf("wire name %q", name)
		}
	}
}

func golden(t *testing.T, name string, got json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, got, "", "  "); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	buf.WriteByte('\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", name, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("%s differs from its golden file (run with -update to accept):\n%s", name, buf.String())
	}
}

func TestFaultsAndOwnMessages(t *testing.T) {
	s := msgtest.New()
	a := open(t, s, "a", `{"workspace":"acme","user":"u-anna","name":"Anna"}`)
	b := open(t, s, "b", `{"workspace":"acme","user":"u-ben","name":"Ben"}`)
	ma, ctl := caller(a, messaging.ContractRef), caller(a, msgtest.ControlRef)
	if _, err := ctl("fail", map[string]any{"operation": "message.post", "times": 1, "kind": "rateLimited"}); err != nil {
		t.Fatal(err)
	}
	post := map[string]any{"channel": "general", "key": "k1", "text": "hi @u-ben"}
	_, err := ma("message.post", post)
	var ce *connectors.Error
	if !errors.As(err, &ce) || ce.Code != connectors.CodeRateLimited || !ce.Retryable() {
		t.Fatalf("%v", err)
	}
	raw, err := ma("message.post", post)
	if err != nil {
		t.Fatal(err)
	}
	var m messaging.Message
	_ = json.Unmarshal(raw, &m)
	if len(m.Mentions) != 1 || m.Mentions[0] != "u-ben" || m.Key != "k1" {
		t.Fatalf("%s", raw)
	}
	// Ben's connection shares the workspace: he sees Anna's post, and may
	// neither edit nor delete it.
	mb := caller(b, messaging.ContractRef)
	raw, _ = mb("messages.list", map[string]any{"channel": "general"})
	if !strings.Contains(string(raw), m.ID) {
		t.Fatalf("ben does not see anna's post: %s", raw)
	}
	for _, op := range []string{"message.edit", "message.delete"} {
		if _, err := mb(op, map[string]any{"channel": "general", "id": m.ID, "text": "x"}); !errors.As(err, &ce) || ce.Code != connectors.CodeInvalid {
			t.Errorf("%s of someone else's message: %v", op, err)
		}
	}
	// A private channel is seen only by its members.
	if _, err := ctl("addChannel", map[string]any{"name": "secret", "kind": "private", "members": []string{"u-anna"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mb("messages.list", map[string]any{"channel": "c-secret"}); !errors.As(err, &ce) || ce.Code != connectors.CodeNotFound {
		t.Errorf("ben listed a private channel he is not in: %v", err)
	}
	if _, err := ma("messages.list", map[string]any{"channel": "c-secret"}); err != nil {
		t.Errorf("anna cannot list her private channel: %v", err)
	}
}

// Two connections in one workspace each get every change as a notice (a
// connection's own posts too, with their key); a dropped notice is still
// in the next list.
func TestWatchBothWays(t *testing.T) {
	s := msgtest.New()
	a := open(t, s, "a", `{"workspace":"acme","user":"u-anna"}`)
	b := open(t, s, "b", `{"workspace":"acme","user":"u-ben"}`)
	wctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	seen := map[string][]string{}
	var wg sync.WaitGroup
	for name, sess := range map[string]adapter.Session{"a": a, "b": b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := ctxFor(messaging.ContractRef)
			c.Context = wctx
			_ = sess.(adapter.Watcher).Watch(c, nil, func(e adapter.Event) error {
				var p struct {
					ID      string          `json:"id"`
					Message json.RawMessage `json:"message"`
				}
				_ = json.Unmarshal(e.Payload, &p)
				if !strings.HasPrefix(p.ID, name+"-") || e.ID != p.ID {
					t.Errorf("notice id %q (event %q) on connection %s", p.ID, e.ID, name)
				}
				mu.Lock()
				seen[name] = append(seen[name], e.Name)
				mu.Unlock()
				return nil
			})
		}()
	}
	ma, ctl := caller(a, messaging.ContractRef), caller(a, msgtest.ControlRef)
	raw, err := ma("message.post", map[string]any{"channel": "general", "key": "nx-1", "text": "from the bridge"})
	if err != nil {
		t.Fatal(err)
	}
	var mine messaging.Message
	_ = json.Unmarshal(raw, &mine)
	if _, err := ctl("dropNotifications", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl("post", map[string]any{"user": "u-dana", "text": "lost notice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl("react", map[string]any{"id": mine.ID, "emoji": "tada"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl("edit", map[string]any{"id": mine.ID, "text": "edited outside"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl("delete", map[string]any{"id": mine.ID}); err != nil {
		t.Fatal(err)
	}
	want := []string{"messagePosted", "reactionChanged", "messageEdited", "messageDeleted"}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		done := len(seen["a"]) >= len(want) && len(seen["b"]) >= len(want)
		mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	for _, name := range []string{"a", "b"} {
		if strings.Join(seen[name], ",") != strings.Join(want, ",") {
			t.Errorf("connection %s saw %v, want %v", name, seen[name], want)
		}
	}
	// The lost notice's message is still listed.
	raw, _ = caller(b, messaging.ContractRef)("messages.list", map[string]any{"channel": "general", "since": "0"})
	if !strings.Contains(string(raw), "lost notice") || !strings.Contains(string(raw), mine.ID) {
		t.Fatalf("%s", raw)
	}
}
