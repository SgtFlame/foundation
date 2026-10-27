package secrets

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/secrets/vault/vaulttest"
)

const secretIDPath = "auth/approle/role/foundation/secret-id"

func status(err error) int {
	var refused *UnsealError
	if errors.As(err, &refused) {
		return refused.Status
	}
	return 0
}

func TestWithoutVaultStartsUnsealed(t *testing.T) {
	b, err := NewBootstrap(VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Unseal(context.Background(), "x", "y"); status(err) != http.StatusConflict {
		t.Errorf("unseal without Vault: %v", err)
	}
	if b.State() != Starting {
		t.Errorf("state %s before Started", b.State())
	}
	b.Started()
	if b.State() != Ready {
		t.Errorf("state %s after Started", b.State())
	}
}

func TestUnseal(t *testing.T) {
	fake := vaulttest.New(t)
	fake.Set("server/db-key", "the-key")
	fake.Set("server/gemini-api-key", "AIza-server")
	b, err := NewBootstrap(VaultOptions{Addr: fake.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if b.State() != Sealed {
		t.Fatalf("state %s", b.State())
	}

	// Refused before anything is spent: no role, a dead token, a token
	// wrapping something else
	if err := b.Unseal(ctx, "w1", ""); status(err) != http.StatusBadRequest {
		t.Errorf("no role ID: %v", err)
	}
	if err := b.Unseal(ctx, "nope", "role-id"); status(err) != http.StatusBadRequest {
		t.Errorf("dead token: %v", err)
	}
	fake.Wrap("other", "auth/token/create")
	if err := b.Unseal(ctx, "other", "role-id"); status(err) != http.StatusBadRequest {
		t.Errorf("token wrapping a token: %v", err)
	}

	fake.Wrap("good", secretIDPath)
	if err := b.Unseal(ctx, "good", "role-id"); err != nil {
		t.Fatalf("unseal: %v", err)
	}
	waited, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	bundle, err := b.Wait(waited)
	if err != nil || bundle["db-key"] != "the-key" || bundle["gemini-api-key"] != "AIza-server" {
		t.Errorf("bundle %v, %v", bundle, err)
	}
	if b.State() != Starting {
		t.Errorf("state %s", b.State())
	}
	b.Started()
	if err := b.Unseal(ctx, "good", "role-id"); status(err) != http.StatusConflict {
		t.Errorf("unseal when ready: %v", err)
	}

	// Losing Vault degrades the server; a new token restores it
	b.Lost(ctx, errors.New("vault: unreachable"))
	if s := b.Status(); s.State != Degraded || s.Problem == "" {
		t.Errorf("after Lost: %+v", s)
	}
	fake.Wrap("again", secretIDPath)
	if err := b.Unseal(ctx, "again", "role-id"); err != nil || b.State() != Ready {
		t.Errorf("unseal when degraded: %v, %s", err, b.State())
	}
}

func TestPinnedRoleID(t *testing.T) {
	fake := vaulttest.New(t)
	fake.Set("server/db-key", "k")
	b, _ := NewBootstrap(VaultOptions{Addr: fake.URL, RoleID: "role-id"})
	fake.Wrap("good", secretIDPath)
	if err := b.Unseal(context.Background(), "good", "someone-else"); status(err) != http.StatusBadRequest {
		t.Errorf("another role: %v", err)
	}
	if err := b.Unseal(context.Background(), "good", ""); err != nil {
		t.Errorf("pinned role, none given: %v", err)
	}
}
