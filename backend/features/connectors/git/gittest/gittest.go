// Package gittest is the Git test server (forge 24a): a builtin connector
// type, gittest.repo, implementing git.repo@1 and git.review@1 over
// repositories it keeps in memory, one per connection, with its own
// contract gittest.control@1 to push from outside, inject faults, protect
// branches, merge pull requests and read the log. Every git part is tested
// against it without a host.
//
// A fresh connection's repository has one commit on its default branch
// (README.md). Blob shas are git's; commit shas are made the same way
// from the commit's tree, parents, author and message. Its state lives in
// the process that serves the type (a test server): give it one adapter
// instance.
package gittest

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The test server's type and control contract.
const (
	TypeID     = "gittest.repo"
	ControlID  = "gittest.control"
	ControlRef = "gittest.control@1"
)

const settings = `{"type":"object","properties":{
  "defaultBranch":{"type":"string","title":"Default branch","description":"The branch a fresh repository starts with; main when left out"},
  "allowDefaultBranch":{"type":"boolean","title":"Allow commits to the default branch","description":"Let the git tools and nodes commit straight to the default branch; otherwise they commit to another branch"}}}`

// Type is the test server's connector type.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{git.RepoRef, git.ReviewRef, ControlRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(settings),
		Services: []string{"files"},
		Meta:     connectors.Meta{Title: "Git test server", Description: "A pretend git host in memory: each connection is a repository with git.repo@1 and git.review@1 (branches, files, commits, pull requests), and gittest.control@1 pushes from outside, fails calls, protects branches and merges pull requests. For tests and trying the git connector by hand; nothing leaves the server.", Tags: []string{"git", "test"}},
	}
}

func j(s string) json.RawMessage { return json.RawMessage(s) }

