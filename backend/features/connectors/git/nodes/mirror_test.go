package gitnodes_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git/gittest"
	gitnodes "github.com/SgtFlame/foundation/backend/features/connectors/git/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type mirrorWorld struct {
	sess   adapter.Session
	root   string
	deps   gitnodes.Deps
	mu     sync.Mutex
	events []string
}

func appArgs() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(features.LocalTenantID, "gitMirror")}
}

func mirrorSetup(t *testing.T) *mirrorWorld {
	t.Helper()
	s := gittest.New(nil)
	a := appArgs()
	sess, err := s.Impl().Open(adapter.Context{Context: context.Background(), Args: a, Connection: "site"}, adapter.Connection{ID: "site", Type: gittest.TypeID, Tenant: features.LocalTenantID})
	if err != nil {
		t.Fatal(err)
	}
	w := &mirrorWorld{sess: sess, root: t.TempDir()}
	ev := volumeservice.NewFileEvents()
	ev.Subscribe(func(e volumeservice.FileEvent) {
		w.mu.Lock()
		defer w.mu.Unlock()
		rel, _ := filepath.Rel(w.root, e.File)
		w.events = append(w.events, filepath.ToSlash(rel))
	})
	w.deps = gitnodes.Deps{Mirrors: volumeservice.Mirrors{Root: w.root, Events: ev},
		Call: func(a features.ServiceArgs, c connectors.Call) (json.RawMessage, *connectors.Error) {
			out, err := sess.Call(adapter.Context{Context: a.Context(), Args: a, Contract: c.Contract, Operation: c.Operation, Connection: c.Connection}, c.Operation, c.Args)
			if err != nil {
				return nil, err.(*connectors.Error)
			}
			return out, nil
		}}
	return w
}

// push commits as someone outside the repository.
func (w *mirrorWorld) push(t *testing.T, changes ...map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"branch": "main", "changes": changes})
	if _, err := w.sess.Call(adapter.Context{Context: context.Background(), Args: appArgs(), Contract: gittest.ControlRef, Operation: "push", Connection: "site"}, "push", raw); err != nil {
		t.Fatal(err)
	}
}

