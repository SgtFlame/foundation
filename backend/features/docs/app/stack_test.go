package docsapp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/branches"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	appimpl "github.com/SgtFlame/foundation/backend/features/apps/services/appservice/impl"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/cognition/traces"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/docs"
	docsapp "github.com/SgtFlame/foundation/backend/features/docs/app"
	docsnodes "github.com/SgtFlame/foundation/backend/features/docs/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	volumeimpl "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice/impl"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

const volumeID = "7340032000"

// stack is the live core with records and apps, as serve wires them, a
// knowledge base with one Volume, and the docs app imported and installed
// in the local tenant.
type stack struct {
	t      *testing.T
	reg    *extensions.Registry
	bus    *bus.Bus
	p      *project.Projections
	subs   *subscribe.Subscriptions
	eng    *engine.Engine
	data   data.Deps
	root   string // the knowledge base root
	volume string // the Volume's folder
	events volumeservice.FileEvents
	graphs map[string]cognition.Graph
	n      atomic.Int64
}

func newStack(t *testing.T) *stack {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	var (
		nodeDeps   livenodes.Deps
		draftDeps  drafts.Deps
		entityDeps entities.Deps
		dataDeps   data.Deps
		prefabDeps prefabs.Deps
		appDeps    manifests.Deps
		installDep installs.Deps
		branchDeps branches.Deps
	)
	s := &stack{t: t, reg: reg, root: t.TempDir(), events: volumeservice.NewFileEvents()}
	s.volume = filepath.Join(s.root, "handbook")
	if err := os.MkdirAll(s.volume, 0o755); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(map[string]any{"id": volumeID, "created": time.Now()})
	if err := os.WriteFile(filepath.Join(s.volume, volumeservice.MarkerFile), marker, 0o644); err != nil {
		t.Fatal(err)
	}
	volumes := volumeimpl.New(volumeimpl.WithRoot(s.root), volumeimpl.WithFileEvents(s.events))
	livenodes.Contribute(reg, func() livenodes.Deps { return nodeDeps })
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return dataDeps })
	recordsnodes.Contribute(reg, func() data.Deps { return dataDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	manifests.Contribute(reg, func() manifests.Deps { return appDeps })
	installs.Contribute(reg, func() installs.Deps { return installDep })
	branches.Contribute(reg, func() branches.Deps { return branchDeps })
	docsnodes.Contribute(reg, func() docs.Deps { return docs.Deps{Root: s.root, Volumes: volumes} })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), records.Models(), ui.Models(), apps.Models())...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	eng := engine.New(traces.In(gdb), reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	tdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "tenant.db")))
	if err != nil {
		t.Fatal(err)
	}
	tables, err := data.NewTables(tdb, p)
	if err != nil {
		t.Fatal(err)
	}
	nodeDeps = livenodes.Deps{Engine: eng, Bus: b, Subscriptions: subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: b, Tables: tables}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	branchDeps = branches.Deps{Projections: p}
	b.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	s.bus, s.p, s.subs, s.eng, s.data = b, p, subs, eng, dataDeps

	svc := appimpl.New(appimpl.WithDeps(func() appimpl.Deps {
		return appimpl.Deps{Projections: p, DB: gdb, Bus: b, Subscriptions: subs, Registry: reg}
	}))
	d, err := docsapp.Document()
	if err != nil {
		t.Fatal(err)
	}
	s.graphs = map[string]cognition.Graph{}
	for _, g := range d.Parts.Graphs {
		s.graphs[g.ID] = g.Graph
	}
	resp := svc.Import(appservice.ImportArgs{ServiceArgs: s.args(), Document: d})
	if resp.Common.Code >= 300 {
		raw, _ := json.Marshal(resp)
		t.Fatalf("import: %s %s", resp.Common.Message, raw)
	}
	s.do("app.install", docsapp.AppID, map[string]any{"version": 1})
	if err := p.Settle(context.Background(), features.LocalTenantID, installs.Projector); err != nil {
		t.Fatal(err)
	}
	until(t, "the app's commands are live", func() bool {
		_, ok := installs.Get(context.Background(), p, features.LocalTenantID, docsapp.AppID)
		return ok
	})
	stop := docs.FollowSaves(context.Background(), s.root, s.events, func() *bus.Bus { return b })
	t.Cleanup(stop)
	return s
}

