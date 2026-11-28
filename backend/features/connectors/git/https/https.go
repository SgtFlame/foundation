// Package https is the git.https connector type (forge 24c): any git host
// that speaks smart HTTP(S) (Gitea, GitLab, Bitbucket, a self-hosted
// server) through go-git, entirely in memory. Each call fetches only what
// it needs, shallow, of one ref, into a fresh in-memory repository within
// the size and file-count limits; a commit is built in memory and pushed
// without force, so a branch that moved meanwhile refuses it (conflict).
// Authentication is a username with a token or password; git.ssh (forge
// 24f) shares this session through NewWith with a key of its own.
package https

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// TypeID is the type's id.
const TypeID = "git.https"

// SettingsSchema is a connection's settings; the token is a secret.
const SettingsSchema = `{"type":"object","properties":{
  "url":{"type":"string","title":"Repository URL","description":"The repository's HTTPS clone URL, such as https://gitea.example.com/acme/site.git"},
  "username":{"type":"string","title":"Username","description":"The account to sign in as; some hosts take any name with a token (oauth2, x-access-token)"},
  "token":{"type":"secret","title":"Token or password","description":"An access token (or the password) with read and write access to the repository"},
  "pollSeconds":{"type":"integer","minimum":15,"title":"Poll every","description":"Seconds between looks for pushes; 60 when left out"},
  "allowDefaultBranch":{"type":"boolean","title":"Allow commits to the default branch","description":"Let the git tools and nodes commit straight to the default branch; otherwise they commit to another branch"}},"required":["url"]}`

// Type is git.https.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{git.RepoRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Services: []string{"files"},
		Meta:     connectors.Meta{Title: "Git over HTTPS", Description: "Any git repository reachable over HTTPS (Gitea, GitLab, Bitbucket, a self-hosted server), with a username and token: reads branches, trees and files by fetching shallowly into memory, and commits by pushing to a branch. Notices pushes by polling. Has no pull requests; use the host's own type where there is one (GitHub).", Tags: []string{"git", "code"}},
	}
}

// Contribute registers the type.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, git.Source, nil, []connectors.Type{Type()})
}

// Settings are a connection's settings, resolved.
type Settings struct {
	URL                string `json:"url"`
	Username           string `json:"username"`
	Token              string `json:"token"`
	PollSeconds        int    `json:"pollSeconds"`
	AllowDefaultBranch bool   `json:"allowDefaultBranch"`
}

// Limits bound what one call fetches.
type Limits struct {
	MaxBytes int64 // the objects one fetch may bring, 200 MB when 0
	MaxFiles int   // the files a tree may hold, 20000 when 0
}

// Options configure the implementation.
type Options struct {
	Files  git.Files
	Limits Limits
	// Poll overrides pollSeconds, for tests.
	Poll time.Duration
	// AllowHTTP lets a connection's URL be plain http (tests only).
	AllowHTTP bool
}

// Impl is git.https's adapter implementation.
func Impl(o Options) adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if len(conn.Settings) > 0 {
			if err := json.Unmarshal(conn.Settings, &st); err != nil {
				return nil, connectors.Errorf(connectors.CodeInvalid, "the connection's settings: %v", err)
			}
		}
		return New(st, o)
	}}
}

// Session is one connection.
type Session struct {
	st     Settings
	o      Options
	tr     Transport
	poll   time.Duration
	remote *gogit.Remote
}

// Transport is how a session signs in to its remote: git.https' username
// and token, or another type's (git.ssh, forge 24f), which shares this
// session's fetch, commit and watch code.
type Transport struct {
	// TypeID names the type in errors.
	TypeID string
	// Auth gives the sign-in for one request; its error fails the call
	// (git.ssh's locked key). Nil signs in as nobody.
	Auth func() (transport.AuthMethod, error)
	// CanWrite is what check reports.
	CanWrite bool
}

// New makes a session from settings.
func New(st Settings, o Options) (*Session, error) {
	if !strings.HasPrefix(st.URL, "https://") && !(o.AllowHTTP && strings.HasPrefix(st.URL, "http://")) {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the repository URL must start with https://")
	}
	tr := Transport{TypeID: TypeID, CanWrite: st.Token != ""}
	if st.Token != "" || st.Username != "" {
		user := st.Username
		if user == "" {
			user = "git"
		}
		auth := &githttp.BasicAuth{Username: user, Password: st.Token}
		tr.Auth = func() (transport.AuthMethod, error) { return auth, nil }
	}
	return NewWith(st, tr, o), nil
}

