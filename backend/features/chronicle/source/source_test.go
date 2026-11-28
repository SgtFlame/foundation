package chroniclesource_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/retrievaltest"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

// An agent's run searches the memory of MemoryOf alone (forge 15a): that
// person's entries and the tenant's shared ones, never the global
// tenant's; without MemoryOf, none. A person still searches their own and
// the global ones.
func TestAgentMemory(t *testing.T) {
	reg := extensions.NewRegistry()
	reg.Declare(retrieval.Sources)
	stores := retrievaltest.Contribute(reg, t.TempDir())
	ctx := context.Background()
	for _, r := range []chronicle.Row{
		{EntryID: 1, TenantID: 1, OwnerID: 2, Title: "Ana's refund note", Text: "refund customers within ten days"},
		{EntryID: 2, TenantID: 1, OwnerID: 3, Title: "Bo's refund note", Text: "refund policy Bo keeps private"},
		{EntryID: 3, TenantID: 1, OwnerID: 4, Shared: true, Title: "Team refund note", Text: "refund requests go to billing"},
		{EntryID: 4, TenantID: features.GlobalTenantID, OwnerID: 9, Shared: true, Title: "Platform refund note", Text: "refund notes shared with everyone"},
	} {
		stores.Memory.Put(ctx, r)
	}
	service := retrievaltest.Service(reg, nil)
	found := func(p features.Principal) []string {
		t.Helper()
		resp := service.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: p}, Query: "refund", Sources: []string{"chronicle"}, LexicalSemantic: -5, TopK: 10})
		if resp.Common.Code >= 300 {
			t.Fatal(resp.Common.Message)
		}
		var out []string
		for _, s := range resp.Segments {
			out = append(out, s.Key)
		}
		slices.Sort(out)
		return out
	}
	forAna := features.AgentPrincipal(1, 50, 2)
	forAna.MemoryOf = 2
	if got := found(forAna); !slices.Equal(got, []string{"1", "3"}) {
		t.Errorf("an agent with Ana's memory found %v, want Ana's own and the tenant's shared", got)
	}
	if got := found(features.AgentPrincipal(1, 50, 2)); len(got) != 0 {
		t.Errorf("an agent with no memory owner found %v", got)
	}
	if got := found(features.AppPrincipal(1, "mail")); len(got) != 0 {
		t.Errorf("an app found %v", got)
	}
	if got := found(features.Principal{Kind: features.PrincipalUser, UserID: 2, TenantID: 1}); !slices.Equal(got, []string{"1", "3", "4"}) {
		t.Errorf("Ana found %v, want her own, the tenant's and the global shared ones", got)
	}
}

// A project's entries (forge 35f-6) are found by its readers, by their
// facts too ("Home: Greyhaven"), and by nobody else.
func TestProjectMemory(t *testing.T) {
	reg := extensions.NewRegistry()
	reg.Declare(retrieval.Sources)
	stores := retrievaltest.Contribute(reg, t.TempDir())
	ctx := context.Background()
	stores.Memory.Put(ctx, chronicle.Row{EntryID: 1, TenantID: 1, OwnerID: 2, Scope: "project:bible", Title: "Wenlan", Text: "# Wenlan\n\nHome: Greyhaven\nStatus: Alive"})
	stores.Memory.Put(ctx, chronicle.Row{EntryID: 2, TenantID: 1, OwnerID: 2, Scope: "project:other", Title: "Tarn", Text: "# Tarn\n\nHome: Greyhaven"})
	chronicle.SetProjectAccess(func(_ context.Context, p features.Principal) []string {
		if p.UserID == 3 {
			return []string{"project:bible"}
		}
		return nil
	})
	defer chronicle.SetProjectAccess(nil)
	service := retrievaltest.Service(reg, nil)
	found := func(user uint64) []string {
		t.Helper()
		resp := service.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}},
			Query: "Greyhaven", Sources: []string{"chronicle"}, LexicalSemantic: -5, TopK: 10})
		var out []string
		for _, s := range resp.Segments {
			out = append(out, s.Key)
		}
		slices.Sort(out)
		return out
	}
	if got := found(3); !slices.Equal(got, []string{"1"}) {
		t.Errorf("a reader of the project found %v", got)
	}
	if got := found(4); len(got) != 0 {
		t.Errorf("someone else found %v", got)
	}
}
