// Package github is the git.github connector type (forge 24b): a GitHub
// repository through the REST v3 API. Reads use the refs, commits, trees
// and contents endpoints; a commit of many files is made without cloning
// through the Git Data API (blobs, a tree on the parent's, a commit, then
// the ref moved with force false, which is the expectHead check); pull
// requests are git.review@1. pushed and pullRequestChanged are polled at
// the connection's pollSeconds. Authentication is a fine-grained personal
// access token (contents and pull requests, read and write, on chosen
// repositories); Sign in with GitHub comes with 26a-2.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// TypeID is the type's id.
const TypeID = "git.github"

// DefaultBase is GitHub's API root; GitHub Enterprise Server's is
// https://<host>/api/v3.
const DefaultBase = "https://api.github.com"

// SettingsSchema is a connection's settings, in Configuration's schema
// language; the token is a secret.
const SettingsSchema = `{"type":"object","properties":{
  "owner":{"type":"string","title":"Owner","description":"The repository's owner, a user or organization (SgtFlame)"},
  "repo":{"type":"string","title":"Repository","description":"The repository's name (code-and-castles)"},
  "token":{"type":"secret","title":"Access token","description":"A fine-grained personal access token for the repository, with Contents and Pull requests read and write"},
  "baseUrl":{"type":"string","title":"API URL","description":"The API's root: https://api.github.com, or https://<host>/api/v3 for GitHub Enterprise Server"},
  "pollSeconds":{"type":"integer","minimum":15,"title":"Poll every","description":"Seconds between looks for pushes and pull request changes; 60 when left out"},
  "allowDefaultBranch":{"type":"boolean","title":"Allow commits to the default branch","description":"Let the git tools and nodes commit straight to the default branch; otherwise they commit to another branch and open a pull request"}},"required":["owner","repo"]}`

// Type is git.github.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{git.RepoRef, git.ReviewRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Services: []string{"files"},
		Meta:     connectors.Meta{Title: "GitHub", Description: "A GitHub repository through GitHub's API, with a fine-grained access token: reads branches, trees and files, commits many files at once without cloning, opens and reads pull requests, and notices pushes and pull request changes by polling.", Tags: []string{"git", "github", "code"}, Icon: "github"},
	}
}

// Contribute registers the type (its contracts are git's).
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, git.Source, nil, []connectors.Type{Type()})
}

// Settings are a connection's settings, resolved.
type Settings struct {
	Owner              string `json:"owner"`
	Repo               string `json:"repo"`
	Token              string `json:"token"`
	BaseURL            string `json:"baseUrl"`
	PollSeconds        int    `json:"pollSeconds"`
	AllowDefaultBranch bool   `json:"allowDefaultBranch"`
}

// Options configure the implementation.
type Options struct {
	HTTP  *http.Client // http.DefaultClient's transport, with a timeout, when nil
	Files git.Files
	// Poll overrides pollSeconds, for tests.
	Poll time.Duration
}

// Impl is git.github's adapter implementation.
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

// Session is one connection's client.
type Session struct {
	st    Settings
	base  string
	hc    *http.Client
	files git.Files
	poll  time.Duration
}

// New makes a session from settings.
func New(st Settings, o Options) (*Session, error) {
	if st.Owner == "" || st.Repo == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "a GitHub connection needs an owner and a repository")
	}
	base := strings.TrimRight(st.BaseURL, "/")
	if base == "" {
		base = DefaultBase
	}
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	poll := o.Poll
	if poll == 0 {
		poll = time.Duration(max(st.PollSeconds, 15)) * time.Second
		if st.PollSeconds == 0 {
			poll = time.Minute
		}
	}
	return &Session{st: st, base: base, hc: hc, files: o.Files, poll: poll}, nil
}

func (s *Session) Close() error { return nil }

