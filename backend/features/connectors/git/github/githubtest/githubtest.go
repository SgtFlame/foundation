// Package githubtest is a stand-in for GitHub's REST API (forge 24b): the
// endpoints git.github uses, over one repository in memory, served by an
// httptest server, so the type passes the git contract suite with every
// request and answer going over HTTP. Its answers follow GitHub's REST
// reference (2022-11-28); recordings against a real repository (question
// 3 of the plan) check it.
package githubtest

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type commit struct {
	sha     string
	tree    string
	parents []string
	message string
	author  string
	time    time.Time
}

type pull struct {
	Number   int
	State    string
	Draft    bool
	Title    string
	Body     string
	Head     string
	Base     string
	MergedAt string
	Updated  time.Time
}

type fault struct {
	method, prefix string
	status, times  int
}

// Server is the fake GitHub.
type Server struct {
	Owner, Repo, Token string
	*httptest.Server

	mu        sync.Mutex
	blobs     map[string][]byte
	trees     map[string]map[string]string // tree sha → path → blob sha
	commits   map[string]*commit
	refs      map[string]string // branch → sha
	protected map[string]bool
	pulls     []*pull
	faults    []*fault
	clock     time.Time
	// Requests counts the requests served, by "METHOD path".
	Requests map[string]int
}

// Start starts a fake with one commit on main (README.md).
func Start(owner, repo, token string) *Server {
	s := &Server{Owner: owner, Repo: repo, Token: token, blobs: map[string][]byte{}, trees: map[string]map[string]string{}, commits: map[string]*commit{},
		refs: map[string]string{}, protected: map[string]bool{}, clock: time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC), Requests: map[string]int{}}
	tree := s.putTree(map[string]string{"README.md": s.putBlob([]byte("# Site\n"))})
	s.refs["main"] = s.putCommit(tree, nil, "Initial commit", "Tony")
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *Server) now() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

func (s *Server) putBlob(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	sha := hex.EncodeToString(h.Sum(nil))
	s.blobs[sha] = append([]byte(nil), data...)
	return sha
}

func (s *Server) putTree(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha1.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s %s\n", files[p], p)
	}
	sha := hex.EncodeToString(h.Sum(nil))
	s.trees[sha] = files
	return sha
}

func (s *Server) putCommit(tree string, parents []string, message, author string) string {
	c := &commit{tree: tree, parents: parents, message: message, author: author, time: s.now()}
	h := sha1.New()
	fmt.Fprintf(h, "tree %s\nparents %s\n%s\n%d\n%s", tree, strings.Join(parents, " "), author, c.time.UnixNano(), message)
	c.sha = hex.EncodeToString(h.Sum(nil))
	s.commits[c.sha] = c
	return c.sha
}

// Push commits path = content to a branch as someone outside.
func (s *Server) Push(branch, path, content string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	parent := s.refs[branch]
	if parent == "" {
		parent = s.refs["main"]
	}
	files := copyMap(s.trees[s.commits[parent].tree])
	files[path] = s.putBlob([]byte(content))
	sha := s.putCommit(s.putTree(files), []string{parent}, "Outside push", "Someone")
	s.refs[branch] = sha
	return sha
}

// Protect refuses updates of a branch.
func (s *Server) Protect(branch string) {
	s.mu.Lock()
	s.protected[branch] = true
	s.mu.Unlock()
}

// Fail makes the next times requests whose method and path (after
// /repos/<owner>/<repo>) start as given answer status.
func (s *Server) Fail(method, prefix string, status, times int) {
	s.mu.Lock()
	s.faults = append(s.faults, &fault{method: method, prefix: prefix, status: status, times: times})
	s.mu.Unlock()
}

// Merge merges an open pull request: the base takes the head's tree.
func (s *Server) Merge(number int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pulls[number-1]
	head := s.commits[s.refs[p.Head]]
	s.refs[p.Base] = s.putCommit(head.tree, []string{s.refs[p.Base], head.sha}, "Merge pull request #"+strconv.Itoa(number), "Reviewer")
	p.State, p.MergedAt, p.Updated = "closed", s.now().Format(time.RFC3339), s.clock
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]any{"message": msg, "documentation_url": "https://docs.github.com/rest"})
}

func (s *Server) resolve(ref string) *commit {
	if sha, ok := s.refs[strings.TrimPrefix(ref, "refs/heads/")]; ok {
		return s.commits[sha]
	}
	return s.commits[ref]
}

func (s *Server) commitJSON(c *commit) map[string]any {
	parents := []map[string]string{}
	for _, p := range c.parents {
		parents = append(parents, map[string]string{"sha": p})
	}
	return map[string]any{"sha": c.sha, "html_url": "https://github.com/" + s.Owner + "/" + s.Repo + "/commit/" + c.sha, "parents": parents,
		"commit": map[string]any{"message": c.message, "tree": map[string]string{"sha": c.tree}, "author": map[string]any{"name": c.author, "date": c.time.Format(time.RFC3339)}}}
}