// NewWith makes a session that signs in through tr; the URL is the
// caller's to check.
func NewWith(st Settings, tr Transport, o Options) *Session {
	if o.Limits.MaxBytes == 0 {
		o.Limits.MaxBytes = 200 << 20
	}
	if o.Limits.MaxFiles == 0 {
		o.Limits.MaxFiles = 20000
	}
	if tr.TypeID == "" {
		tr.TypeID = TypeID
	}
	s := &Session{st: st, o: o, tr: tr, poll: o.Poll}
	if s.poll == 0 {
		s.poll = time.Duration(max(st.PollSeconds, 15)) * time.Second
		if st.PollSeconds == 0 {
			s.poll = time.Minute
		}
	}
	s.remote = gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{st.URL}})
	return s
}

// auth is the sign-in for one request.
func (s *Session) auth() (transport.AuthMethod, error) {
	if s.tr.Auth == nil {
		return nil, nil
	}
	return s.tr.Auth()
}

func init() {
	// go-git's protocol clients are global: one per scheme with a bounded
	// timeout, shared by every session (only this package uses go-git)
	client.InstallProtocol("https", githttp.NewClient(&http.Client{Timeout: 2 * time.Minute}))
	client.InstallProtocol("http", githttp.NewClient(&http.Client{Timeout: 2 * time.Minute}))
}

func (s *Session) Close() error { return nil }

// mapErr turns a go-git error into a connector error.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return ce
	}
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed), strings.Contains(err.Error(), "unable to authenticate"):
		return connectors.Errorf(connectors.CodeAuth, "the host refused the credentials")
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return connectors.Errorf(connectors.CodeNotFound, "the host has no such repository")
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		return connectors.Errorf(connectors.CodeNotFound, "the repository is empty")
	case errors.Is(err, gogit.ErrForceNeeded), strings.Contains(err.Error(), "non-fast-forward"), strings.Contains(err.Error(), "fetch first"), strings.Contains(err.Error(), "required to be"):
		return connectors.Errorf(connectors.CodeConflict, "the branch moved: read it again")
	case strings.Contains(err.Error(), "protected"), strings.Contains(err.Error(), "pre-receive hook declined"):
		return connectors.Errorf(connectors.CodeInvalid, "the host refused the push: %v", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &connectors.Error{Code: connectors.CodeUnavailable, Message: "the host did not answer in time", Retry: true}
	}
	return &connectors.Error{Code: connectors.CodeUnavailable, Message: "the host: " + err.Error(), Retry: true}
}

// advertised lists the remote's refs and its default branch.
func (s *Session) advertised(ctx context.Context) (map[string]string, string, error) {
	auth, err := s.auth()
	if err != nil {
		return nil, "", err
	}
	refs, err := s.remote.ListContext(ctx, &gogit.ListOptions{Auth: auth})
	if err != nil {
		return nil, "", mapErr(err)
	}
	out := map[string]string{}
	def := ""
	for _, r := range refs {
		switch {
		case r.Name() == plumbing.HEAD && r.Type() == plumbing.SymbolicReference:
			def = r.Target().Short()
		case r.Name().IsBranch() || r.Name().IsTag():
			if r.Type() == plumbing.HashReference {
				out[r.Name().String()] = r.Hash().String()
			}
		}
	}
	if def == "" {
		for _, b := range []string{"main", "master"} {
			if _, ok := out["refs/heads/"+b]; ok {
				def = b
				break
			}
		}
	}
	return out, def, nil
}

var errTooLarge = errors.New("too large")

// fetched is a fresh in-memory repository holding what one fetch brought.
type fetched struct {
	repo *gogit.Repository
	refs map[string]string // the advertised refs at the fetch
	def  string
}