// apiError is GitHub's error body.
type apiError struct {
	Message string `json:"message"`
	Errors  []struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"errors"`
}

// do sends one request to /repos/<owner>/<repo><path> (or path itself when
// it starts with //) and decodes the JSON answer into out.
func (s *Session) do(ctx context.Context, method, path string, query url.Values, body, out any) (http.Header, error) {
	u := s.base + "/repos/" + url.PathEscape(s.st.Owner) + "/" + url.PathEscape(s.st.Repo) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "the request: %v", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the request: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "axiom-git-connector")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.st.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.st.Token)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, connectors.Errorf(connectors.CodeUnavailable, "cancelled")
		}
		return nil, &connectors.Error{Code: connectors.CodeUnavailable, Message: "GitHub did not answer", Retry: true}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 400 {
		return resp.Header, failure(resp, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.Header, connectors.Errorf(connectors.CodeUnavailable, "GitHub's answer is not JSON: %v", err)
		}
	}
	return resp.Header, nil
}

// failure maps a GitHub error to a connector error: a rate limit (429, or
// 403 with no requests left) is rateLimited with when to try again; 401
// and other 403s are auth; 404 notFound (GitHub answers 404 for a private
// repository the token cannot see); 409 conflict; 422 invalid, unless it
// says a fast-forward failed or a ref exists (conflict); 5xx unavailable.
func failure(resp *http.Response, raw []byte) *connectors.Error {
	var e apiError
	_ = json.Unmarshal(raw, &e)
	msg := e.Message
	for _, d := range e.Errors {
		if d.Message != "" {
			msg += ": " + d.Message
		}
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	low := strings.ToLower(msg)
	code := resp.StatusCode
	switch {
	case code == http.StatusTooManyRequests || code == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(low, "rate limit")):
		ce := &connectors.Error{Code: connectors.CodeRateLimited, Message: "GitHub's rate limit: " + msg, Retry: true}
		if ra, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			ce.RetryAfterMs = int64(ra) * 1000
		} else if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			ce.RetryAfterMs = max(0, time.Until(time.Unix(reset, 0)).Milliseconds())
		}
		return ce
	case strings.Contains(low, "protected branch"):
		return connectors.Errorf(connectors.CodeInvalid, "GitHub refused: %s", msg)
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return connectors.Errorf(connectors.CodeAuth, "GitHub refused the token: %s", msg)
	case code == http.StatusNotFound:
		return connectors.Errorf(connectors.CodeNotFound, "GitHub: %s", msg)
	case code == http.StatusConflict:
		return connectors.Errorf(connectors.CodeConflict, "GitHub: %s", msg)
	case code == http.StatusUnprocessableEntity && (strings.Contains(low, "fast forward") || strings.Contains(low, "already exists")):
		return connectors.Errorf(connectors.CodeConflict, "GitHub: %s", msg)
	case code >= 500:
		return &connectors.Error{Code: connectors.CodeUnavailable, Message: "GitHub: " + msg, Retry: true}
	}
	return connectors.Errorf(connectors.CodeInvalid, "GitHub: %s", msg)
}

func esc(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// repoInfo is GET /repos/{o}/{r}.
type repoInfo struct {
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

func (s *Session) repo(ctx context.Context) (repoInfo, error) {
	var r repoInfo
	_, err := s.do(ctx, http.MethodGet, "", nil, nil, &r)
	return r, err
}

// commitInfo is the part of GET /commits/{ref} used.
type commitInfo struct {
	SHA     string                 `json:"sha"`
	HTMLURL string                 `json:"html_url"`
	Parents []struct{ SHA string } `json:"parents"`
	Commit  struct {
		Message string `json:"message"`
		Tree    struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	} `json:"commit"`
}

// resolve finds the commit a ref names (a branch, full ref or sha).
func (s *Session) resolve(ctx context.Context, ref string) (commitInfo, error) {
	if ref == "" {
		r, err := s.repo(ctx)
		if err != nil {
			return commitInfo{}, err
		}
		ref = r.DefaultBranch
	}
	var c commitInfo
	_, err := s.do(ctx, http.MethodGet, "/commits/"+esc(strings.TrimPrefix(ref, "refs/heads/")), nil, nil, &c)
	if ce := connectors.AsError(err); ce != nil && (ce.Code == connectors.CodeInvalid || ce.Code == connectors.CodeNotFound) {
		return commitInfo{}, connectors.Errorf(connectors.CodeNotFound, "there is no ref or commit %q", ref)
	}
	return c, err
}

type treeEntry struct {
	Path string  `json:"path"`
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	SHA  *string `json:"sha"`
	Size int64   `json:"size,omitempty"`
	// Content is set only when making a tree
	Content *string `json:"content,omitempty"`
}

// files reads a tree's every file (path → entry).
func (s *Session) treeFiles(ctx context.Context, tree string) (map[string]git.Entry, error) {
	var t struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if _, err := s.do(ctx, http.MethodGet, "/git/trees/"+url.PathEscape(tree), url.Values{"recursive": {"1"}}, nil, &t); err != nil {
		return nil, err
	}
	if t.Truncated {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the repository's tree is larger than GitHub lists at once")
	}
	out := make(map[string]git.Entry, len(t.Tree))
	for _, e := range t.Tree {
		if e.Type != "blob" || e.SHA == nil {
			continue
		}
		out[e.Path] = git.Entry{Path: e.Path, Kind: "file", Size: e.Size, SHA: *e.SHA}
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
		r, err := s.repo(ctx)
		if err != nil {
			return nil, err
		}
		return git.Result(map[string]any{"ok": true, "defaultBranch": r.DefaultBranch, "canWrite": r.Permissions.Push})
	case "listRefs":
		var a struct{ Prefix string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		refs, err := s.refs(ctx, a.Prefix)
		if err != nil {
			return nil, err
		}
		r, err := s.repo(ctx)
		if err != nil {
			return nil, err
		}
		return git.Result(map[string]any{"refs": refs, "defaultBranch": r.DefaultBranch})
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
		c, err := s.resolve(ctx, a.Ref)
		if err != nil {
			return nil, err
		}
		files, err := s.treeFiles(ctx, c.Commit.Tree.SHA)
		if err != nil {
			return nil, err
		}
		page, next, err := git.List(files, dir, a.Recursive, a.Cursor, a.Limit)
		if err != nil {
			return nil, err
		}
		return git.Result(git.Tree{SHA: c.SHA, Entries: page, Next: next})
	case "readFile":
		var a struct{ Ref, Path string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		p, err := git.CleanPath(a.Path)
		if err != nil {
			return nil, err
		}
		sha, data, err := s.readFile(ctx, a.Ref, p)
		if err != nil {
			return nil, err
		}
		f, err := git.FileResult(ctx, s.files, p, sha, data)
		if err != nil {
			return nil, err
		}
		return git.Result(f)
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
		writes, err := git.Decode(ctx, s.files, &a)
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
	case "openPullRequest":
		return s.openPullRequest(ctx, args)
	case "getPullRequest":
		var a struct{ Number int }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		var p pull
		if _, err := s.do(ctx, http.MethodGet, "/pulls/"+strconv.Itoa(a.Number), nil, nil, &p); err != nil {
			return nil, err
		}
		return git.Result(p.toPR())
	case "listPullRequests":
		var a struct{ State, Head, Base string }
		if err := git.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		prs, err := s.pulls(ctx, a.State, a.Head, a.Base, "created", 100)
		if err != nil {
			return nil, err
		}
		return git.Result(map[string]any{"pullRequests": prs})
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "git.github has no operation %q", op)
}

// refs lists refs starting with prefix (every ref when empty).
func (s *Session) refs(ctx context.Context, prefix string) ([]git.Ref, error) {
	var out []git.Ref
	// matching-refs takes a prefix without refs/; it matches refs starting
	// with it, so a prefix that is not a whole segment works too
	m := strings.TrimPrefix(prefix, "refs/")
	if prefix != "" && !strings.HasPrefix(prefix, "refs/") {
		return []git.Ref{}, nil
	}
	for page := 1; page <= 50; page++ {
		var got []struct {
			Ref    string `json:"ref"`
			Object struct {
				SHA  string `json:"sha"`
				Type string `json:"type"`
			} `json:"object"`
		}
		if _, err := s.do(ctx, http.MethodGet, "/git/matching-refs/"+esc(m), url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}, nil, &got); err != nil {
			return nil, err
		}
		for _, r := range got {
			if strings.HasPrefix(r.Ref, prefix) {
				out = append(out, git.Ref{Name: r.Ref, SHA: r.Object.SHA})
			}
		}
		if len(got) < 100 {
			break
		}
	}
	if out == nil {
		out = []git.Ref{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// readFile reads a file's blob at a ref: from the contents endpoint, or
// the blob when it is over the inline size the contents endpoint gives.
func (s *Session) readFile(ctx context.Context, ref, p string) (string, []byte, error) {
	var raw json.RawMessage
	q := url.Values{}
	if ref != "" {
		q.Set("ref", ref)
	}
	if _, err := s.do(ctx, http.MethodGet, "/contents/"+esc(p), q, nil, &raw); err != nil {
		if ce := connectors.AsError(err); ce != nil && ce.Code == connectors.CodeNotFound {
			return "", nil, connectors.Errorf(connectors.CodeNotFound, "there is no file %s at %s", p, ref)
		}
		return "", nil, err
	}
	if len(raw) > 0 && raw[0] == '[' {
		return "", nil, connectors.Errorf(connectors.CodeInvalid, "%s is a folder, not a file", p)
	}
	var c struct {
		Type     string `json:"type"`
		SHA      string `json:"sha"`
		Size     int64  `json:"size"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	_ = json.Unmarshal(raw, &c)
	if c.Type != "" && c.Type != "file" {
		return "", nil, connectors.Errorf(connectors.CodeInvalid, "%s is a %s, not a file", p, c.Type)
	}
	if c.Encoding == "base64" && (c.Content != "" || c.Size == 0) {
		data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(c.Content, "\n", ""))
		if err != nil {
			return "", nil, connectors.Errorf(connectors.CodeUnavailable, "GitHub's content of %s is not base64", p)
		}
		return c.SHA, data, nil
	}
	if c.Size > git.MaxFileBytes {
		return "", nil, connectors.Errorf(connectors.CodeInvalid, "%s is over %d MB", p, git.MaxFileBytes>>20)
	}
	var b struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if _, err := s.do(ctx, http.MethodGet, "/git/blobs/"+url.PathEscape(c.SHA), nil, nil, &b); err != nil {
		return "", nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(b.Content, "\n", ""))
	if err != nil {
		return "", nil, connectors.Errorf(connectors.CodeUnavailable, "GitHub's blob of %s is not base64", p)
	}
	return c.SHA, data, nil
}

