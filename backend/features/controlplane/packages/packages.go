// Package packages is the first-party apt repository (forge 20f-1): a
// repository that scripts/repo.sh builds and signs is published into the
// control account's object store ctl_repo, and every NATS node's instance
// serves it over HTTP (`axiomd repo-serve`), so a site updates with no
// outside address. It is shared by serve's CLI and axiomd, so it imports
// neither apps nor records.
package packages

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("controlplane").Tags("packages")

// Current names the generation of the index being served. A publish
// uploads the pool first, then the new index under gen/<generation>/,
// then Current: a client never sees an index naming a file not there, nor
// half of one index and half of another.
const Current = "current"

// KeepGenerations is how many indexes a publish keeps.
var KeepGenerations = 3

// beforePut, when set (tests), runs before each object is put.
var beforePut func(name string) error

// Store opens, or makes, the repository's object store.
func Store(ctx context.Context, js jetstream.JetStream) (jetstream.ObjectStore, error) {
	os, err := js.CreateOrUpdateObjectStore(ctx, natsd.Object(jetstream.ObjectStoreConfig{
		Bucket: ctl.RepoBucket, Description: "The first-party apt repository (forge 20f-1)", Storage: jetstream.FileStorage,
	}))
	return os, log.Wrap(err)
}

// Published is what a publish uploaded.
type Published struct {
	Generation string   `json:"generation"`
	Uploaded   []string `json:"uploaded"`
	Kept       int      `json:"kept"`
}

// Publish uploads a repository directory (pool/ and dists/, as repo.sh
// writes it): pool files not already there with the same digest, then
// the index as a new generation, then Current; it drops generations past
// KeepGenerations.
func Publish(ctx context.Context, js jetstream.JetStream, dir string) (Published, error) {
	var out Published
	store, err := Store(ctx, js)
	if err != nil {
		return out, err
	}
	pool, err := files(filepath.Join(dir, "pool"))
	if err != nil {
		return out, err
	}
	dists, err := files(filepath.Join(dir, "dists"))
	if err != nil {
		return out, err
	}
	if len(dists) == 0 {
		return out, log.Code("no_index").Errorf("%s has no dists/: build it with scripts/repo.sh", dir)
	}
	for _, rel := range pool {
		name := "pool/" + rel
		local := filepath.Join(dir, "pool", rel)
		if info, err := store.GetInfo(ctx, name); err == nil {
			if d, err := digest(local); err == nil && d == info.Digest {
				out.Kept++
				continue
			}
		}
		if err := put(ctx, store, name, local); err != nil {
			return out, err
		}
		out.Uploaded = append(out.Uploaded, name)
	}
	gen := time.Now().UTC().Format("20060102T150405.000000000Z")
	for _, rel := range dists {
		name := "gen/" + gen + "/dists/" + rel
		if err := put(ctx, store, name, filepath.Join(dir, "dists", rel)); err != nil {
			return out, err
		}
		out.Uploaded = append(out.Uploaded, name)
	}
	if beforePut != nil {
		if err := beforePut(Current); err != nil {
			return out, err
		}
	}
	if _, err := store.PutString(ctx, Current, gen); err != nil {
		return out, log.Wrapf(err, "publish the index")
	}
	out.Generation = gen
	prune(ctx, store, gen)
	return out, nil
}

func put(ctx context.Context, store jetstream.ObjectStore, name, local string) error {
	if beforePut != nil {
		if err := beforePut(name); err != nil {
			return err
		}
	}
	f, err := os.Open(local)
	if err != nil {
		return log.Wrap(err)
	}
	defer f.Close()
	_, err = store.Put(ctx, jetstream.ObjectMeta{Name: name}, f)
	return log.Wrapf(err, "upload %s", name)
}

// files lists a directory's regular files, relative and slashed.
func files(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, log.Wrap(err)
}

// digest is a file's digest as the object store writes it.
func digest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "SHA-256=" + base64.URLEncoding.EncodeToString(h.Sum(nil)), nil
}

// prune deletes generations older than the last KeepGenerations.
func prune(ctx context.Context, store jetstream.ObjectStore, current string) {
	list, err := store.List(ctx)
	if err != nil {
		return
	}
	byGen := map[string][]string{}
	for _, o := range list {
		if rest, ok := strings.CutPrefix(o.Name, "gen/"); ok {
			g, _, _ := strings.Cut(rest, "/")
			byGen[g] = append(byGen[g], o.Name)
		}
	}
	gens := make([]string, 0, len(byGen))
	for g := range byGen {
		gens = append(gens, g)
	}
	sort.Strings(gens)
	for i := 0; i < len(gens)-KeepGenerations; i++ {
		if gens[i] == current {
			continue
		}
		for _, name := range byGen[gens[i]] {
			_ = store.Delete(ctx, name)
		}
	}
}

// Server serves the repository read-only: /dists/… from the current
// generation, /pool/… as it is, and /healthz.
type Server struct {
	JS jetstream.JetStream
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "read only", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	store, err := s.JS.ObjectStore(ctx, ctl.RepoBucket)
	if err != nil {
		http.Error(w, "no repository", http.StatusServiceUnavailable)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	var name string
	switch {
	case p == "/healthz":
		_, _ = io.WriteString(w, "ok\n")
		return
	case strings.HasPrefix(p, "/dists/"):
		gen, err := store.GetString(ctx, Current)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name = "gen/" + strings.TrimSpace(gen) + p
	case strings.HasPrefix(p, "/pool/"):
		name = strings.TrimPrefix(p, "/")
	default:
		http.NotFound(w, r)
		return
	}
	obj, err := store.Get(ctx, name)
	if err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "cannot read the repository", http.StatusBadGateway)
		}
		return
	}
	defer obj.Close()
	info, err := obj.Info()
	if err == nil {
		w.Header().Set("Content-Length", strconv.FormatUint(info.Size, 10))
	}
	w.Header().Set("Content-Type", contentType(p))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, obj)
}

func contentType(p string) string {
	switch {
	case strings.HasSuffix(p, ".deb"):
		return "application/vnd.debian.binary-package"
	case strings.HasSuffix(p, ".gz"):
		return "application/gzip"
	case strings.HasSuffix(p, ".gpg"):
		return "application/pgp-signature"
	}
	return "text/plain; charset=utf-8"
}