func (w *mirrorWorld) read(name string) string {
	b, err := os.ReadFile(filepath.Join(w.root, "mirrors", "site", filepath.FromSlash(name)))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestMirror(t *testing.T) {
	w := mirrorSetup(t)
	w.push(t,
		map[string]any{"path": "www/content/code/a/index.md", "content": "# A\n"},
		map[string]any{"path": "www/content/node_modules/x.js", "content": "x"},
		map[string]any{"path": "www/content/big.txt", "content": strings.Repeat("a", gitnodes.MaxMirrorFile+1)},
		map[string]any{"path": "www/hugo.toml", "content": "title = 'x'"})
	// Binary content through the type's own commit, which takes base64
	raw, _ := json.Marshal(map[string]any{"branch": "main", "message": "A picture", "changes": []any{
		map[string]any{"path": "www/content/code/a/pic.png", "content": base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G', 0}), "encoding": "base64"}}})
	if _, err := w.sess.Call(adapter.Context{Context: context.Background(), Args: appArgs(), Contract: "git.repo@1", Operation: "commit", Connection: "site"}, "commit", raw); err != nil {
		t.Fatal(err)
	}
	in := gitnodes.SyncInput{Connection: "site", Ref: "main", Path: "www/content", Volume: "site"}
	res, e := w.deps.Sync(appArgs(), in)
	if e != nil {
		t.Fatal(e)
	}
	if !res.Full || res.Written != 2 || res.Skipped != 2 || res.VolumeID == 0 {
		t.Fatalf("the first sync: %+v", res)
	}
	if w.read("code/a/index.md") != "# A\n" || w.read("code/a/pic.png") != "\x89PNG\x00" || w.read("big.txt") != "<missing>" || w.read("node_modules/x.js") != "<missing>" {
		t.Fatalf("the mirrored files: %q %q %q %q", w.read("code/a/index.md"), w.read("code/a/pic.png"), w.read("big.txt"), w.read("node_modules/x.js"))
	}
	if !slices.Contains(w.events, "mirrors/site/code/a/index.md") {
		t.Fatalf("the index was not told: %v", w.events)
	}
	// People cannot write it; the marker names the mirror
	dir := filepath.Join(w.root, "mirrors", "site", "code")
	user := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	if volumeservice.May(user, w.root, dir, "write") || volumeservice.Check(user, w.root, dir, "write") == nil {
		t.Fatal("a person may write a mirrored Volume")
	}
	if err := (volumeservice.Mirrors{Root: w.root}).Write(user, "site", "x.md", []byte("x")); err == nil {
		t.Fatal("a person wrote through Mirrors")
	}
	if m, _ := volumeservice.ReadMarker(filepath.Join(w.root, "mirrors", "site")); m.Mirror == nil || m.Mirror.Path != "www/content" {
		t.Fatalf("the marker: %+v", m)
	}
	// An increment: one changed, one deleted, one added
	w.push(t,
		map[string]any{"path": "www/content/code/a/index.md", "content": "# A, again\n"},
		map[string]any{"path": "www/content/code/a/pic.png", "delete": true},
		map[string]any{"path": "www/content/code/b.md", "content": "# B\n"},
		map[string]any{"path": "README.md", "content": "outside the mirrored folder"})
	in.From = res.SHA
	res2, e := w.deps.Sync(appArgs(), in)
	if e != nil {
		t.Fatal(e)
	}
	if res2.Full || res2.Written != 2 || res2.Removed != 1 || res2.SHA == res.SHA || res2.VolumeID != res.VolumeID {
		t.Fatalf("the increment: %+v", res2)
	}
	if w.read("code/a/index.md") != "# A, again\n" || w.read("code/a/pic.png") != "<missing>" || w.read("code/b.md") != "# B\n" {
		t.Fatal("the increment's files")
	}
	// Nothing new: nothing done
	in.From = res2.SHA
	if res3, _ := w.deps.Sync(appArgs(), in); res3.Written+res3.Removed != 0 {
		t.Fatalf("an idle sync: %+v", res3)
	}
	// A lost From (a force push) copies everything and removes strays
	_ = os.WriteFile(filepath.Join(w.root, "mirrors", "site", "stray.md"), []byte("x"), 0o644)
	in.From = "0000000000000000000000000000000000000000"
	res4, e := w.deps.Sync(appArgs(), in)
	if e != nil || !res4.Full || w.read("stray.md") != "<missing>" || w.read("code/b.md") != "# B\n" {
		t.Fatalf("a lost From: %+v %v", res4, e)
	}
	// A Volume name used for another mirror is refused
	if _, e := w.deps.Sync(appArgs(), gitnodes.SyncInput{Connection: "site", Ref: "main", Volume: "site"}); e == nil || e.Code != connectors.CodeInvalid {
		t.Fatalf("a second mirror into one Volume: %v", e)
	}
	_ = kbservice.InTenant
}

func TestMirrorNode(t *testing.T) {
	w := mirrorSetup(t)
	w.push(t, map[string]any{"path": "docs/a.md", "content": "# A\n"})
	var node cognition.NodeType
	r := extensions.NewRegistry()
	r.Declare(cognition.Nodes)
	gitnodes.Contribute(r, func() gitnodes.Deps { return w.deps })
	for _, n := range cognition.Nodes.All(r) {
		if n.ID == gitnodes.NodeMirror {
			node = n
		}
	}
	out, pin, err := node.Run(&cognition.Exec{ServiceArgs: appArgs()}, cognition.Values{"connection": "site", "ref": "main", "path": "docs", "volume": "docs"})
	if err != nil || pin != cognition.Then || out["written"] != float64(1) || out["sha"] == "" {
		t.Fatalf("%v %s %v", out, pin, err)
	}
	// As a person: refused, by Error
	out, pin, _ = node.Run(exec("n"), cognition.Values{"connection": "site", "ref": "main", "volume": "other"})
	if pin != gitnodes.Failed {
		t.Fatalf("a person's sync: %v", out)
	}
}