// fetch brings refs (full names) into a fresh repository, depth commits
// deep; refs empty fetches only what the remote advertises.
func (s *Session) fetch(ctx context.Context, refs []string, depth int) (*fetched, error) {
	adv, def, err := s.advertised(ctx)
	if err != nil {
		return nil, err
	}
	repo, err := gogit.Init(memory.NewStorage(), nil)
	if err != nil {
		return nil, mapErr(err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{s.st.URL}}); err != nil {
		return nil, mapErr(err)
	}
	var specs []config.RefSpec
	for _, r := range refs {
		if _, ok := adv[r]; ok {
			specs = append(specs, config.RefSpec("+"+r+":"+r))
		}
	}
	f := &fetched{repo: repo, refs: adv, def: def}
	if len(specs) == 0 {
		return f, nil
	}
	auth, err := s.auth()
	if err != nil {
		return nil, err
	}
	err = repo.FetchContext(ctx, &gogit.FetchOptions{RemoteName: "origin", RefSpecs: specs, Depth: depth, Auth: auth, Tags: gogit.NoTags,
		Progress: sideband.Progress(io.Discard)})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil, mapErr(err)
	}
	// The objects' size bound: what the in-memory store holds
	var total int64
	iter, err := repo.Storer.IterEncodedObjects(plumbing.AnyObject)
	if err == nil {
		_ = iter.ForEach(func(o plumbing.EncodedObject) error {
			total += o.Size()
			if total > s.o.Limits.MaxBytes {
				return errTooLarge
			}
			return nil
		})
	}
	if total > s.o.Limits.MaxBytes {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the repository is over %d MB fetched; this type reads repositories up to that", s.o.Limits.MaxBytes>>20)
	}
	return f, nil
}

// refName is the full ref a ref names, or "" for a sha (or unknown).
func refName(adv map[string]string, ref, def string) string {
	if ref == "" || ref == "HEAD" {
		ref = def
	}
	for _, c := range []string{ref, "refs/heads/" + ref, "refs/tags/" + ref} {
		if _, ok := adv[c]; ok {
			return c
		}
	}
	return ""
}

// commitAt fetches what a ref names and gives its commit: a branch or tag
// shallowly; a sha by fetching every branch KeyDepth deep and finding it.
func (s *Session) commitAt(ctx context.Context, ref string, depth int) (*fetched, *object.Commit, error) {
	adv, def, err := s.advertised(ctx)
	if err != nil {
		return nil, nil, err
	}
	if name := refName(adv, ref, def); name != "" {
		f, err := s.fetch(ctx, []string{name}, depth)
		if err != nil {
			return nil, nil, err
		}
		c, err := f.repo.CommitObject(plumbing.NewHash(f.refs[name]))
		if err != nil {
			return nil, nil, connectors.Errorf(connectors.CodeUnavailable, "the fetched commit is missing: %v", err)
		}
		return f, c, nil
	}
	if !plumbing.IsHash(ref) && (len(ref) < 7 || strings.HasPrefix(ref, "refs/")) {
		return nil, nil, connectors.Errorf(connectors.CodeNotFound, "there is no ref or commit %q", ref)
	}
	var heads []string
	for n := range adv {
		if strings.HasPrefix(n, "refs/heads/") {
			heads = append(heads, n)
		}
	}
	sort.Strings(heads)
	f, err := s.fetch(ctx, heads, max(depth, git.KeyDepth))
	if err != nil {
		return nil, nil, err
	}
	c, err := findCommit(f.repo, ref)
	if err != nil {
		return nil, nil, err
	}
	return f, c, nil
}

func findCommit(repo *gogit.Repository, ref string) (*object.Commit, error) {
	if plumbing.IsHash(ref) {
		if c, err := repo.CommitObject(plumbing.NewHash(ref)); err == nil {
			return c, nil
		}
		return nil, connectors.Errorf(connectors.CodeNotFound, "there is no commit %s within %d commits of a branch", ref, git.KeyDepth)
	}
	iter, err := repo.CommitObjects()
	if err != nil {
		return nil, mapErr(err)
	}
	var found *object.Commit
	_ = iter.ForEach(func(c *object.Commit) error {
		if strings.HasPrefix(c.Hash.String(), ref) {
			found = c
		}
		return nil
	})
	if found == nil {
		return nil, connectors.Errorf(connectors.CodeNotFound, "there is no ref or commit %q", ref)
	}
	return found, nil
}

// files flattens a commit's tree (path → entry), within the file limit.
func (s *Session) files(c *object.Commit) (map[string]git.Entry, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the tree is missing: %v", err)
	}
	out := map[string]git.Entry{}
	err = tree.Files().ForEach(func(f *object.File) error {
		if len(out) >= s.o.Limits.MaxFiles {
			return errTooLarge
		}
		out[f.Name] = git.Entry{Path: f.Name, Kind: "file", Size: f.Size, SHA: f.Hash.String()}
		return nil
	})
	if errors.Is(err, errTooLarge) {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the repository has more than %d files; this type reads repositories up to that", s.o.Limits.MaxFiles)
	}
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "reading the tree: %v", err)
	}
	return out, nil
}

