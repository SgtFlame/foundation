package azure_test

import (
	"os"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/infra/contracttest"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/azure"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/cassette"
	"github.com/SgtFlame/foundation/backend/features/infra/providers/providertest"
)

const (
	login      = "https://login.azure.test"
	management = "https://management.azure.test"
)

// settings and options replay file, or record it (INFRA_RECORD=1) with
// the AZURE_* variables against the real API.
func setup(t *testing.T, file string) (map[string]any, azure.Options) {
	t.Helper()
	st := map[string]any{"tenantId": "11111111-2222-3333-4444-555555555555", "clientId": "99999999-8888-7777-6666-555555555555", "clientSecret": "recorded",
		"subscriptionId": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "location": "westeurope", "sshPublicKey": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample axiom"}
	if cassette.Recording() {
		for _, k := range []string{"AZURE_TENANT_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET", "AZURE_SUBSCRIPTION_ID"} {
			if os.Getenv(k) == "" {
				t.Skip("recording needs AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_CLIENT_SECRET and AZURE_SUBSCRIPTION_ID")
			}
		}
		st["tenantId"], st["clientId"], st["clientSecret"], st["subscriptionId"] = os.Getenv("AZURE_TENANT_ID"), os.Getenv("AZURE_CLIENT_ID"), os.Getenv("AZURE_CLIENT_SECRET"), os.Getenv("AZURE_SUBSCRIPTION_ID")
		r := cassette.Record(t, file, "azure", "recorded "+time.Now().UTC().Format("2006-01-02")+"; scrub the subscription, tenant and client ids before committing", nil)
		r.From, r.To = azure.Management, management
		return st, azure.Options{HTTP: r.Client()}
	}
	return st, azure.Options{Login: login, Management: management, HTTP: cassette.Replay(t, file).Client(), Poll: -1}
}

func TestContract(t *testing.T) {
	st, o := setup(t, "testdata/contract.json")
	s := providertest.Open(t, azure.Impl(o), st)
	contracttest.Run(t, providertest.Caller(s), contracttest.Options{Size: "Standard_D2s_v3", Region: "westeurope", Ready: 2 * time.Second})
}

func TestWrongSecret(t *testing.T) {
	if cassette.Recording() {
		t.Skip("written by hand")
	}
	st, o := setup(t, "testdata/errors.json")
	s := providertest.Open(t, azure.Impl(o), st)
	_, err := providertest.Caller(s)("check", map[string]any{})
	if ce := connectors.AsError(err); ce.Code != connectors.CodeAuth {
		t.Fatalf("a wrong secret = %v, want auth", err)
	}
}

func TestSizes(t *testing.T) {
	for size, want := range map[string]bool{"Standard_D2s_v3": true, "Standard_E4s_v5": true, "Standard_D16ads_v5": true, "Standard_B2s": false, "Standard_D2_v2": false, "Standard_F4s_v2": false} {
		if got := azure.NestedCapable(size); got != want {
			t.Errorf("NestedCapable(%s) = %v", size, got)
		}
	}
}

func TestType(t *testing.T) { providertest.CheckType(t, azure.Type()) }
