package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
)

func TestPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse") {
		t.Error("the right password does not verify")
	}
	if VerifyPassword(hash, "correct horse ") || VerifyPassword("not a hash", "correct horse") {
		t.Error("a wrong password or hash verifies")
	}
}

func TestTokens(t *testing.T) {
	s := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	now := time.Now()
	s.now = func() time.Time { return now }
	token, _, err := s.Issue(User{Model: db.Model{ID: 7}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if want := (features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: 3}); p != want {
		t.Errorf("principal = %+v, want %+v", p, want)
	}
	if _, err := NewSigner([]byte("another key, another key, abcdef")).Verify(token); err == nil || errors.Is(err, ErrExpired) {
		t.Errorf("a token signed with another key: %v", err)
	}
	s.now = func() time.Time { return now.Add(AccessLifetime + time.Minute) }
	if _, err := s.Verify(token); !errors.Is(err, ErrExpired) {
		t.Errorf("an expired token: %v, want ErrExpired", err)
	}
}

func testSessions(t *testing.T) (Sessions, *time.Time, User) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, Models()...); err != nil {
		t.Fatal(err)
	}
	u := User{Model: db.Model{ID: 1}, Username: "ada", PasswordHash: "x"}
	if err := gdb.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []uint64{1, 2} {
		if err := gdb.Create(&Membership{TenantID: tenant, UserID: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	return Sessions{DB: gdb, Signer: NewSigner([]byte("0123456789abcdef0123456789abcdef")), Now: func() time.Time { return now }}, &now, u
}

func TestRotate(t *testing.T) {
	ctx := context.Background()
	s, now, u := testSessions(t)
	first, err := s.Start(ctx, u, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Rotate(ctx, first.Refresh)
	if err != nil || second.Refresh == "" || second.TenantID != 2 {
		t.Fatalf("rotate: %+v, %v", second, err)
	}

	// Another tab refreshing at once with the old token gets an access token only
	again, err := s.Rotate(ctx, first.Refresh)
	if err != nil || again.Access == "" || again.Refresh != "" {
		t.Fatalf("within the grace: %+v, %v", again, err)
	}

	// After the grace, the old token is taken as stolen and the family dies
	*now = now.Add(RefreshGrace + time.Second)
	if _, err := s.Rotate(ctx, first.Refresh); !errors.Is(err, ErrReused) {
		t.Fatalf("reuse: %v, want ErrReused", err)
	}
	if _, err := s.Rotate(ctx, second.Refresh); !errors.Is(err, ErrBadRefresh) {
		t.Errorf("the family's live token after reuse: %v, want ErrBadRefresh", err)
	}
}

func TestEndAndMembership(t *testing.T) {
	ctx := context.Background()
	s, now, u := testSessions(t)
	a, _ := s.Start(ctx, u, 1)
	b, _ := s.Rotate(ctx, a.Refresh)
	if err := s.End(ctx, b.Refresh); err != nil {
		t.Fatal(err)
	}
	// Signed out: not even the grace gives a token
	if _, err := s.Rotate(ctx, a.Refresh); err == nil {
		t.Error("a token of an ended session refreshes")
	}
	if _, err := s.Rotate(ctx, b.Refresh); err == nil {
		t.Error("an ended session refreshes")
	}

	// Leaving the tenant ends refreshing there
	c, _ := s.Start(ctx, u, 2)
	if err := s.DB.Where("tenant_id = 2").Delete(&Membership{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rotate(ctx, c.Refresh); !errors.Is(err, ErrBadRefresh) {
		t.Errorf("after leaving the tenant: %v", err)
	}

	// Expired
	d, _ := s.Start(ctx, u, 1)
	*now = now.Add(RefreshLifetime + time.Hour)
	if _, err := s.Rotate(ctx, d.Refresh); !errors.Is(err, ErrBadRefresh) {
		t.Errorf("an expired token: %v", err)
	}
}

func TestTenantFor(t *testing.T) {
	s, _, u := testSessions(t)
	cases := []struct {
		want, last, got uint64
	}{{2, 0, 2}, {0, 2, 2}, {9, 0, 1}, {0, 0, 1}}
	for _, c := range cases {
		u.LastTenantID = c.last
		got, ok, err := TenantFor(s.DB, u, c.want)
		if err != nil || !ok || got != c.got {
			t.Errorf("TenantFor(want %d, last %d) = %d, %v, %v; want %d", c.want, c.last, got, ok, err, c.got)
		}
	}
	_, ok, _ := TenantFor(s.DB.Session(&gorm.Session{}), User{Model: db.Model{ID: 99}}, 0)
	if ok {
		t.Error("a user in no tenant has one")
	}
}