func snapshot(files map[string]git.Entry) map[string]string {
	out := make(map[string]string, len(files))
	for p, e := range files {
		out[p] = e.SHA
	}
	return out
}

// Call runs one operation.
func (s *Session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	switch op {
	case "check":
		_, def, err := s.advertised(ctx)
		if err != nil {
			return nil, err
		}
		// Whether a push would be allowed is known only by pushing; a
		// token given is taken to write
		return git.Result(map[string]any{"ok": true, "defaultBranch": def, "canWrite": s.tr.CanWrite, "allowDefaultBranch": s.st.AllowDefaultBranch})
	case "listRefs":
		var a struct{ Prefix string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		adv, def, err := s.advertised(ctx)
		if err != nil {
			return nil, err
		}
		refs := []git.Ref{}
		for n, sha := range adv {
			if strings.HasPrefix(n, a.Prefix) {
				refs = append(refs, git.Ref{Name: n, SHA: sha})
			}
		}
		sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
		return git.Result(map[string]any{"refs": refs, "defaultBranch": def})
	case "readTree":
		var a struct {
			Ref, Path, Cursor string
			Recursive         bool
			Limit             int
		}
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		dir, err := git.CleanDir(a.Path)
		if err != nil {
			return nil, err
		}
		_, c, err := s.commitAt(ctx, a.Ref, 1)
		if err != nil {
			return nil, err
		}
		files, err := s.files(c)
		if err != nil {
			return nil, err
		}
		page, next, err := git.List(files, dir, a.Recursive, a.Cursor, a.Limit)
		if err != nil {
			return nil, err
		}
		return git.Result(git.Tree{SHA: c.Hash.String(), Entries: page, Next: next})
	case "readFile":
		var a struct{ Ref, Path string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		p, err := git.CleanPath(a.Path)
		if err != nil {
			return nil, err
		}
		_, c, err := s.commitAt(ctx, a.Ref, 1)
		if err != nil {
			return nil, err
		}
		f, err := c.File(p)
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no file %s at %s", p, a.Ref)
		}
		if f.Size > git.MaxFileBytes {
			return nil, connectors.Errorf(connectors.CodeInvalid, "%s is over %d MB", p, git.MaxFileBytes>>20)
		}
		r, err := f.Reader()
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeUnavailable, "reading %s: %v", p, err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeUnavailable, "reading %s: %v", p, err)
		}
		res, err := git.FileResult(ctx, s.o.Files, p, f.Hash.String(), data)
		if err != nil {
			return nil, err
		}
		return git.Result(res)
	case "compare":
		var a struct{ Base, Head string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		cmp, err := s.compare(ctx, a.Base, a.Head)
		if err != nil {
			return nil, err
		}
		return git.Result(cmp)
	case "commit":
		var a git.CommitArgs
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		writes, err := git.Decode(ctx, s.o.Files, &a)
		if err != nil {
			return nil, err
		}
		res, err := s.commit(ctx, a, writes)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(res)
		_ = ctx.Accepted(raw)
		return raw, nil
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "%s has no operation %q", s.tr.TypeID, op)
}

func (s *Session) compare(ctx context.Context, base, head string) (git.Comparison, error) {
	_, b, err := s.commitAt(ctx, base, 1)
	if err != nil {
		return git.Comparison{}, err
	}
	_, h, err := s.commitAt(ctx, head, 1)
	if err != nil {
		return git.Comparison{}, err
	}
	bf, err := s.files(b)
	if err != nil {
		return git.Comparison{}, err
	}
	hf, err := s.files(h)
	if err != nil {
		return git.Comparison{}, err
	}
	return git.Comparison{Base: b.Hash.String(), Head: h.Hash.String(), Changes: git.Diff(snapshot(bf), snapshot(hf))}, nil
}