func (s *stack) args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

// try sends a command as the local user.
func (s *stack) try(typ, target string, args any) bus.Result {
	raw, _ := json.Marshal(args)
	return s.bus.Execute(bus.Request{ServiceArgs: s.args(), ID: fmt.Sprintf("c%d", s.n.Add(1)), Type: typ, Target: target, Args: raw})
}

// do sends a command and fails the test when it is refused.
func (s *stack) do(typ, target string, args any) string {
	s.t.Helper()
	res := s.try(typ, target, args)
	if res.Common.Code >= 300 {
		s.t.Fatalf("%s %s: %s %+v", typ, target, res.Common.Message, res.Problems)
	}
	return res.Target
}

// call runs one of the app's function graphs (its tools) as the local
// user.
func (s *stack) call(graph string, inputs cognition.Values) cognition.Values {
	s.t.Helper()
	g := s.graphs[graph]
	res, err := s.eng.Run(engine.RunArgs{ServiceArgs: s.args(), Graph: &g, Event: "event.call", Inputs: inputs})
	if err != nil {
		s.t.Fatalf("%s: %v", graph, err)
	}
	return res.Outputs
}

// create makes a page with the createPage tool.
func (s *stack) create(parent, title string) string {
	s.t.Helper()
	out := s.call("fn-docs-createPage", cognition.Values{"volume": volumeID, "parent": parent, "title": title})
	id, _ := out["id"].(string)
	if id == "" {
		s.t.Fatalf("createPage %q: %v", title, out)
	}
	return id
}

func (s *stack) page(id string) map[string]any {
	_ = s.p.Settle(context.Background(), features.LocalTenantID, data.Projector)
	r, _, err := data.Get(s.args(), s.data, docs.EntityPage, id)
	if err != nil || r == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range r {
		var x any
		_ = json.Unmarshal(v, &x)
		out[k] = x
	}
	return out
}

// query reads one of the app's queries once.
func (s *stack) query(name string, params any) []map[string]any {
	s.t.Helper()
	raw, _ := json.Marshal(params)
	v, _, failed := s.subs.Once(s.args(), docsapp.AppID+"."+name, raw)
	if failed != nil {
		s.t.Fatalf("%s: %s", name, failed.Message)
	}
	b, _ := json.Marshal(v)
	var out struct {
		Result []map[string]any `json:"result"`
	}
	if json.Unmarshal(b, &out) != nil {
		var list []map[string]any
		_ = json.Unmarshal(b, &list)
		return list
	}
	return out.Result
}

// titles are a tree level's titles in order.
func (s *stack) titles(parent string) []string {
	var out []string
	for _, r := range s.query("tree", map[string]any{"volume": volumeID, "parent": parent}) {
		out = append(out, fmt.Sprint(r["title"]))
	}
	return out
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting until %s", what)
}