var ok = j(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true"}},"required":["ok"]}`)

// ControlContract is gittest.control@1.
func ControlContract() connectors.Contract {
	return connectors.Contract{
		ID: ControlID, Version: 1,
		Meta: connectors.Meta{Title: "Git test server control", Description: "Drives the Git test server from outside: push a commit as someone else would, fail the next calls of an operation, protect a branch, merge a pull request, and read a repository's branches, log and pull requests. For tests only.", Tags: []string{"git", "test"}},
		Operations: []connectors.Operation{
			{
				ID: "push", Title: "Push from outside", Description: "Commits to a branch as someone outside Axiom would, raising git.repo's pushed event. Without changes it writes pushes/<n>.md.",
				Effect: connectors.EffectWrite,
				Args: j(`{"type":"object","properties":{
  "branch":{"type":"string","description":"The branch; the default branch when left out"},
  "message":{"type":"string","description":"The commit message"},
  "changes":{"type":"array","description":"The files to write or delete","items":{"type":"object","properties":{
    "path":{"type":"string","description":"The file's path"},
    "content":{"type":"string","description":"Its new content"},
    "delete":{"type":"boolean","description":"Delete it"}},"required":["path"]}}}}`),
				Result:   j(`{"type":"object","properties":{"sha":{"type":"string","description":"The new commit"},"branch":{"type":"string","description":"The branch"}},"required":["sha","branch"]}`),
				Examples: []connectors.Example{{Args: j(`{"branch":"main","message":"Fix a typo","changes":[{"path":"README.md","content":"# Site\n"}]}`), Result: j(`{"sha":"1f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c","branch":"main"}`)}},
				Errors:   []string{connectors.CodeInvalid},
			},
			{
				ID: "fail", Title: "Fail the next calls", Description: "Makes the next times calls of a git.repo or git.review operation fail with the error kind given (rateLimited and unavailable are retried by jobs).",
				Effect: connectors.EffectWrite,
				Args: j(`{"type":"object","properties":{
  "operation":{"type":"string","description":"The operation, such as commit or openPullRequest"},
  "times":{"type":"integer","minimum":1,"description":"How many calls fail; once when left out"},
  "kind":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"The error code; unavailable when left out"}},"required":["operation"]}`),
				Result: ok, Examples: []connectors.Example{{Args: j(`{"operation":"commit","times":2,"kind":"unavailable"}`), Result: j(`{"ok":true}`)}},
				Errors: []string{connectors.CodeInvalid},
			},
			{
				ID: "protect", Title: "Protect a branch", Description: "Refuses commits to a branch (invalid), as a host's branch protection does; protected false lifts it.",
				Effect: connectors.EffectWrite,
				Args: j(`{"type":"object","properties":{
  "branch":{"type":"string","description":"The branch"},
  "protected":{"type":"boolean","description":"False lifts the protection; true when left out"}},"required":["branch"]}`),
				Result: ok, Examples: []connectors.Example{{Args: j(`{"branch":"main"}`), Result: j(`{"ok":true}`)}},
			},
			{
				ID: "merge", Title: "Merge a pull request", Description: "Merges an open pull request as a reviewer would: its base takes the head's files in a merge commit, raising pushed and pullRequestChanged.",
				Effect:   connectors.EffectWrite,
				Args:     j(`{"type":"object","properties":{"number":{"type":"integer","description":"The pull request's number"}},"required":["number"]}`),
				Result:   j(`{"type":"object","properties":{"sha":{"type":"string","description":"The merge commit"}},"required":["sha"]}`),
				Examples: []connectors.Example{{Args: j(`{"number":1}`), Result: j(`{"sha":"1f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c"}`)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeConflict},
			},
			{
				ID: "log", Title: "Read the log", Description: "A repository's branches, the commits of one branch newest first, its pull requests and the faults still to happen.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{
  "branch":{"type":"string","description":"The branch whose commits to list; the default branch when left out"},
  "limit":{"type":"integer","minimum":1,"description":"The most commits; 50 when left out"}}}`),
				Result: j(`{"type":"object","properties":{
  "defaultBranch":{"type":"string","description":"The default branch"},
  "branches":{"type":"array","description":"Every branch","items":{"type":"object","properties":{
    "name":{"type":"string","description":"Its name"},
    "sha":{"type":"string","description":"Its head"},
    "protected":{"type":"boolean","description":"True when commits are refused"}},"required":["name","sha"]}},
  "commits":{"type":"array","description":"The branch's commits, newest first","items":{"type":"object","properties":{
    "sha":{"type":"string","description":"The commit"},
    "parents":{"type":"array","items":{"type":"string"},"description":"Its parents"},
    "message":{"type":"string","description":"Its message"},
    "author":{"type":"string","description":"Who it is by"},
    "time":{"type":"string","description":"When, RFC 3339"},
    "paths":{"type":"array","items":{"type":"string"},"description":"The paths it changed"}},"required":["sha","message"]}},
  "pullRequests":{"type":"array","description":"Every pull request, newest first","items":{"type":"object","properties":{
    "number":{"type":"integer","description":"Its number"},
    "state":{"type":"string","description":"open, closed or merged"},
    "title":{"type":"string","description":"Its title"},
    "head":{"type":"string","description":"From"},
    "base":{"type":"string","description":"Into"}},"required":["number","state"]}},
  "faults":{"type":"array","description":"Faults still to happen","items":{"type":"object","properties":{
    "operation":{"type":"string","description":"The operation"},
    "times":{"type":"integer","description":"Calls left to fail"},
    "kind":{"type":"string","description":"The error code"}},"required":["operation"]}}},"required":["defaultBranch","branches","commits"]}`),
				Examples: []connectors.Example{{Args: j(`{"branch":"main","limit":1}`), Result: j(`{"defaultBranch":"main","branches":[{"name":"main","sha":"1f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c"}],"commits":[{"sha":"1f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c","parents":[],"message":"Initial commit","author":"Git test server","time":"2026-11-27T12:00:00Z","paths":["README.md"]}],"pullRequests":[],"faults":[]}`)}},
			},
		},
	}
}

// Contribute registers the test server's type and control contract.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, TypeID, []connectors.Contract{ControlContract()}, []connectors.Type{Type()})
}

// Server holds every connection's repository in this process.
type Server struct {
	mu    sync.Mutex
	repos map[string]*repo
	now   func() time.Time
	files git.Files
}

// New makes an empty server; files, when not nil, is the tenant's file
// store (readFile's large files, commit's file changes).
func New(files git.Files) *Server {
	return &Server{repos: map[string]*repo{}, now: time.Now, files: files}
}

// Default is this process's server, without a file store until
// SetFiles.
var Default = New(nil)

// SetFiles gives the server the tenant's file store.
func (s *Server) SetFiles(files git.Files) {
	s.mu.Lock()
	s.files = files
	s.mu.Unlock()
}

// SetNow replaces the clock, for tests.
func (s *Server) SetNow(now func() time.Time) {
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

type commit struct {
	SHA     string
	Parents []string
	Tree    map[string]string // path → blob sha
	Message string
	Author  git.Author
	Time    time.Time
}

type fault struct {
	times int
	kind  string
}

type event struct {
	seq     int
	name    string
	payload json.RawMessage
}

type pr struct{ git.PullRequest }

type repo struct {
	def       string
	blobs     map[string][]byte
	commits   map[string]*commit
	refs      map[string]string // branch → sha
	protected map[string]bool
	faults    map[string]*fault
	prs       []*pr
	events    []event
	wake      chan struct{}
	pushes    int
}

func blobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (r *repo) put(data []byte) string {
	sha := blobSHA(data)
	if _, ok := r.blobs[sha]; !ok {
		r.blobs[sha] = append([]byte(nil), data...)
	}
	return sha
}

// make stores a commit made from its fields and gives its sha.
func (r *repo) make(c *commit) string {
	paths := make([]string, 0, len(c.Tree))
	for p := range c.Tree {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha1.New()
	fmt.Fprintf(h, "commit\x00")
	for _, p := range paths {
		fmt.Fprintf(h, "%s %s\n", c.Tree[p], p)
	}
	fmt.Fprintf(h, "parents %s\nauthor %s <%s> %d\n\n%s", strings.Join(c.Parents, " "), c.Author.Name, c.Author.Email, c.Time.UnixNano(), c.Message)
	c.SHA = hex.EncodeToString(h.Sum(nil))
	r.commits[c.SHA] = c
	return c.SHA
}

func (s *Server) repo(tenant uint64, conn string) *repo {
	k := strconv.FormatUint(tenant, 10) + "." + conn
	r := s.repos[k]
	if r == nil {
		r = &repo{def: "main", blobs: map[string][]byte{}, commits: map[string]*commit{}, refs: map[string]string{},
			protected: map[string]bool{}, faults: map[string]*fault{}, wake: make(chan struct{})}
		s.repos[k] = r
	}
	return r
}

// seed gives a fresh repository its first commit; under s.mu.
func (s *Server) seed(r *repo) {
	if len(r.refs) > 0 {
		return
	}
	c := &commit{Tree: map[string]string{"README.md": r.put([]byte("# Test repository\n\nMade by the Git test server.\n"))},
		Message: "Initial commit", Author: git.Author{Name: "Git test server", Email: "gittest@example.invalid"}, Time: s.now().UTC()}
	r.refs[r.def] = r.make(c)
}

// emit logs an event and wakes the watches; under s.mu.
func (r *repo) emit(name string, payload any) {
	raw, _ := json.Marshal(payload)
	r.events = append(r.events, event{seq: len(r.events) + 1, name: name, payload: raw})
	close(r.wake)
	r.wake = make(chan struct{})
}

// move points a branch at a commit, raising pushed; under s.mu.
func (r *repo) move(branch, sha string) {
	before := r.refs[branch]
	r.refs[branch] = sha
	var base map[string]string
	if before != "" {
		base = r.commits[before].Tree
	}
	r.emit(git.EventPush, git.Pushed{Ref: "refs/heads/" + branch, Before: before, After: sha, Paths: git.Paths(git.Diff(base, r.commits[sha].Tree))})
	for _, p := range r.prs {
		if p.Head == branch && p.State == "open" {
			p.HeadSHA = sha
			p.UpdatedAt = r.commits[sha].Time.Format(time.RFC3339Nano)
			r.emit(git.EventPR, p.PullRequest)
		}
	}
}

// resolve finds the commit a ref names: a branch, refs/heads/<branch>, or
// a commit sha (or a unique prefix of at least 7).
func (r *repo) resolve(ref string) (*commit, error) {
	if ref == "" || ref == "HEAD" {
		ref = r.def
	}
	b := strings.TrimPrefix(ref, "refs/heads/")
	if sha, ok := r.refs[b]; ok {
		return r.commits[sha], nil
	}
	if c, ok := r.commits[ref]; ok {
		return c, nil
	}
	if len(ref) >= 7 && !strings.HasPrefix(ref, "refs/") {
		var found *commit
		for sha, c := range r.commits {
			if strings.HasPrefix(sha, ref) {
				if found != nil {
					return nil, connectors.Errorf(connectors.CodeInvalid, "%s names more than one commit", ref)
				}
				found = c
			}
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, connectors.Errorf(connectors.CodeNotFound, "there is no ref or commit %q", ref)
}

// Impl is the adapter implementation of the test server's type.
func (s *Server) Impl() adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st struct {
			DefaultBranch      string `json:"defaultBranch"`
			AllowDefaultBranch bool   `json:"allowDefaultBranch"`
		}
		_ = json.Unmarshal(conn.Settings, &st)
		s.mu.Lock()
		r := s.repo(ctx.Tenant(), conn.ID)
		if st.DefaultBranch != "" && len(r.refs) == 0 {
			if b, err := git.BranchName(st.DefaultBranch); err == nil {
				r.def = b
			}
		}
		s.seed(r)
		s.mu.Unlock()
		return &session{s: s, tenant: ctx.Tenant(), conn: conn.ID, allowDefault: st.AllowDefaultBranch}, nil
	}}
}

// Options are adapter options serving only the test server, for tests.
func (s *Server) Options(name string) adapter.Options {
	return adapter.Options{Name: name, Types: []connectors.Type{Type()}, Contracts: append(git.Contracts(), ControlContract()), Impls: []adapter.Impl{s.Impl()}}
}

type session struct {
	s      *Server
	tenant uint64
	conn   string
	// allowDefault is the connection's allowDefaultBranch, given by check
	allowDefault bool
}

func (x *session) Close() error { return nil }

// Watch reports pushed and pullRequestChanged in order, resuming after
// the cursor (the last event's number).
func (x *session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	from := 0
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &from)
	}
	repoDef, _ := git.RepoContract().Event(git.EventPush)
	prDef, _ := git.ReviewContract().Event(git.EventPR)
	for {
		x.s.mu.Lock()
		r := x.s.repo(x.tenant, x.conn)
		x.s.seed(r)
		pending := append([]event(nil), r.events[min(from, len(r.events)):]...)
		wake := r.wake
		now := x.s.now()
		x.s.mu.Unlock()
		for _, e := range pending {
			contract, def := git.RepoRef, repoDef
			if e.name == git.EventPR {
				contract, def = git.ReviewRef, prDef
			}
			c, _ := json.Marshal(e.seq)
			if err := emit(adapter.Event{Contract: contract, Name: e.name, ID: connectors.EventID(def, contract, e.payload), Payload: e.payload, At: now, Cursor: c}); err != nil {
				return err
			}
			from = e.seq
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (x *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	if ctx.Contract == ControlRef {
		return x.control(ctx, op, args)
	}
	x.s.mu.Lock()
	r := x.s.repo(x.tenant, x.conn)
	x.s.seed(r)
	if f := r.faults[op]; f != nil {
		f.times--
		kind := f.kind
		if f.times <= 0 {
			delete(r.faults, op)
		}
		x.s.mu.Unlock()
		return nil, connectors.Errorf(kind, "the test server failed %s as asked", op)
	}
	files := x.s.files
	x.s.mu.Unlock()

	switch op {
	case "commit":
		// Read the file changes outside the lock: the file store is NATS
		var a git.CommitArgs
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		writes, err := git.Decode(ctx, files, &a)
		if err != nil {
			return nil, err
		}
		x.s.mu.Lock()
		defer x.s.mu.Unlock()
		res, err := x.commit(r, a, writes)
		if err != nil {
			return nil, err
		}
		_ = ctx.Accepted(mustJSON(res))
		return git.Result(res)
	case "readFile":
		var a struct{ Ref, Path string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		p, err := git.CleanPath(a.Path)
		if err != nil {
			return nil, err
		}
		x.s.mu.Lock()
		c, err := r.resolve(a.Ref)
		var data []byte
		var sha string
		if err == nil {
			sha = c.Tree[p]
			data = r.blobs[sha]
		}
		x.s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if sha == "" {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no file %s at %s", p, a.Ref)
		}
		f, err := git.FileResult(ctx, files, p, sha, data)
		if err != nil {
			return nil, err
		}
		return git.Result(f)
	}

	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	switch op {
	case "check":
		return git.Result(map[string]any{"ok": true, "defaultBranch": r.def, "canWrite": true, "allowDefaultBranch": x.allowDefault})
	case "listRefs":
		var a struct{ Prefix string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		refs := []git.Ref{}
		for b, sha := range r.refs {
			if name := "refs/heads/" + b; strings.HasPrefix(name, a.Prefix) {
				refs = append(refs, git.Ref{Name: name, SHA: sha})
			}
		}
		sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
		return git.Result(map[string]any{"refs": refs, "defaultBranch": r.def})
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
		c, err := r.resolve(a.Ref)
		if err != nil {
			return nil, err
		}
		files := make(map[string]git.Entry, len(c.Tree))
		for p, sha := range c.Tree {
			files[p] = git.Entry{Path: p, Kind: "file", Size: int64(len(r.blobs[sha])), SHA: sha}
		}
		page, next, err := git.List(files, dir, a.Recursive, a.Cursor, a.Limit)
		if err != nil {
			return nil, err
		}
		return git.Result(git.Tree{SHA: c.SHA, Entries: page, Next: next})
	case "compare":
		var a struct{ Base, Head string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		b, err := r.resolve(a.Base)
		if err != nil {
			return nil, err
		}
		h, err := r.resolve(a.Head)
		if err != nil {
			return nil, err
		}
		return git.Result(git.Comparison{Base: b.SHA, Head: h.SHA, Changes: git.Diff(b.Tree, h.Tree)})
	case "openPullRequest":
		var a struct {
			Head, Base, Title, Body, Key string
			Draft                        bool
		}
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Base == "" {
			a.Base = r.def
		}
		if strings.TrimSpace(a.Title) == "" {
			return nil, connectors.Errorf(connectors.CodeInvalid, "a pull request needs a title")
		}
		for _, p := range r.prs {
			if p.Head == a.Head && p.Base == a.Base && p.State == "open" {
				return git.Result(p.PullRequest)
			}
		}
		head, okH := r.refs[a.Head]
		if !okH {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no branch %s", a.Head)
		}
		if _, ok := r.refs[a.Base]; !ok {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no branch %s", a.Base)
		}
		if a.Head == a.Base {
			return nil, connectors.Errorf(connectors.CodeInvalid, "a pull request needs two branches")
		}
		n := len(r.prs) + 1
		p := &pr{git.PullRequest{Number: n, URL: fmt.Sprintf("gittest://%s/pull/%d", x.conn, n), State: "open", Draft: a.Draft, Title: a.Title, Body: a.Body,
			Head: a.Head, Base: a.Base, HeadSHA: head, UpdatedAt: x.s.now().UTC().Format(time.RFC3339Nano)}}
		r.prs = append(r.prs, p)
		r.emit(git.EventPR, p.PullRequest)
		res := mustJSON(p.PullRequest)
		_ = ctx.Accepted(res)
		return res, nil
	case "getPullRequest":
		var a struct{ Number int }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Number < 1 || a.Number > len(r.prs) {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no pull request %d", a.Number)
		}
		return git.Result(r.prs[a.Number-1].PullRequest)
	case "listPullRequests":
		var a struct{ State, Head, Base string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.State == "" {
			a.State = "open"
		}
		out := []git.PullRequest{}
		for i := len(r.prs) - 1; i >= 0; i-- {
			p := r.prs[i]
			if (a.State == "all" || p.State == a.State) && (a.Head == "" || p.Head == a.Head) && (a.Base == "" || p.Base == a.Base) {
				out = append(out, p.PullRequest)
			}
		}
		return git.Result(map[string]any{"pullRequests": out})
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// commit makes one commit on a branch; under s.mu.
func (x *session) commit(r *repo, a git.CommitArgs, writes []git.Write) (git.CommitResult, error) {
	if r.protected[a.Branch] {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeInvalid, "branch %s is protected", a.Branch)
	}
	head, exists := r.refs[a.Branch]
	// A key already on the branch: this is a retry
	if a.Key != "" && exists {
		sha := head
		for i := 0; i < git.KeyDepth && sha != ""; i++ {
			c := r.commits[sha]
			if git.HasKey(c.Message, a.Key) {
				return git.CommitResult{SHA: c.SHA, Branch: a.Branch, Before: first(c.Parents)}, nil
			}
			sha = first(c.Parents)
		}
	}
	if a.ExpectHead != "" && a.ExpectHead != head {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeConflict, "branch %s is at %s, not %s", a.Branch, short(head), short(a.ExpectHead))
	}
	var parent *commit
	if exists {
		parent = r.commits[head]
	} else {
		base, err := r.resolve(a.Base)
		if err != nil {
			return git.CommitResult{}, err
		}
		parent = base
	}
	tree := make(map[string]string, len(parent.Tree))
	for p, sha := range parent.Tree {
		tree[p] = sha
	}
	for _, w := range writes {
		if w.Deleted {
			delete(tree, w.Path)
			continue
		}
		for p := range tree { // a file cannot also be a folder
			if strings.HasPrefix(p, w.Path+"/") || strings.HasPrefix(w.Path, p+"/") {
				return git.CommitResult{}, connectors.Errorf(connectors.CodeInvalid, "%s and %s cannot both exist", w.Path, p)
			}
		}
		tree[w.Path] = r.put(w.Data)
	}
	if exists && len(git.Diff(parent.Tree, tree)) == 0 {
		return git.CommitResult{SHA: head, Branch: a.Branch, Before: head, Unchanged: true}, nil
	}
	au := git.Author{Name: "Axiom", Email: "axiom@example.invalid"}
	if a.Author != nil && a.Author.Name != "" {
		au = *a.Author
	}
	c := &commit{Parents: []string{parent.SHA}, Tree: tree, Message: git.WithKey(a.Message, a.Key), Author: au, Time: x.s.now().UTC()}
	sha := r.make(c)
	r.move(a.Branch, sha)
	return git.CommitResult{SHA: sha, Branch: a.Branch, Before: head, Created: !exists}, nil
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func short(sha string) string {
	if sha == "" {
		return "nothing (no such branch)"
	}
	return sha[:min(len(sha), 7)]
}

func (x *session) control(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	r := x.s.repo(x.tenant, x.conn)
	x.s.seed(r)
	switch op {
	case "push":
		var a struct {
			Branch, Message string
			Changes         []struct {
				Path    string
				Content string
				Delete  bool
			}
		}
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Branch == "" {
			a.Branch = r.def
		}
		b, err := git.BranchName(a.Branch)
		if err != nil {
			return nil, err
		}
		r.pushes++
		if len(a.Changes) == 0 {
			a.Changes = append(a.Changes, struct {
				Path    string
				Content string
				Delete  bool
			}{Path: fmt.Sprintf("pushes/%d.md", r.pushes), Content: fmt.Sprintf("# Push %d\n", r.pushes)})
		}
		if a.Message == "" {
			a.Message = fmt.Sprintf("Outside push %d", r.pushes)
		}
		head := r.refs[b]
		parent := r.commits[head]
		if parent == nil {
			parent = r.commits[r.refs[r.def]]
		}
		tree := map[string]string{}
		for p, sha := range parent.Tree {
			tree[p] = sha
		}
		for _, c := range a.Changes {
			p, err := git.CleanPath(c.Path)
			if err != nil {
				return nil, err
			}
			if c.Delete {
				delete(tree, p)
			} else {
				tree[p] = r.put([]byte(c.Content))
			}
		}
		c := &commit{Parents: []string{parent.SHA}, Tree: tree, Message: a.Message, Author: git.Author{Name: "Someone outside", Email: "outside@example.invalid"}, Time: x.s.now().UTC()}
		sha := r.make(c)
		r.move(b, sha)
		return git.Result(map[string]any{"sha": sha, "branch": b})
	case "fail":
		var a struct {
			Operation, Kind string
			Times           int
		}
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		_, inRepo := git.RepoContract().Operation(a.Operation)
		_, inReview := git.ReviewContract().Operation(a.Operation)
		if !inRepo && !inReview {
			return nil, connectors.Errorf(connectors.CodeInvalid, "git.repo and git.review have no operation %q", a.Operation)
		}
		if a.Kind == "" {
			a.Kind = connectors.CodeUnavailable
		}
		r.faults[a.Operation] = &fault{times: max(a.Times, 1), kind: a.Kind}
	case "protect":
		var a struct {
			Branch    string
			Protected *bool
		}
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Protected == nil || *a.Protected {
			r.protected[a.Branch] = true
		} else {
			delete(r.protected, a.Branch)
		}
	case "merge":
		var a struct{ Number int }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Number < 1 || a.Number > len(r.prs) {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no pull request %d", a.Number)
		}
		p := r.prs[a.Number-1]
		if p.State != "open" {
			return nil, connectors.Errorf(connectors.CodeConflict, "pull request %d is %s", a.Number, p.State)
		}
		head, base := r.commits[r.refs[p.Head]], r.commits[r.refs[p.Base]]
		tree := map[string]string{}
		for path, sha := range base.Tree {
			tree[path] = sha
		}
		// The head's changes since it left the base win; the test server
		// has no real merge, only what tests need
		fork := mergeBase(r, head, base)
		for _, ch := range git.Diff(fork, head.Tree) {
			if ch.Status == git.Deleted {
				delete(tree, ch.Path)
			} else {
				tree[ch.Path] = head.Tree[ch.Path]
			}
		}
		c := &commit{Parents: []string{base.SHA, head.SHA}, Tree: tree, Message: fmt.Sprintf("Merge pull request #%d from %s\n\n%s", p.Number, p.Head, p.Title),
			Author: git.Author{Name: "A reviewer", Email: "reviewer@example.invalid"}, Time: x.s.now().UTC()}
		sha := r.make(c)
		p.State = "merged"
		p.UpdatedAt = c.Time.Format(time.RFC3339Nano)
		r.emit(git.EventPR, p.PullRequest)
		r.move(p.Base, sha)
		return git.Result(map[string]any{"sha": sha})
	case "log":
		var a struct {
			Branch string
			Limit  int
		}
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		if a.Branch == "" {
			a.Branch = r.def
		}
		if a.Limit <= 0 {
			a.Limit = 50
		}
		branches := []map[string]any{}
		names := make([]string, 0, len(r.refs))
		for b := range r.refs {
			names = append(names, b)
		}
		sort.Strings(names)
		for _, b := range names {
			branches = append(branches, map[string]any{"name": b, "sha": r.refs[b], "protected": r.protected[b]})
		}
		commits := []map[string]any{}
		for sha := r.refs[a.Branch]; sha != "" && len(commits) < a.Limit; {
			c := r.commits[sha]
			var parent map[string]string
			if len(c.Parents) > 0 {
				parent = r.commits[c.Parents[0]].Tree
			}
			commits = append(commits, map[string]any{"sha": c.SHA, "parents": append([]string{}, c.Parents...), "message": c.Message,
				"author": c.Author.Name, "time": c.Time.Format(time.RFC3339), "paths": git.Paths(git.Diff(parent, c.Tree))})
			sha = first(c.Parents)
		}
		prs := []map[string]any{}
		for i := len(r.prs) - 1; i >= 0; i-- {
			p := r.prs[i]
			prs = append(prs, map[string]any{"number": p.Number, "state": p.State, "title": p.Title, "head": p.Head, "base": p.Base})
		}
		faults := []map[string]any{}
		ops := make([]string, 0, len(r.faults))
		for o := range r.faults {
			ops = append(ops, o)
		}
		sort.Strings(ops)
		for _, o := range ops {
			faults = append(faults, map[string]any{"operation": o, "times": r.faults[o].times, "kind": r.faults[o].kind})
		}
		return git.Result(map[string]any{"defaultBranch": r.def, "branches": branches, "commits": commits, "pullRequests": prs, "faults": faults})
	default:
		return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
	}
	return json.RawMessage(`{"ok":true}`), nil
}

// mergeBase is the newest commit on the base's first-parent line that the
// head's first-parent line also holds: the tree the head forked from.
func mergeBase(r *repo, head, base *commit) map[string]string {
	onBase := map[string]bool{}
	for sha := base.SHA; sha != ""; sha = first(r.commits[sha].Parents) {
		onBase[sha] = true
	}
	for sha := head.SHA; sha != ""; sha = first(r.commits[sha].Parents) {
		if onBase[sha] {
			return r.commits[sha].Tree
		}
	}
	return map[string]string{}
}
