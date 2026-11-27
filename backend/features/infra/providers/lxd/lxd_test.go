package lxd_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/contracttest"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/cassette"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/lxd"
)

// open opens a session on the cassette, or, recording (INFRA_RECORD=1),
// on the server LXD_URL with the certificate files LXD_CERT and LXD_KEY.
func open(t *testing.T, file string) adapter.Session {
	t.Helper()
	st := map[string]any{"baseUrl": "https://lxd.test:8443", "clientCert": "unused", "clientKey": "unused"}
	o := lxd.Options{}
	if cassette.Recording() {
		cert, _ := os.ReadFile(os.Getenv("LXD_CERT"))
		key, _ := os.ReadFile(os.Getenv("LXD_KEY"))
		if os.Getenv("LXD_URL") == "" || len(cert) == 0 || len(key) == 0 {
			t.Skip("recording needs LXD_URL, LXD_CERT and LXD_KEY")
		}
		st = map[string]any{"baseUrl": os.Getenv("LXD_URL"), "clientCert": string(cert), "clientKey": string(key)}
		o.Wrap = func(next http.RoundTripper) http.RoundTripper {
			r := cassette.Record(t, file, "lxd", "recorded "+time.Now().UTC().Format("2006-01-02"), next)
			r.From, r.To = strings.TrimRight(os.Getenv("LXD_URL"), "/"), "https://lxd.test:8443"
			return r
		}
	} else {
		o.HTTP = cassette.Replay(t, file).Client()
	}
	raw, _ := json.Marshal(st)
	s, err := lxd.Impl(o).Open(adapter.Context{Context: context.Background()}, adapter.Connection{ID: "lxd1", Type: lxd.TypeID, Tenant: features.RootTenantID, Settings: raw})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func caller(s adapter.Session) contracttest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		return s.Call(adapter.Context{Context: context.Background(), Contract: infra.ComputeRef, Operation: op}, op, raw)
	}
}

func TestContract(t *testing.T) {
	s := open(t, "testdata/contract.json")
	contracttest.Run(t, caller(s), contracttest.Options{Size: "m", Region: "local", Ready: time.Second})
}

func TestType(t *testing.T) {
	ps := connectors.CheckType(lxd.Type(), func(ref string) (connectors.Contract, bool) {
		return infra.ComputeContract(), ref == infra.ComputeRef
	})
	if len(ps) > 0 {
		t.Fatalf("problems: %+v", ps)
	}
}

func TestName(t *testing.T) {
	for in, want := range map[string]string{"west-general-1": "west-general-1", "Host_A.1": "host-a-1", "1st": "h-1st"} {
		if got := lxd.Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSettings(t *testing.T) {
	if _, err := lxd.New(lxd.Settings{BaseURL: "http://lxd.test"}, lxd.Options{HTTP: http.DefaultClient}); connectors.AsError(err).Code != connectors.CodeInvalid {
		t.Fatalf("an http address = %v, want invalid", err)
	}
	if _, err := lxd.New(lxd.Settings{BaseURL: "https://lxd.test", ClientCert: "x", ClientKey: "y"}, lxd.Options{}); connectors.AsError(err).Code != connectors.CodeInvalid {
		t.Fatalf("a bad key pair = %v, want invalid", err)
	}
}
