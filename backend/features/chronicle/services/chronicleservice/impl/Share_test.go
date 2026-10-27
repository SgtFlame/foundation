package impl

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

func TestSharing(t *testing.T) {
	s, store, _ := newTestService(t)
	appDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	chronicle.ContributeAccess(reg)
	pol := authz.NewPolicy(appDB, reg)
	authz.Install(pol)
	defer authz.Install(nil)

	ctx := context.Background()
	as := func(user, tenant uint64) features.ServiceArgs {
		return features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: tenant}}
	}
	owner, colleague, moderator, global, outsider := as(1, 1), as(2, 1), as(3, 1), as(4, 9), as(5, 9)
	for _, g := range []authz.GrantArgs{
		{UserID: 3, RoleID: authz.RoleModerator, Resource: "tenant:1"},
		{UserID: 4, RoleID: authz.RoleGlobalMod, Resource: "app"},
		{UserID: 3, RoleID: authz.RoleGlobalMod, Resource: "app"}, // also global, but may not approve twice
	} {
		if _, err := pol.Grant(ctx, nil, g); err != nil {
			t.Fatal(err)
		}
	}
	entry := create(t, s, chronicleservice.EntryInput{Type: "note", Title: "Release process", ShortSummary: "How we ship."})
	visibleTo := func(args features.ServiceArgs, id uint64) bool {
		return s.Get(chronicleservice.GetArgs{ServiceArgs: args, ID: id}).Common.Code == http.StatusOK
	}
	if visibleTo(colleague, entry.ID) {
		t.Fatal("a colleague sees an unshared entry")
	}

	// With the tenant: at once, read-only, and found by search
	resp := s.Share(chronicleservice.ShareArgs{ServiceArgs: owner, ID: entry.ID, Target: chronicle.SharedTenant})
	s.indexing.Wait()
	if resp.Common.Code != http.StatusOK || !visibleTo(colleague, entry.ID) || !store.rows[entry.ID].Shared {
		t.Fatalf("share with the tenant: %+v, row %+v", resp.Common, store.rows[entry.ID])
	}
	update := s.Update(chronicleservice.UpdateArgs{ServiceArgs: colleague, ID: entry.ID, EntryInput: chronicleservice.EntryInput{Type: "note", Title: "Mine now"}})
	if update.Common.Code != http.StatusNotFound {
		t.Errorf("a colleague changed a shared entry: %+v", update.Common)
	}
	if list := s.List(chronicleservice.ListArgs{ServiceArgs: colleague}); len(list.Entries) != 1 {
		t.Errorf("the colleague's list: %+v", list.Entries)
	}

	// Globally: two approvals, by others, in order
	resp = s.Share(chronicleservice.ShareArgs{ServiceArgs: owner, ID: entry.ID, Target: chronicle.SharedGlobal, Note: "useful everywhere"})
	if resp.Common.Code != http.StatusOK || resp.Share == nil || resp.Share.Status != chronicle.SharePending {
		t.Fatalf("request: %+v", resp)
	}
	share := resp.Share.ID
	if again := s.Share(chronicleservice.ShareArgs{ServiceArgs: owner, ID: entry.ID, Target: chronicle.SharedGlobal}); again.Common.Code != http.StatusConflict {
		t.Errorf("a second request: %+v", again.Common)
	}
	if q := s.Shares(chronicleservice.SharesArgs{ServiceArgs: colleague, Queue: true}); q.Common.Code != authz.Status {
		t.Errorf("a non-moderator's queue: %+v", q.Common)
	}
	if q := s.Shares(chronicleservice.SharesArgs{ServiceArgs: global, Queue: true}); len(q.Shares) != 0 {
		t.Errorf("the global moderator sees a request before the tenant approved it: %+v", q.Shares)
	}
	if m := s.Moderate(chronicleservice.ModerateArgs{ServiceArgs: global, ShareID: share, Approve: true}); m.Common.Code == http.StatusOK {
		t.Error("the global moderator approved first")
	}
	if q := s.Shares(chronicleservice.SharesArgs{ServiceArgs: moderator, Queue: true}); len(q.Shares) != 1 {
		t.Fatalf("the tenant moderator's queue: %+v", q)
	}
	if m := s.Moderate(chronicleservice.ModerateArgs{ServiceArgs: moderator, ShareID: share, Approve: true}); m.Share.Status != chronicle.ShareTenantApproved {
		t.Fatalf("tenant approval: %+v", m)
	}
	if visibleTo(outsider, entry.ID) {
		t.Fatal("another tenant sees the entry before the global approval")
	}
	if twice := s.Moderate(chronicleservice.ModerateArgs{ServiceArgs: moderator, ShareID: share, Approve: true}); twice.Common.Code != authz.Status {
		t.Errorf("one moderator gave both approvals: %+v", twice.Common)
	}
	m := s.Moderate(chronicleservice.ModerateArgs{ServiceArgs: global, ShareID: share, Approve: true})
	s.indexing.Wait()
	if m.Share.Status != chronicle.ShareApproved || m.Share.CopyID == 0 || store.rows[m.Share.CopyID].TenantID != features.GlobalTenantID {
		t.Fatalf("global approval: %+v", m)
	}
	if !visibleTo(outsider, m.Share.CopyID) || visibleTo(outsider, entry.ID) || visibleTo(owner, m.Share.CopyID) {
		t.Error("another tenant should see the global copy, not the original, and the owner not their own copy")
	}

	// Stopping sharing removes the copy
	s.Share(chronicleservice.ShareArgs{ServiceArgs: owner, ID: entry.ID, Target: ""})
	s.indexing.Wait()
	if visibleTo(outsider, m.Share.CopyID) || visibleTo(colleague, entry.ID) {
		t.Error("still shared after stopping")
	}
	if _, ok := store.rows[m.Share.CopyID]; ok {
		t.Error("the global copy is still indexed")
	}
}
