package infra_test

import (
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra"
)

func TestDiffSummaries(t *testing.T) {
	a := infra.Document{
		Sites:     []infra.Site{{ID: "west", Name: "West", OnPremise: true}},
		HostPools: []infra.HostPool{{ID: "general", Site: "west", Size: "m", Count: 3}},
	}
	b := a.Clone()
	b.HostPools[0].Count = 5
	b.Sites = append(b.Sites, infra.Site{ID: "east", Name: "East", OnPremise: true})
	got := infra.Diff(a, b)
	want := map[string]bool{"host pool general on west: count 3 → 5": false, "adds site east": false}
	for _, c := range got {
		if _, ok := want[c.Summary]; ok {
			want[c.Summary] = true
		}
	}
	for s, seen := range want {
		if !seen {
			t.Errorf("missing %q in %+v", s, got)
		}
	}
	if len(infra.Diff(a, a)) != 0 {
		t.Error("equal documents differ")
	}
}

func TestExportRoundTrip(t *testing.T) {
	st := newStack(t)
	seedDraft(st)
	st.must("infra.config.put", m{"kind": "profiles", "item": m{"id": "web", "name": "Web", "kind": "container", "units": []m{{"name": "web", "exec": "/usr/bin/web"}}}})
	st.must("infra.config.put", m{"kind": "placements", "item": m{"id": "web", "profile": "web", "sites": []m{{"site": "west", "count": 2}}}})
	doc := st.config().Draft
	files, ps := infra.Export(doc)
	if len(ps) > 0 {
		t.Fatal(ps)
	}
	for _, f := range []string{"config.yaml", "sites/west.yaml", "placements.yaml"} {
		if files[f] == "" {
			t.Fatalf("no %s in %v", f, files)
		}
	}
	back, ps := infra.Import(files)
	if len(ps) > 0 || len(infra.Diff(doc, back)) != 0 {
		t.Fatalf("round trip: %v %+v", ps, infra.Diff(doc, back))
	}
	// Importing the files replaces the draft (after a commit, so it is clean).
	st.must("infra.config.commit", m{"message": "First", "expect": st.config().Rev})
	files["sites/west.yaml"] = strings.Replace(files["sites/west.yaml"], "count: 3", "count: 4", 1)
	st.must("infra.config.import", m{"files": files})
	if c := st.config(); c.Draft.HostPools[0].Count != 4 {
		t.Fatalf("after import = %+v", c.Draft.HostPools)
	}
}

func TestExportRefusesSecrets(t *testing.T) {
	doc := infra.Document{Profiles: []infra.Profile{{ID: "web", Name: "Web", Kind: "container",
		Files: []infra.File{{Path: "/etc/key.pem", Content: "-----BEGIN RSA PRIVATE KEY-----\nabc"}}}}}
	_, ps := infra.Export(doc)
	if len(ps) != 1 || ps[0].Code != "secret_in_config" || ps[0].Path != "document.profiles[web].files[/etc/key.pem].content" {
		t.Fatalf("problems = %+v", ps)
	}
	st := newStack(t)
	r := st.send("infra.config.import", m{"document": m{"profiles": []m{{"id": "web", "name": "Web", "kind": "container", "files": []m{{"path": "/t", "content": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc"}}}}}})
	if r.Common.ErrorCode != "secret_in_config" {
		t.Fatalf("import with a JWT = %+v", r.Common)
	}
}
