package digitalocean_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/infra/contracttest"
	"github.com/SgtFlame/foundation/backend/features/infra/providers"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/cassette"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/digitalocean"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/providertest"
)

const base = "https://api.digitalocean.test"

// options replay file, or record it (INFRA_RECORD=1) with
// DIGITALOCEAN_TOKEN against the real API.
func options(t *testing.T, file string) (digitalocean.Options, string) {
	t.Helper()
	if cassette.Recording() {
		token := os.Getenv("DIGITALOCEAN_TOKEN")
		if token == "" {
			t.Skip("recording needs DIGITALOCEAN_TOKEN")
		}
		r := cassette.Record(t, file, "digitalocean", "recorded "+time.Now().UTC().Format("2006-01-02"), nil)
		r.From, r.To = digitalocean.API, base
		return digitalocean.Options{HTTP: r.Client()}, token
	}
	return digitalocean.Options{Base: base, HTTP: cassette.Replay(t, file).Client()}, "recorded"
}

func TestContract(t *testing.T) {
	o, token := options(t, "testdata/contract.json")
	s := providertest.Open(t, digitalocean.Impl(o), map[string]any{"token": token})
	contracttest.Run(t, providertest.Caller(s), contracttest.Options{Size: "s-2vcpu-4gb", Region: "ams3", Ready: 2 * time.Second})
}

func TestPagesAndErrors(t *testing.T) {
	o, _ := options(t, "testdata/errors.json")
	if cassette.Recording() {
		t.Skip("written by hand")
	}
	b, err := digitalocean.New(digitalocean.Settings{Token: "recorded"}, o)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	regions, err := b.Regions(ctx)
	if err != nil || len(regions) != 2 || regions[1].ID != "ams3" {
		t.Fatalf("regions over two pages = %+v, %v", regions, err)
	}
	_, err = b.Create(ctx, providers.CreateArgs{ID: "x", Name: "x", Size: "g-2vcpu-8gb", Region: "ams3"})
	if ce := connectors.AsError(err); ce.Code != connectors.CodeInvalid || ce.Message == "" {
		t.Fatalf("a 422 = %v, want invalid with DigitalOcean's message", err)
	}
	if err := b.Check(ctx); connectors.AsError(err).Code != connectors.CodeAuth {
		t.Fatalf("a 401 = %v, want auth", err)
	}
	if err := b.Check(ctx); connectors.AsError(err).Code != connectors.CodeRateLimited {
		t.Fatalf("a 429 = %v, want rateLimited", err)
	}
}

func TestType(t *testing.T) { providertest.CheckType(t, digitalocean.Type()) }

func TestSlug(t *testing.T) {
	if got := digitalocean.Slug("ubuntu-24.04"); got != "ubuntu-24-04-x64" {
		t.Fatalf("Slug = %q", got)
	}
}