// compare diffs two commits' trees (exactly base to head, not from their
// merge base as GitHub's compare does).
func (s *Session) compare(ctx context.Context, base, head string) (git.Comparison, error) {
	b, err := s.resolve(ctx, base)
	if err != nil {
		return git.Comparison{}, err
	}
	h, err := s.resolve(ctx, head)
	if err != nil {
		return git.Comparison{}, err
	}
	bf, err := s.treeFiles(ctx, b.Commit.Tree.SHA)
	if err != nil {
		return git.Comparison{}, err
	}
	hf := bf
	if h.Commit.Tree.SHA != b.Commit.Tree.SHA {
		if hf, err = s.treeFiles(ctx, h.Commit.Tree.SHA); err != nil {
			return git.Comparison{}, err
		}
	}
	return git.Comparison{Base: b.SHA, Head: h.SHA, Changes: git.Diff(snapshot(bf), snapshot(hf))}, nil
}

// head is a branch's commit, "" when it does not exist.
func (s *Session) head(ctx context.Context, branch string) (string, error) {
	var r struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	_, err := s.do(ctx, http.MethodGet, "/git/ref/heads/"+esc(branch), nil, nil, &r)
	if ce := connectors.AsError(err); ce != nil && ce.Code == connectors.CodeNotFound {
		return "", nil
	}
	return r.Object.SHA, err
}

