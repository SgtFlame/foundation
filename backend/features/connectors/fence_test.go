package connectors_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice"
	cximpl "github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice/impl"
	cxproxy "github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice/proxy"
	cxserver "github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice/server"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/nats-io/nats.go"
)

// violations collects the permission errors NATS reports on a connection.
type violations struct {
	mu   sync.Mutex
	errs []string
}

func (v *violations) handler(_ *nats.Conn, _ *nats.Subscription, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.errs = append(v.errs, err.Error())
}

func (v *violations) wait(t *testing.T, subject string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v.mu.Lock()
		for _, e := range v.errs {
			if strings.Contains(e, "Permissions Violation") && strings.Contains(e, subject) {
				v.mu.Unlock()
				return
			}
		}
		v.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no permissions violation for %s: %v", subject, v.errs)
}

// An external adapter under the user its types' declaration gives serves
// its calls through svc.connectors, and NATS refuses it a tenant's
// stream, JetStream and every service its types do not declare (forge
// 21e-2).
func TestAdapterFencedByNATS(t *testing.T) {
	o := connectorstest.Options("fenced", nil)
	perms := connectors.AdapterPermissions(o.Types...)
	srv := natsd.StartTestUsers(t, natsd.User{Name: "adapter", Password: "pw", Publish: perms.Publish, Subscribe: perms.Subscribe})
	f := setup(t, srv)
	cx, err := cxserver.Serve(srv.Conn(), cximpl.New(cximpl.WithJetStream(srv.JS())), services.TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cx.Stop)

	var seen violations
	nc, err := srv.ConnectAs("fenced", "adapter", "pw", nats.ErrorHandler(seen.handler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	o.Books = connectorsservice.Remote(cxproxy.New(&services.Client{NC: nc, Caller: "fenced"}))
	o.AnnounceEvery, o.TTL = 100*time.Millisecond, 400*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adapter.Serve(ctx, nc, o) }()
	t.Cleanup(func() { cancel(); <-done })

	f.online("test.echo", true, 3*time.Second)
	if out, e := f.echo("hi"); e != nil || out["by"] != "fenced" {
		t.Fatalf("echo through the fence: %v %v", out, e)
	}

	// A tenant's event stream and JetStream's API are out of its reach
	_ = nc.Publish("evt.1.rec.task.1.created", []byte(`{}`))
	seen.wait(t, "evt.1.rec.task.1.created")
	if _, err := nc.Request("$JS.API.INFO", nil, time.Second); err == nil {
		t.Error("the adapter reached JetStream's API")
	}
	seen.wait(t, "$JS.API.INFO")

	// So are services its types do not declare: identity, and files
	// (connectorstest's types declare none)
	idc := &services.Client{NC: nc, Caller: "fenced"}
	cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
	defer ccancel()
	r := services.Call[struct{ Common features.ServiceResponse }](idc, "identity", "Status", struct{ features.ServiceArgs }{features.ServiceArgs{Ctx: cctx, Principal: features.ServicePrincipal(1)}})
	if r.Common.Code == 200 {
		t.Errorf("the adapter called svc.identity: %+v", r)
	}
	seen.wait(t, "svc.identity.Status")
	_ = nc.Publish("svc.files.Get", []byte(`{}`))
	seen.wait(t, "svc.files.Get")
}

// A type declaring files may call svc.files; one naming an unknown
// service is refused by the check.
func TestTypeServices(t *testing.T) {
	ty := connectorstest.Types()[0]
	ty.Services = []string{"files"}
	p := connectors.AdapterPermissions(ty)
	if !contains(p.Publish, "svc.files.>") || contains(p.Publish, "svc.record.>") || contains(p.Publish, "evt.>") {
		t.Errorf("permissions %+v", p)
	}
	ty.Services = []string{"identity"}
	problems := connectors.CheckType(ty, nil)
	found := false
	for _, pr := range problems {
		found = found || pr.Path == "services[0]"
	}
	if !found {
		t.Errorf("an unknown service passed the check: %+v", problems)
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