// commit builds the commit in memory and pushes it without force.
func (s *Session) commit(ctx context.Context, a git.CommitArgs, writes []git.Write) (git.CommitResult, error) {
	adv, def, err := s.advertised(ctx)
	if err != nil {
		return git.CommitResult{}, err
	}
	branchRef := "refs/heads/" + a.Branch
	head := adv[branchRef]
	if a.Key != "" && head != "" {
		// A key already on the branch: this is a retry
		f, err := s.fetch(ctx, []string{branchRef}, git.KeyDepth)
		if err != nil {
			return git.CommitResult{}, err
		}
		c, err := f.repo.CommitObject(plumbing.NewHash(head))
		for i := 0; err == nil && i < git.KeyDepth; i++ {
			if git.HasKey(c.Message, a.Key) {
				before := ""
				if len(c.ParentHashes) > 0 {
					before = c.ParentHashes[0].String()
				}
				return git.CommitResult{SHA: c.Hash.String(), Branch: a.Branch, Before: before}, nil
			}
			if len(c.ParentHashes) == 0 {
				break
			}
			c, err = f.repo.CommitObject(c.ParentHashes[0])
		}
	}
	if a.ExpectHead != "" && a.ExpectHead != head {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeConflict, "branch %s has moved since %s", a.Branch, a.ExpectHead[:min(7, len(a.ExpectHead))])
	}
	from := a.Base
	if head != "" {
		from = branchRef
	} else if from == "" {
		from = def
	}
	f, parent, err := s.commitAt(ctx, from, 1)
	if err != nil {
		return git.CommitResult{}, err
	}
	if head != "" && parent.Hash.String() != head {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeConflict, "branch %s moved while committing: read it again", a.Branch)
	}
	files, err := s.files(parent)
	if err != nil {
		return git.CommitResult{}, err
	}
	modes := map[string]filemode.FileMode{}
	parentTree, _ := parent.Tree()
	_ = parentTree.Files().ForEach(func(fl *object.File) error { modes[fl.Name] = fl.Mode; return nil })
	tree := snapshot(files)
	st := f.repo.Storer
	for _, w := range writes {
		if w.Deleted {
			delete(tree, w.Path)
			delete(modes, w.Path)
			continue
		}
		for p := range tree {
			if strings.HasPrefix(p, w.Path+"/") || strings.HasPrefix(w.Path, p+"/") {
				return git.CommitResult{}, connectors.Errorf(connectors.CodeInvalid, "%s and %s cannot both exist", w.Path, p)
			}
		}
		obj := st.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		wr, _ := obj.Writer()
		_, _ = wr.Write(w.Data)
		_ = wr.Close()
		h, err := st.SetEncodedObject(obj)
		if err != nil {
			return git.CommitResult{}, mapErr(err)
		}
		tree[w.Path] = h.String()
		if _, ok := modes[w.Path]; !ok {
			modes[w.Path] = filemode.Regular
		}
	}
	if len(git.Diff(snapshot(files), tree)) == 0 && head != "" {
		return git.CommitResult{SHA: head, Branch: a.Branch, Before: head, Unchanged: true}, nil
	}
	root, err := writeTree(st, tree, modes)
	if err != nil {
		return git.CommitResult{}, mapErr(err)
	}
	name, email := "Axiom", "axiom@localhost"
	if a.Author != nil && a.Author.Name != "" {
		name, email = a.Author.Name, a.Author.Email
	}
	now := time.Now()
	c := &object.Commit{Author: object.Signature{Name: name, Email: email, When: now}, Committer: object.Signature{Name: "Axiom", Email: "axiom@localhost", When: now},
		Message: git.WithKey(a.Message, a.Key), TreeHash: root, ParentHashes: []plumbing.Hash{parent.Hash}}
	obj := st.NewEncodedObject()
	if err := c.Encode(obj); err != nil {
		return git.CommitResult{}, mapErr(err)
	}
	sha, err := st.SetEncodedObject(obj)
	if err != nil {
		return git.CommitResult{}, mapErr(err)
	}
	// Push without force: refused when the branch moved since the fetch
	// (or was created by someone else meanwhile)
	spec := config.RefSpec(sha.String() + ":" + branchRef)
	auth, err := s.auth()
	if err != nil {
		return git.CommitResult{}, err
	}
	err = f.repo.PushContext(ctx, &gogit.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{spec}, Auth: auth, Progress: sideband.Progress(io.Discard),
		RequireRemoteRefs: requireRemote(branchRef, head)})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return git.CommitResult{}, mapErr(err)
	}
	return git.CommitResult{SHA: sha.String(), Branch: a.Branch, Before: head, Created: head == ""}, nil
}

// requireRemote makes the push refuse unless the branch is still where it
// was read (a compare-and-swap); none for a new branch, where a racing
// create is refused as non-fast-forward.
func requireRemote(ref, head string) []config.RefSpec {
	if head == "" {
		return nil
	}
	return []config.RefSpec{config.RefSpec(head + ":" + ref)}
}