// commit makes one commit with the Git Data API.
func (s *Session) commit(ctx adapter.Context, a git.CommitArgs, writes []git.Write) (git.CommitResult, error) {
	head, err := s.head(ctx, a.Branch)
	if err != nil {
		return git.CommitResult{}, err
	}
	// A key already on the branch: this is a retry
	if a.Key != "" && head != "" {
		var log []struct {
			SHA     string                 `json:"sha"`
			Parents []struct{ SHA string } `json:"parents"`
			Commit  struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		if _, err := s.do(ctx, http.MethodGet, "/commits", url.Values{"sha": {head}, "per_page": {strconv.Itoa(git.KeyDepth)}}, nil, &log); err != nil {
			return git.CommitResult{}, err
		}
		for _, c := range log {
			if git.HasKey(c.Commit.Message, a.Key) {
				before := ""
				if len(c.Parents) > 0 {
					before = c.Parents[0].SHA
				}
				return git.CommitResult{SHA: c.SHA, Branch: a.Branch, Before: before, URL: s.commitURL(c.SHA)}, nil
			}
		}
	}
	if a.ExpectHead != "" && a.ExpectHead != head {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeConflict, "branch %s has moved since %s", a.Branch, a.ExpectHead[:min(7, len(a.ExpectHead))])
	}
	var parent commitInfo
	if head != "" {
		parent, err = s.resolve(ctx, head)
	} else {
		parent, err = s.resolve(ctx, a.Base)
	}
	if err != nil {
		return git.CommitResult{}, err
	}
	entries := make([]treeEntry, 0, len(writes))
	for _, w := range writes {
		e := treeEntry{Path: w.Path, Mode: "100644", Type: "blob"}
		switch {
		case w.Deleted:
			e.SHA = nil // a null sha deletes the path
		case isText(w.Data):
			content := string(w.Data)
			e.Content = &content
		default:
			var b struct {
				SHA string `json:"sha"`
			}
			if _, err := s.do(ctx, http.MethodPost, "/git/blobs", nil, map[string]string{"content": base64.StdEncoding.EncodeToString(w.Data), "encoding": "base64"}, &b); err != nil {
				return git.CommitResult{}, err
			}
			e.SHA = &b.SHA
		}
		entries = append(entries, e)
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if _, err := s.do(ctx, http.MethodPost, "/git/trees", nil, map[string]any{"base_tree": parent.Commit.Tree.SHA, "tree": entries}, &tree); err != nil {
		return git.CommitResult{}, err
	}
	if head != "" && tree.SHA == parent.Commit.Tree.SHA {
		return git.CommitResult{SHA: head, Branch: a.Branch, Before: head, Unchanged: true, URL: s.commitURL(head)}, nil
	}
	body := map[string]any{"message": git.WithKey(a.Message, a.Key), "tree": tree.SHA, "parents": []string{parent.SHA}}
	if a.Author != nil && a.Author.Name != "" && a.Author.Email != "" {
		body["author"] = map[string]string{"name": a.Author.Name, "email": a.Author.Email}
	}
	var made struct {
		SHA string `json:"sha"`
	}
	if _, err := s.do(ctx, http.MethodPost, "/git/commits", nil, body, &made); err != nil {
		return git.CommitResult{}, err
	}
	if head != "" {
		// force false: refused unless the branch is still at the parent,
		// which is the expectHead check against a racing push
		_, err = s.do(ctx, http.MethodPatch, "/git/refs/heads/"+esc(a.Branch), nil, map[string]any{"sha": made.SHA, "force": false}, nil)
	} else {
		_, err = s.do(ctx, http.MethodPost, "/git/refs", nil, map[string]any{"ref": "refs/heads/" + a.Branch, "sha": made.SHA}, nil)
	}
	if err != nil {
		if ce := connectors.AsError(err); ce != nil && ce.Code == connectors.CodeConflict {
			return git.CommitResult{}, connectors.Errorf(connectors.CodeConflict, "branch %s moved while committing: read it again", a.Branch)
		}
		return git.CommitResult{}, err
	}
	return git.CommitResult{SHA: made.SHA, Branch: a.Branch, Before: head, Created: head == "", URL: s.commitURL(made.SHA)}, nil
}

func (s *Session) commitURL(sha string) string {
	if s.base != DefaultBase {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", s.st.Owner, s.st.Repo, sha)
}

// isText reports whether content can go in a tree as UTF-8 text.
func isText(data []byte) bool {
	return utf8.Valid(data) && !bytes.ContainsRune(data, 0)
}

// pull is GitHub's pull request.
type pull struct {
	Number   int    `json:"number"`
	HTMLURL  string `json:"html_url"`
	State    string `json:"state"`
	Draft    bool   `json:"draft"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	MergedAt string `json:"merged_at"`
	Updated  string `json:"updated_at"`
	Head     struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p pull) toPR() git.PullRequest {
	state := p.State
	if state == "closed" && p.MergedAt != "" {
		state = "merged"
	}
	return git.PullRequest{Number: p.Number, URL: p.HTMLURL, State: state, Draft: p.Draft, Title: p.Title, Body: p.Body,
		Head: p.Head.Ref, Base: p.Base.Ref, HeadSHA: p.Head.SHA, UpdatedAt: p.Updated}
}

// pulls lists pull requests, newest by sort first.
func (s *Session) pulls(ctx context.Context, state, head, base, sortBy string, limit int) ([]git.PullRequest, error) {
	q := url.Values{"sort": {sortBy}, "direction": {"desc"}, "per_page": {strconv.Itoa(min(limit, 100))}}
	switch state {
	case "", "open":
		q.Set("state", "open")
	case "closed", "merged":
		q.Set("state", "closed")
	case "all":
		q.Set("state", "all")
	default:
		return nil, connectors.Errorf(connectors.CodeInvalid, "state is open, closed, merged or all, not %q", state)
	}
	if head != "" {
		q.Set("head", s.st.Owner+":"+head)
	}
	if base != "" {
		q.Set("base", base)
	}
	var got []pull
	if _, err := s.do(ctx, http.MethodGet, "/pulls", q, nil, &got); err != nil {
		return nil, err
	}
	out := []git.PullRequest{}
	for _, p := range got {
		pr := p.toPR()
		if state == "closed" && pr.State == "merged" || state == "merged" && pr.State != "merged" {
			continue
		}
		out = append(out, pr)
	}
	return out, nil
}

func (s *Session) openPullRequest(ctx adapter.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Head, Base, Title, Body, Key string
		Draft                        bool
	}
	if err := git.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Title) == "" || a.Head == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "a pull request needs a head branch and a title")
	}
	if a.Base == "" {
		r, err := s.repo(ctx)
		if err != nil {
			return nil, err
		}
		a.Base = r.DefaultBranch
	}
	// One already open from head into base is the one asked for
	open, err := s.pulls(ctx, "open", a.Head, a.Base, "created", 10)
	if err != nil {
		return nil, err
	}
	if len(open) > 0 {
		return git.Result(open[0])
	}
	var p pull
	if _, err := s.do(ctx, http.MethodPost, "/pulls", nil, map[string]any{"title": a.Title, "head": a.Head, "base": a.Base, "body": a.Body, "draft": a.Draft}, &p); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(p.toPR())
	_ = ctx.Accepted(raw)
	return raw, nil
}

// cursor is where Watch resumes: each branch's head, and the newest pull
// request change seen.
type cursor struct {
	Heads  map[string]string `json:"heads"`
	PRSeen string            `json:"prSeen,omitempty"`
}

// Watch polls the branch heads and the pull requests: a moved, new or
// deleted-then-recreated branch is pushed, with the paths its move
// changed; a pull request updated since the last look is
// pullRequestChanged. The first look (no cursor) only notes where things
// are.
func (s *Session) Watch(ctx adapter.Context, raw json.RawMessage, emit func(adapter.Event) error) error {
	var cur cursor
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cur)
	}
	pushDef, _ := git.RepoContract().Event(git.EventPush)
	prDef, _ := git.ReviewContract().Event(git.EventPR)
	first := cur.Heads == nil
	for {
		next, events, err := s.look(ctx, cur, first)
		if err == nil {
			for i, e := range events {
				def, contract := pushDef, git.RepoRef
				if e.Name == git.EventPR {
					def, contract = prDef, git.ReviewRef
				}
				e.Contract = contract
				e.ID = connectors.EventID(def, contract, e.Payload)
				e.At = time.Now()
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

// look reads where the branches and pull requests are now and what
// changed since cur.
func (s *Session) look(ctx context.Context, cur cursor, first bool) (cursor, []adapter.Event, error) {
	refs, err := s.refs(ctx, "refs/heads/")
	if err != nil {
		return cur, nil, err
	}
	next := cursor{Heads: map[string]string{}, PRSeen: cur.PRSeen}
	var events []adapter.Event
	for _, r := range refs {
		next.Heads[r.Name] = r.SHA
		before := cur.Heads[r.Name]
		if first || before == r.SHA {
			continue
		}
		p := git.Pushed{Ref: r.Name, Before: before, After: r.SHA, Paths: []string{}}
		base := before
		if base == "" {
			base = r.SHA // a new branch: its paths are those of its last commit
			if c, err := s.resolve(ctx, r.SHA); err == nil && len(c.Parents) > 0 {
				base = c.Parents[0].SHA
			}
		}
		if base != r.SHA {
			if cmp, err := s.compare(ctx, base, r.SHA); err == nil {
				p.Paths = git.Paths(cmp.Changes)
			}
		}
		payload, _ := json.Marshal(p)
		events = append(events, adapter.Event{Name: git.EventPush, Payload: payload})
	}
	prs, err := s.pulls(ctx, "all", "", "", "updated", 30)
	if err != nil {
		return cur, nil, err
	}
	newest := cur.PRSeen
	for i := len(prs) - 1; i >= 0; i-- { // oldest first
		pr := prs[i]
		if pr.UpdatedAt > newest {
			newest = pr.UpdatedAt
		}
		if first || pr.UpdatedAt <= cur.PRSeen {
			continue
		}
		payload, _ := json.Marshal(pr)
		events = append(events, adapter.Event{Name: git.EventPR, Payload: payload})
	}
	next.PRSeen = newest
	// The first look notes where things are without an event; a cursor is
	// stored with an event, so a restart before any looks afresh
	return next, events, nil
}