// A page is a document file and a record in one request, at the end of its
// parent's subpages, counted on its parent; the tree lists a level in order.
func TestCreatePages(t *testing.T) {
	s := newStack(t)
	a := s.create("", "Alpha")
	b := s.create("", "Beta")
	a1 := s.create(a, "Alpha one")
	a2 := s.create(a, "Alpha two")
	if got := s.titles(""); !slices.Equal(got, []string{"Alpha", "Beta"}) {
		t.Errorf("top level %v", got)
	}
	if got := s.titles(a); !slices.Equal(got, []string{"Alpha one", "Alpha two"}) {
		t.Errorf("under Alpha %v", got)
	}
	if _, err := os.Stat(filepath.Join(s.volume, "pages", a1+".doc.json")); err != nil {
		t.Errorf("no document file: %v", err)
	}
	pa, p2 := s.page(a), s.page(a2)
	if pa["childCount"] != float64(2) {
		t.Errorf("Alpha counts %v subpages", pa["childCount"])
	}
	if anc, _ := p2["ancestors"].([]any); len(anc) != 1 || anc[0] != a {
		t.Errorf("Alpha two's ancestors %v", p2["ancestors"])
	}
	if f, _ := p2["file"].(map[string]any); f["path"] != "pages/"+a2+".doc.json" || f["volumeId"] != volumeID {
		t.Errorf("file %v", p2["file"])
	}
	// a template's document is copied, and its title used
	tpl := s.create("", "SOP template")
	file := filepath.Join(s.volume, "pages", tpl+".doc.json")
	doc := `{"format":1,"doc":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Purpose"}]}]}}`
	if err := os.WriteFile(file, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	out := s.call("fn-docs-createPage", cognition.Values{"volume": volumeID, "template": tpl, "parent": b})
	made, _ := out["id"].(string)
	raw, err := os.ReadFile(filepath.Join(s.volume, "pages", made+".doc.json"))
	if err != nil || !json.Valid(raw) || !containsText(raw, "Purpose") {
		t.Errorf("the template's document was not copied: %s %v", raw, err)
	}
	if s.page(made)["title"] != "SOP template" {
		t.Errorf("title %v", s.page(made)["title"])
	}
	// a parent in no Volume of the page is refused, and the file removed
	out = s.call("fn-docs-createPage", cognition.Values{"volume": volumeID, "parent": "123"})
	if out["error"] == nil {
		t.Errorf("a missing parent was not refused: %v", out)
	}
	entries, _ := os.ReadDir(filepath.Join(s.volume, "pages"))
	if len(entries) != 6 {
		t.Errorf("%d files in pages/, want 6", len(entries))
	}
}

func containsText(raw []byte, s string) bool {
	return json.Valid(raw) && len(raw) > 0 && string(raw) != "" && (len(s) == 0 || contains(string(raw), s))
}

func contains(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}

// A move carries the subtree, keeps counts and order, and is refused into
// the page's own subtree.
func TestMove(t *testing.T) {
	s := newStack(t)
	a := s.create("", "A")
	b := s.create("", "B")
	a1 := s.create(a, "A1")
	a11 := s.create(a1, "A11")
	// A goes under B, A1 and A11 with it
	s.do("docs.page.move", a, map[string]any{"parent": b})
	if got := s.titles(""); !slices.Equal(got, []string{"B"}) {
		t.Errorf("top level %v", got)
	}
	if got := s.titles(b); !slices.Equal(got, []string{"A"}) {
		t.Errorf("under B %v", got)
	}
	if anc := s.page(a11)["ancestors"]; fmt.Sprint(anc) != fmt.Sprint([]any{b, a, a1}) {
		t.Errorf("A11's ancestors %v", anc)
	}
	if s.page(b)["childCount"] != float64(1) {
		t.Errorf("B counts %v", s.page(b)["childCount"])
	}
	// into its own subtree, or under itself: refused
	for _, parent := range []string{a1, a11, b} {
		target := a
		if parent == b {
			target = b
		}
		if res := s.try("docs.page.move", target, map[string]any{"parent": parent}); res.Common.Code < 300 {
			t.Errorf("moving %s under %s was not refused", target, parent)
		}
	}
	// order within a level
	c := s.create(b, "C")
	s.do("docs.page.move", c, map[string]any{"parent": b, "before": a})
	if got := s.titles(b); !slices.Equal(got, []string{"C", "A"}) {
		t.Errorf("under B after reordering %v", got)
	}
}

// Two people moving pages at once end with one tree: of two moves that
// would make a cycle, exactly one is kept.
func TestConcurrentMoves(t *testing.T) {
	s := newStack(t)
	for round := 0; round < 5; round++ {
		x := s.create("", fmt.Sprintf("X%d", round))
		y := s.create("", fmt.Sprintf("Y%d", round))
		var wg sync.WaitGroup
		var codes [2]int
		for i, mv := range [][2]string{{x, y}, {y, x}} {
			wg.Add(1)
			go func(i int, page, parent string) {
				defer wg.Done()
				codes[i] = int(s.try("docs.page.move", page, map[string]any{"parent": parent}).Common.Code)
			}(i, mv[0], mv[1])
		}
		wg.Wait()
		ok := 0
		for _, c := range codes {
			if c < 300 {
				ok++
			}
		}
		if ok != 1 {
			t.Fatalf("round %d: %d of the two moves were kept (%v)", round, ok, codes)
		}
		px, py := s.page(x), s.page(y)
		if (px["parent"] == y) == (py["parent"] == x) {
			t.Fatalf("round %d: not one tree: X under %v, Y under %v", round, px["parent"], py["parent"])
		}
	}
}

// Trashing a page trashes its subtree as one batch; restoring brings it back,
// and a page whose parent is gone comes back at the top.
func TestTrashAndRestore(t *testing.T) {
	s := newStack(t)
	a := s.create("", "A")
	a1 := s.create(a, "A1")
	a11 := s.create(a1, "A11")
	s.do("docs.page.trash", a1, map[string]any{})
	for _, id := range []string{a1, a11} {
		if p := s.page(id); p["trashedAt"] == nil || p["trashedWith"] != a1 {
			t.Errorf("%s not trashed with A1: %v %v", id, p["trashedAt"], p["trashedWith"])
		}
	}
	if got := s.titles(a); len(got) != 0 {
		t.Errorf("A still lists %v", got)
	}
	if s.page(a)["childCount"] != float64(0) {
		t.Errorf("A counts %v", s.page(a)["childCount"])
	}
	trash := s.query("trash", map[string]any{"volume": volumeID})
	if len(trash) != 1 || trash[0]["title"] != "A1" || trash[0]["with"] != float64(1) {
		t.Errorf("trash %v", trash)
	}
	s.do("docs.page.restore", a1, map[string]any{})
	if got := s.titles(a); !slices.Equal(got, []string{"A1"}) {
		t.Errorf("A lists %v after the restore", got)
	}
	if s.page(a11)["trashedAt"] != nil {
		t.Errorf("A11 is still in the trash")
	}
	// A1 trashed, then A: restoring A1 alone puts it at the top
	s.do("docs.page.trash", a1, map[string]any{})
	s.do("docs.page.trash", a, map[string]any{})
	s.do("docs.page.restore", a1, map[string]any{})
	if got := s.titles(""); !slices.Equal(got, []string{"A1"}) {
		t.Errorf("top level %v", got)
	}
	if anc := s.page(a11)["ancestors"]; fmt.Sprint(anc) != fmt.Sprint([]any{a1}) {
		t.Errorf("A11's ancestors %v", anc)
	}
	// deleting forever removes the files and records
	out := s.call("fn-docs-purgePage", cognition.Values{"id": a})
	if fmt.Sprint(out["deleted"]) != "1" {
		t.Errorf("purge %v", out)
	}
	if s.page(a) != nil {
		t.Errorf("A is still there")
	}
	if _, err := os.Stat(filepath.Join(s.volume, "pages", a+".doc.json")); !os.IsNotExist(err) {
		t.Errorf("A's file is still there: %v", err)
	}
}

// A page's document written (by a room's writer) marks it saved.
func TestSaved(t *testing.T) {
	s := newStack(t)
	a := s.create("", "A")
	file := filepath.Join(s.volume, "pages", a+".doc.json")
	if err := os.WriteFile(file, []byte(`{"format":1,"doc":{"type":"doc","content":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.events.Publish(volumeservice.FileEvent{Op: volumeservice.FileChanged, File: file})
	until(t, "the page is marked saved", func() bool {
		p := s.page(a)
		return p["lastSavedAt"] != nil && p["hash"] != nil
	})
}

// A thousand pages list per parent within budget.
func TestManyPages(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	s := newStack(t)
	parent := s.create("", "Big")
	ranks := records.EvenRanks(1000)
	for i := 0; i < 1000; i++ {
		s.do("record.create", "", map[string]any{"entity": docs.EntityPage, "values": map[string]any{
			"title": fmt.Sprintf("Page %04d", i), "volume": volumeID, "parent": parent, "ancestors": []string{parent}, "rank": ranks[i]}})
	}
	start := time.Now()
	got := s.titles(parent)
	took := time.Since(start)
	if len(got) != 1000 || got[0] != "Page 0000" || got[999] != "Page 0999" {
		t.Fatalf("%d pages listed, first %v", len(got), got[:min(3, len(got))])
	}
	if took > 2*time.Second {
		t.Errorf("listing 1000 pages took %v", took)
	}
	t.Logf("listed 1000 pages in %v", took)
}