// writeTree writes nested tree objects for a flat snapshot and gives the
// root's hash.
func writeTree(st interface {
	NewEncodedObject() plumbing.EncodedObject
	SetEncodedObject(plumbing.EncodedObject) (plumbing.Hash, error)
}, files map[string]string, modes map[string]filemode.FileMode) (plumbing.Hash, error) {
	type dir struct {
		files map[string]string
		dirs  map[string]*dir
	}
	root := &dir{files: map[string]string{}, dirs: map[string]*dir{}}
	for p, sha := range files {
		d := root
		parts := strings.Split(p, "/")
		for _, seg := range parts[:len(parts)-1] {
			nd := d.dirs[seg]
			if nd == nil {
				nd = &dir{files: map[string]string{}, dirs: map[string]*dir{}}
				d.dirs[seg] = nd
			}
			d = nd
		}
		d.files[parts[len(parts)-1]] = p
		_ = sha
	}
	var write func(d *dir) (plumbing.Hash, error)
	write = func(d *dir) (plumbing.Hash, error) {
		t := &object.Tree{}
		for name, full := range d.files {
			m := modes[full]
			if m == 0 {
				m = filemode.Regular
			}
			t.Entries = append(t.Entries, object.TreeEntry{Name: name, Mode: m, Hash: plumbing.NewHash(files[full])})
		}
		for name, sub := range d.dirs {
			h, err := write(sub)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			t.Entries = append(t.Entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: h})
		}
		// git sorts entries by name, a folder as if it ended with /
		sort.Slice(t.Entries, func(i, j int) bool { return sortKey(t.Entries[i]) < sortKey(t.Entries[j]) })
		obj := st.NewEncodedObject()
		if err := t.Encode(obj); err != nil {
			return plumbing.ZeroHash, err
		}
		return st.SetEncodedObject(obj)
	}
	return write(root)
}

func sortKey(e object.TreeEntry) string {
	if e.Mode == filemode.Dir {
		return e.Name + "/"
	}
	return e.Name
}

// cursor is where Watch resumes: each branch's head.
type cursor struct {
	Heads map[string]string `json:"heads"`
}

// Watch polls the advertised branch heads; a moved or new branch is
// pushed, with the paths its move changed (from its last commit when the
// old head is not within reach). The first look only notes where they
// are.
func (s *Session) Watch(ctx adapter.Context, raw json.RawMessage, emit func(adapter.Event) error) error {
	var cur cursor
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cur)
	}
	def, _ := git.RepoContract().Event(git.EventPush)
	first := cur.Heads == nil
	for {
		adv, _, err := s.advertised(ctx)
		if err == nil {
			next := cursor{Heads: map[string]string{}}
			names := make([]string, 0, len(adv))
			for n, sha := range adv {
				if strings.HasPrefix(n, "refs/heads/") {
					next.Heads[n] = sha
					names = append(names, n)
				}
			}
			sort.Strings(names)
			var events []adapter.Event
			for _, n := range names {
				before, after := cur.Heads[n], next.Heads[n]
				if first || before == after {
					continue
				}
				p := git.Pushed{Ref: n, Before: before, After: after, Paths: s.pathsOf(ctx, n, before, after)}
				payload, _ := json.Marshal(p)
				events = append(events, adapter.Event{Contract: git.RepoRef, Name: git.EventPush, ID: connectors.EventID(def, git.RepoRef, payload), Payload: payload, At: time.Now()})
			}
			for i, e := range events {
				if i == len(events)-1 {
					e.Cursor, _ = json.Marshal(next)
				}
				if err := emit(e); err != nil {
					return err
				}
			}
			cur, first = next, false
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.poll):
		}
	}
}

func (s *Session) pathsOf(ctx context.Context, ref, before, after string) []string {
	f, err := s.fetch(ctx, []string{ref}, git.KeyDepth)
	if err != nil {
		return []string{}
	}
	a, err := f.repo.CommitObject(plumbing.NewHash(after))
	if err != nil {
		return []string{}
	}
	var b *object.Commit
	if before != "" {
		b, _ = f.repo.CommitObject(plumbing.NewHash(before))
	}
	if b == nil && len(a.ParentHashes) > 0 {
		b, _ = f.repo.CommitObject(a.ParentHashes[0])
	}
	af, err := s.files(a)
	if err != nil {
		return []string{}
	}
	bf := map[string]git.Entry{}
	if b != nil {
		if bf, err = s.files(b); err != nil {
			return []string{}
		}
	}
	return git.Paths(git.Diff(snapshot(bf), snapshot(af)))
}