func (s *Server) pullJSON(p *pull) map[string]any {
	var merged any
	if p.MergedAt != "" {
		merged = p.MergedAt
	}
	return map[string]any{"number": p.Number, "html_url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", s.Owner, s.Repo, p.Number), "state": p.State, "draft": p.Draft,
		"title": p.Title, "body": p.Body, "merged_at": merged, "updated_at": p.Updated.Format(time.RFC3339),
		"head": map[string]string{"ref": p.Head, "sha": s.refs[p.Head]}, "base": map[string]string{"ref": p.Base}}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+s.Token {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return
	}
	root := "/repos/" + s.Owner + "/" + s.Repo
	if !strings.HasPrefix(r.URL.Path, root) {
		fail(w, http.StatusNotFound, "Not Found")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, root)
	s.Requests[r.Method+" "+path]++
	for i, f := range s.faults {
		if f.method == r.Method && strings.HasPrefix(path, f.prefix) {
			if f.times--; f.times <= 0 {
				s.faults = append(s.faults[:i], s.faults[i+1:]...)
			}
			if f.status == http.StatusForbidden || f.status == http.StatusTooManyRequests {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "7")
				fail(w, f.status, "API rate limit exceeded")
				return
			}
			fail(w, f.status, http.StatusText(f.status))
			return
		}
	}
	var body map[string]json.RawMessage
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(body[k], &v)
		return v
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && path == "":
		write(w, 200, map[string]any{"default_branch": "main", "html_url": "https://github.com/" + s.Owner + "/" + s.Repo, "permissions": map[string]bool{"push": true, "pull": true}})
	case r.Method == http.MethodGet && path == "/commits":
		var out []map[string]any
		n, _ := strconv.Atoi(q.Get("per_page"))
		for c := s.resolve(q.Get("sha")); c != nil && len(out) < max(n, 1); {
			out = append(out, s.commitJSON(c))
			if len(c.parents) == 0 {
				break
			}
			c = s.commits[c.parents[0]]
		}
		write(w, 200, out)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/commits/"):
		c := s.resolve(strings.TrimPrefix(path, "/commits/"))
		if c == nil {
			fail(w, 422, "No commit found for SHA: "+strings.TrimPrefix(path, "/commits/"))
			return
		}
		write(w, 200, s.commitJSON(c))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/git/trees/"):
		files, ok := s.trees[strings.TrimPrefix(path, "/git/trees/")]
		if !ok {
			fail(w, 404, "Not Found")
			return
		}
		entries := []map[string]any{}
		dirs := map[string]bool{}
		for p, sha := range files {
			entries = append(entries, map[string]any{"path": p, "mode": "100644", "type": "blob", "sha": sha, "size": len(s.blobs[sha])})
			for d := p; strings.Contains(d, "/"); {
				d = d[:strings.LastIndex(d, "/")]
				if !dirs[d] {
					dirs[d] = true
					entries = append(entries, map[string]any{"path": d, "mode": "040000", "type": "tree", "sha": "0000000000000000000000000000000000000000"})
				}
			}
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i]["path"].(string) < entries[j]["path"].(string) })
		write(w, 200, map[string]any{"sha": strings.TrimPrefix(path, "/git/trees/"), "tree": entries, "truncated": false})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/contents/"):
		ref := q.Get("ref")
		if ref == "" {
			ref = "main"
		}
		c := s.resolve(ref)
		p := strings.TrimPrefix(path, "/contents/")
		if c == nil {
			fail(w, 404, "No commit found for the ref "+ref)
			return
		}
		files := s.trees[c.tree]
		sha, ok := files[p]
		if !ok {
			for f := range files {
				if strings.HasPrefix(f, p+"/") {
					write(w, 200, []map[string]any{{"type": "file", "path": f}})
					return
				}
			}
			fail(w, 404, "Not Found")
			return
		}
		data := s.blobs[sha]
		out := map[string]any{"type": "file", "path": p, "sha": sha, "size": len(data), "encoding": "base64", "content": wrap(base64.StdEncoding.EncodeToString(data))}
		if len(data) > 1<<20 { // GitHub gives no content over 1 MB
			out["encoding"], out["content"] = "none", ""
		}
		write(w, 200, out)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/git/blobs/"):
		data, ok := s.blobs[strings.TrimPrefix(path, "/git/blobs/")]
		if !ok {
			fail(w, 404, "Not Found")
			return
		}
		write(w, 200, map[string]any{"content": wrap(base64.StdEncoding.EncodeToString(data)), "encoding": "base64", "size": len(data)})
	case r.Method == http.MethodPost && path == "/git/blobs":
		data, err := base64.StdEncoding.DecodeString(str("content"))
		if err != nil || str("encoding") != "base64" {
			fail(w, 422, "Invalid request")
			return
		}
		write(w, 201, map[string]string{"sha": s.putBlob(data)})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/git/matching-refs/"):
		prefix := "refs/" + strings.TrimPrefix(path, "/git/matching-refs/")
		names := make([]string, 0, len(s.refs))
		for b := range s.refs {
			names = append(names, b)
		}
		sort.Strings(names)
		out := []map[string]any{}
		page, _ := strconv.Atoi(q.Get("page"))
		if page > 1 {
			write(w, 200, out)
			return
		}
		for _, b := range names {
			if ref := "refs/heads/" + b; strings.HasPrefix(ref, prefix) {
				out = append(out, map[string]any{"ref": ref, "object": map[string]string{"sha": s.refs[b], "type": "commit"}})
			}
		}
		write(w, 200, out)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/git/ref/heads/"):
		sha, ok := s.refs[strings.TrimPrefix(path, "/git/ref/heads/")]
		if !ok {
			fail(w, 404, "Not Found")
			return
		}
		write(w, 200, map[string]any{"ref": "refs/heads/" + strings.TrimPrefix(path, "/git/ref/heads/"), "object": map[string]string{"sha": sha, "type": "commit"}})
	case r.Method == http.MethodPost && path == "/git/trees":
		files := map[string]string{}
		if base := str("base_tree"); base != "" {
			bt, ok := s.trees[base]
			if !ok {
				fail(w, 422, "Invalid tree info")
				return
			}
			files = copyMap(bt)
		}
		var entries []struct {
			Path    string  `json:"path"`
			SHA     *string `json:"sha"`
			Content *string `json:"content"`
		}
		_ = json.Unmarshal(body["tree"], &entries)
		for _, e := range entries {
			switch {
			case e.Content != nil:
				files[e.Path] = s.putBlob([]byte(*e.Content))
			case e.SHA == nil:
				delete(files, e.Path)
			default:
				files[e.Path] = *e.SHA
			}
		}
		write(w, 201, map[string]string{"sha": s.putTree(files)})
	case r.Method == http.MethodPost && path == "/git/commits":
		var parents []string
		_ = json.Unmarshal(body["parents"], &parents)
		if _, ok := s.trees[str("tree")]; !ok {
			fail(w, 422, "Tree SHA does not exist")
			return
		}
		write(w, 201, map[string]string{"sha": s.putCommit(str("tree"), parents, str("message"), "Axiom")})
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/git/refs/heads/"):
		branch := strings.TrimPrefix(path, "/git/refs/heads/")
		head, ok := s.refs[branch]
		if !ok {
			fail(w, 422, "Reference does not exist")
			return
		}
		if s.protected[branch] {
			fail(w, 422, "Protected branch update failed for refs/heads/"+branch)
			return
		}
		c := s.commits[str("sha")]
		if c == nil || len(c.parents) == 0 || c.parents[0] != head {
			fail(w, 422, "Update is not a fast forward")
			return
		}
		s.refs[branch] = c.sha
		write(w, 200, map[string]any{"ref": "refs/heads/" + branch, "object": map[string]string{"sha": c.sha}})
	case r.Method == http.MethodPost && path == "/git/refs":
		branch := strings.TrimPrefix(str("ref"), "refs/heads/")
		if _, ok := s.refs[branch]; ok {
			fail(w, 422, "Reference already exists")
			return
		}
		s.refs[branch] = str("sha")
		write(w, 201, map[string]any{"ref": str("ref"), "object": map[string]string{"sha": str("sha")}})
	case r.Method == http.MethodGet && path == "/pulls":
		out := []map[string]any{}
		ps := append([]*pull(nil), s.pulls...)
		if q.Get("sort") == "updated" {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].Updated.After(ps[j].Updated) })
		} else {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].Number > ps[j].Number })
		}
		for _, p := range ps {
			if st := q.Get("state"); st != "all" && st != p.State {
				continue
			}
			if h := q.Get("head"); h != "" && h != s.Owner+":"+p.Head {
				continue
			}
			if b := q.Get("base"); b != "" && b != p.Base {
				continue
			}
			out = append(out, s.pullJSON(p))
		}
		write(w, 200, out)
	case r.Method == http.MethodPost && path == "/pulls":
		if _, ok := s.refs[str("head")]; !ok {
			fail(w, 422, "Validation Failed: head is invalid")
			return
		}
		var draft bool
		_ = json.Unmarshal(body["draft"], &draft)
		p := &pull{Number: len(s.pulls) + 1, State: "open", Draft: draft, Title: str("title"), Body: str("body"), Head: str("head"), Base: str("base"), Updated: s.now()}
		s.pulls = append(s.pulls, p)
		write(w, 201, s.pullJSON(p))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/pulls/"):
		n, _ := strconv.Atoi(strings.TrimPrefix(path, "/pulls/"))
		if n < 1 || n > len(s.pulls) {
			fail(w, 404, "Not Found")
			return
		}
		write(w, 200, s.pullJSON(s.pulls[n-1]))
	default:
		fail(w, 404, "Not Found")
	}
}

// wrap breaks base64 into 60-character lines, as GitHub sends it.
func wrap(s string) string {
	var b strings.Builder
	for len(s) > 60 {
		b.WriteString(s[:60] + "\n")
		s = s[60:]
	}
	b.WriteString(s)
	return b.String()
}
