package infra_test

import (
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

// seedDraft puts a small configuration into the draft: a test provider
// account, a cloud site with its cluster and a host pool of three.
func seedDraft(st *stack) {
	st.must("infra.config.put", m{"kind": "accounts", "item": m{"id": "test", "name": "Test provider", "provider": "infratest", "connection": "c1"}})
	st.must("infra.config.put", m{"kind": "sites", "item": m{"id": "west", "name": "West", "account": "test", "region": "r1"}})
	st.must("infra.config.put", m{"kind": "clusters", "item": m{"id": "west", "site": "west"}})
	st.must("infra.config.put", m{"kind": "hostPools", "item": m{"id": "general", "site": "west", "size": "m", "count": 3}})
}

func TestDraftAndCommit(t *testing.T) {
	st := newStack(t)
	seedDraft(st)
	c := st.config()
	if len(c.Draft.Sites) != 1 || c.Draft.HostPools[0].Count != 3 || c.Rev != 4 || c.Head != 0 {
		t.Fatalf("draft = %+v", c)
	}
	// A reference to a site that does not exist is refused with its path.
	r := st.send("infra.config.put", m{"kind": "hostPools", "item": m{"id": "east", "site": "east", "size": "m", "count": 1}})
	if r.Common.Code < 300 || len(r.Problems) == 0 || r.Problems[0].Path != "document.hostPools[east].site" {
		t.Fatalf("bad reference = %d %+v", r.Common.Code, r.Problems)
	}
	// Removing a site something refers to is refused.
	if r := st.send("infra.config.remove", m{"kind": "sites", "id": "west"}); r.Common.Code < 300 {
		t.Fatal("removed a site a cluster refers to")
	}
	// Commit makes version 1; a commit with a stale rev is refused.
	st.must("infra.config.commit", m{"message": "First", "expect": c.Rev})
	var v infra.Version
	if !st.get(infra.VersionProjector, infra.VersionType+"."+infra.VersionID("main", 1), &v) || v.Number != 1 || v.Author != "7" || len(v.Document.HostPools) != 1 {
		t.Fatalf("version 1 = %+v", v)
	}
	if c := st.config(); c.Head != 1 || c.Dirty() {
		t.Fatalf("after commit = %+v", c)
	}
	if r := st.send("infra.config.commit", m{"message": "Again", "expect": c.Rev}); r.Common.ErrorCode != "nothing_to_commit" {
		t.Fatalf("empty commit = %+v", r.Common)
	}
	st.must("infra.config.put", m{"kind": "hostPools", "item": m{"id": "general", "site": "west", "size": "m", "count": 5}})
	if r := st.send("infra.config.commit", m{"message": "Stale", "expect": c.Rev}); r.Common.ErrorCode != "head_moved" {
		t.Fatalf("stale commit = %+v", r.Common)
	}
	c = st.config()
	st.must("infra.config.commit", m{"message": "Five hosts", "expect": c.Rev})
	var v2 infra.Version
	st.get(infra.VersionProjector, infra.VersionType+"."+infra.VersionID("main", 2), &v2)
	if v2.Parent != 1 || v2.Document.HostPools[0].Count != 5 {
		t.Fatalf("version 2 = %+v", v2)
	}
	// Version 1 never changed.
	st.get(infra.VersionProjector, infra.VersionType+"."+infra.VersionID("main", 1), &v)
	if v.Document.HostPools[0].Count != 3 {
		t.Fatal("version 1 changed")
	}
}

func TestOutsideTheRootTenant(t *testing.T) {
	st := newStack(t)
	other := operator(7)
	other.Principal.TenantID = 1
	r := st.sendAs(other, "infra.config.put", infra.DefaultConfig, m{"kind": "accounts", "item": m{"id": "a", "name": "A", "provider": "lxd"}})
	if r.Common.ErrorCode != "not_root_tenant" {
		t.Fatalf("tenant 1 = %+v", r.Common)
	}
	_ = features.RootTenantID
}

func TestDocumentIsNormalized(t *testing.T) {
	a := infra.Document{Sites: []infra.Site{{ID: "b", OnPremise: true}, {ID: "a", OnPremise: true}}}.Normalized()
	b := infra.Document{Sites: []infra.Site{{ID: "a", OnPremise: true}, {ID: "b", OnPremise: true}}}.Normalized()
	if a.Sites[0].ID != "a" || a.Sites[0] != b.Sites[0] || a.Sites[1] != b.Sites[1] {
		t.Fatalf("a = %+v, b = %+v", a, b)
	}
}

func TestImportRefusesADirtyDraft(t *testing.T) {
	st := newStack(t)
	seedDraft(st)
	doc := m{"sites": []m{{"id": "lab", "name": "Lab", "onPremise": true}}}
	if r := st.send("infra.config.import", m{"document": doc}); r.Common.ErrorCode != "draft_dirty" {
		t.Fatalf("import over a dirty draft = %+v", r.Common)
	}
	st.must("infra.config.import", m{"document": doc, "force": true})
	if c := st.config(); len(c.Draft.Sites) != 1 || c.Draft.Sites[0].ID != "lab" || len(c.Draft.Accounts) != 0 {
		t.Fatalf("after import = %+v", c.Draft)
	}
}
