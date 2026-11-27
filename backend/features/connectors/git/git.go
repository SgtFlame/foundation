package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/records"
)

func sprintf(format, a string) string { return fmt.Sprintf(format, a) }

// Files is the tenant's file store as the git types use it (records/files'
// Store): readFile keeps large files there as the caller, and commit reads
// the files a change names, as the caller may.
type Files interface {
	Put(ctx context.Context, tenant, user uint64, name, contentType string, r io.Reader, max int64) (records.FileRef, error)
	Uploader(ctx context.Context, tenant uint64, id string) (uint64, bool, error)
	Open(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error)
}

// MaxFileBytes is the largest file read or written through the file store
// (the records section's default maxFileMB, 25).
const MaxFileBytes int64 = 25 << 20

// Ref is one entry of listRefs.
type Ref struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

// Entry is one entry of readTree.
type Entry struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // file or dir
	Size int64  `json:"size,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

// Tree is readTree's result.
type Tree struct {
	SHA     string  `json:"sha"`
	Entries []Entry `json:"entries"`
	Next    string  `json:"next,omitempty"`
}

// File is readFile's result.
type File struct {
	Path     string           `json:"path"`
	SHA      string           `json:"sha"`
	Size     int64            `json:"size"`
	Encoding string           `json:"encoding,omitempty"`
	Content  string           `json:"content,omitempty"`
	File     *records.FileRef `json:"file,omitempty"`
}

// Change is one changed path of compare.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added, modified or deleted
}

// Statuses of a change.
const (
	Added    = "added"
	Modified = "modified"
	Deleted  = "deleted"
)

// Comparison is compare's result.
type Comparison struct {
	Base    string   `json:"base"`
	Head    string   `json:"head"`
	Changes []Change `json:"changes"`
}

// Author is who a commit is by.
type Author struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// FileArg is a file reference as a change gives it.
type FileArg struct {
	records.FileRef
	HeldBy json.RawMessage `json:"heldBy,omitempty"`
}

// ChangeArg is one change of commit, as sent.
type ChangeArg struct {
	Path     string   `json:"path"`
	Content  *string  `json:"content,omitempty"`
	Encoding string   `json:"encoding,omitempty"`
	File     *FileArg `json:"file,omitempty"`
	Delete   bool     `json:"delete,omitempty"`
}

// CommitArgs are commit's arguments.
type CommitArgs struct {
	Branch     string      `json:"branch"`
	Base       string      `json:"base,omitempty"`
	ExpectHead string      `json:"expectHead,omitempty"`
	Message    string      `json:"message"`
	Author     *Author     `json:"author,omitempty"`
	Changes    []ChangeArg `json:"changes"`
	Key        string      `json:"key,omitempty"`
}

// CommitResult is commit's result.
type CommitResult struct {
	SHA       string `json:"sha"`
	Branch    string `json:"branch"`
	Before    string `json:"before,omitempty"`
	Created   bool   `json:"created,omitempty"`
	Unchanged bool   `json:"unchanged,omitempty"`
	URL       string `json:"url,omitempty"`
}

// PullRequest is a pull request of git.review@1.
type PullRequest struct {
	Number    int    `json:"number"`
	URL       string `json:"url,omitempty"`
	State     string `json:"state"`
	Draft     bool   `json:"draft,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Head      string `json:"head"`
	Base      string `json:"base"`
	HeadSHA   string `json:"headSha,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Pushed is the payload of git.repo's pushed event.
type Pushed struct {
	Ref    string   `json:"ref"`
	Before string   `json:"before,omitempty"`
	After  string   `json:"after"`
	Paths  []string `json:"paths"`
}

// Write is one change ready to commit: a path with its new content, or
// deleted.
type Write struct {
	Path    string
	Data    []byte
	Deleted bool
}

// CleanPath checks a path from the repository's root: relative, no . or
// .. segments, no .git, no empty segments. It returns the path as given
// when it is clean.
func CleanPath(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") || strings.Contains(p, "\\") || strings.ContainsRune(p, 0) {
		return "", connectors.Errorf(connectors.CodeInvalid, "%q is not a path from the repository's root", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.EqualFold(seg, ".git") {
			return "", connectors.Errorf(connectors.CodeInvalid, "%q is not a path from the repository's root", p)
		}
	}
	return path.Clean(p), nil
}

// CleanDir checks a folder path for readTree: empty for the root, else a
// clean path (a trailing / is dropped).
func CleanDir(p string) (string, error) {
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", nil
	}
	return CleanPath(p)
}

// BranchName strips refs/heads/ and checks a branch name as git does
// (check-ref-format's main rules).
func BranchName(b string) (string, error) {
	b = strings.TrimPrefix(b, "refs/heads/")
	bad := b == "" || b == "HEAD" || strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".") ||
		strings.HasSuffix(b, ".lock") || strings.Contains(b, "..") || strings.Contains(b, "//") || strings.Contains(b, "@{") ||
		strings.ContainsAny(b, " ~^:?*[\\")
	for _, r := range b {
		if r < 0x20 || r == 0x7f {
			bad = true
		}
	}
	for _, seg := range strings.Split(b, "/") {
		if strings.HasPrefix(seg, ".") {
			bad = true
		}
	}
	if bad {
		return "", connectors.Errorf(connectors.CodeInvalid, "%q is not a branch name", b)
	}
	return b, nil
}

// Decode checks a commit's arguments and reads its changes: content
// inline, or a file from the tenant's file store that the caller may use
// (one it uploaded, any for a service principal, or one the dispatcher
// vouches for). A path changed twice is invalid.
func Decode(ctx adapter.Context, files Files, a *CommitArgs) ([]Write, error) {
	b, err := BranchName(a.Branch)
	if err != nil {
		return nil, err
	}
	a.Branch = b
	if strings.TrimSpace(a.Message) == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "a commit needs a message")
	}
	if len(a.Changes) == 0 {
		return nil, connectors.Errorf(connectors.CodeInvalid, "a commit needs changes")
	}
	seen := map[string]bool{}
	out := make([]Write, 0, len(a.Changes))
	for i, c := range a.Changes {
		p, err := CleanPath(c.Path)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			return nil, connectors.Errorf(connectors.CodeInvalid, "changes[%d] changes %s again", i, p)
		}
		seen[p] = true
		n := 0
		if c.Content != nil {
			n++
		}
		if c.File != nil {
			n++
		}
		if c.Delete {
			n++
		}
		if n != 1 {
			return nil, connectors.Errorf(connectors.CodeInvalid, "changes[%d] (%s) needs one of content, file or delete", i, p)
		}
		switch {
		case c.Delete:
			out = append(out, Write{Path: p, Deleted: true})
		case c.Content != nil:
			data := []byte(*c.Content)
			switch c.Encoding {
			case "", "text":
			case "base64":
				if data, err = base64.StdEncoding.DecodeString(*c.Content); err != nil {
					return nil, connectors.Errorf(connectors.CodeInvalid, "changes[%d] (%s) is not base64", i, p)
				}
			default:
				return nil, connectors.Errorf(connectors.CodeInvalid, "changes[%d] (%s) has encoding %q: text or base64", i, p, c.Encoding)
			}
			out = append(out, Write{Path: p, Data: data})
		default:
			data, err := ReadFile(ctx, files, c.File.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, Write{Path: p, Data: data})
		}
	}
	return out, nil
}

// ReadFile reads a file of the tenant's file store that the call's caller
// may use; to anyone else it does not exist (notFound).
func ReadFile(ctx adapter.Context, files Files, id string) ([]byte, error) {
	notFound := connectors.Errorf(connectors.CodeNotFound, "there is no file %s you may use", id)
	if files == nil || id == "" {
		return nil, notFound
	}
	p := ctx.Args.Principal
	user, ok, err := files.Uploader(ctx, ctx.Tenant(), id)
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the file store: %v", err)
	}
	own := user == p.UserID && p.Kind != features.PrincipalAgent
	if !ok || p.Kind != features.PrincipalService && !own && !ctx.Vouched(id) {
		return nil, notFound
	}
	r, _, ok, err := files.Open(ctx, ctx.Tenant(), id)
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the file store: %v", err)
	}
	if !ok {
		return nil, notFound
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the file store: %v", err)
	}
	if int64(len(data)) > MaxFileBytes {
		return nil, connectors.Errorf(connectors.CodeInvalid, "file %s is over %d MB", id, MaxFileBytes>>20)
	}
	return data, nil
}

// FileResult makes readFile's result: inline up to MaxInline (text when
// it is UTF-8, else base64), else stored in the file store as the caller.
func FileResult(ctx adapter.Context, files Files, p, sha string, data []byte) (File, error) {
	f := File{Path: p, SHA: sha, Size: int64(len(data))}
	if len(data) <= MaxInline {
		if utf8.Valid(data) && !bytes.ContainsRune(data, 0) {
			f.Encoding, f.Content = "text", string(data)
		} else {
			f.Encoding, f.Content = "base64", base64.StdEncoding.EncodeToString(data)
		}
		return f, nil
	}
	if files == nil {
		return File{}, connectors.Errorf(connectors.CodeInvalid, "%s is %d bytes, over the 1 MB read inline, and no file store is here", p, len(data))
	}
	if int64(len(data)) > MaxFileBytes {
		return File{}, connectors.Errorf(connectors.CodeInvalid, "%s is over %d MB", p, MaxFileBytes>>20)
	}
	ref, err := files.Put(ctx, ctx.Tenant(), ctx.Args.Principal.UserID, path.Base(p), "application/octet-stream", bytes.NewReader(data), MaxFileBytes)
	if err != nil {
		return File{}, connectors.Errorf(connectors.CodeUnavailable, "the file store: %v", err)
	}
	f.File = &ref
	return f, nil
}

// WithKey adds the idempotency key to a commit message as a trailer, so a
// retry that finds it on the branch makes no second commit; no key leaves
// the message as it is.
func WithKey(message, key string) string {
	if key == "" {
		return message
	}
	return strings.TrimRight(message, "\n") + "\n\n" + KeyTrailer + ": " + key + "\n"
}

// HasKey reports whether a commit message carries the key's trailer.
func HasKey(message, key string) bool {
	if key == "" {
		return false
	}
	for _, line := range strings.Split(message, "\n") {
		if strings.TrimSpace(line) == KeyTrailer+": "+key {
			return true
		}
	}
	return false
}

// KeyDepth is how many commits back from a branch's head a type looks for
// a key already committed.
const KeyDepth = 50

// Diff compares two snapshots (path → blob sha): every path added,
// modified or deleted, sorted.
func Diff(base, head map[string]string) []Change {
	out := []Change{}
	for p, h := range head {
		b, ok := base[p]
		switch {
		case !ok:
			out = append(out, Change{Path: p, Status: Added})
		case b != h:
			out = append(out, Change{Path: p, Status: Modified})
		}
	}
	for p := range base {
		if _, ok := head[p]; !ok {
			out = append(out, Change{Path: p, Status: Deleted})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Paths are a comparison's paths.
func Paths(cs []Change) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Path
	}
	return out
}

// List makes one readTree page from a snapshot's files (path → entry, the
// entry's Path set): the children of dir, or every file below it when
// recursive, after the cursor. A dir with no files below it is notFound.
func List(files map[string]Entry, dir string, recursive bool, cursor string, limit int) ([]Entry, string, error) {
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	found := dir == ""
	var all []Entry
	dirs := map[string]bool{}
	for p, e := range files {
		if !strings.HasPrefix(p, prefix) {
			if p == dir {
				return nil, "", connectors.Errorf(connectors.CodeInvalid, "%s is a file, not a folder", dir)
			}
			continue
		}
		found = true
		rest := p[len(prefix):]
		if recursive {
			all = append(all, e)
			continue
		}
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			d := prefix + rest[:i]
			if !dirs[d] {
				dirs[d] = true
				all = append(all, Entry{Path: d, Kind: "dir"})
			}
			continue
		}
		all = append(all, e)
	}
	if !found {
		return nil, "", connectors.Errorf(connectors.CodeNotFound, "there is no folder %s", dir)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	start := sort.Search(len(all), func(i int) bool { return all[i].Path > cursor })
	if cursor == "" {
		start = 0
	}
	end := min(start+limit, len(all))
	page := append([]Entry{}, all[start:end]...)
	next := ""
	if end < len(all) && len(page) > 0 {
		next = page[len(page)-1].Path
	}
	return page, next, nil
}

// Unmarshal decodes a call's args, invalid when they do not decode.
func Unmarshal(args json.RawMessage, into any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, into); err != nil {
		return connectors.Errorf(connectors.CodeInvalid, "the arguments: %v", err)
	}
	return nil
}

// Result marshals a call's result.
func Result(v any) (json.RawMessage, error) { return json.Marshal(v) }

// LazyFiles is a Files given its store later (serve.go makes the adapter
// options before the record files' store): until Set, every call fails.
type LazyFiles struct {
	mu sync.RWMutex
	f  Files
}

// Set gives it the store.
func (l *LazyFiles) Set(f Files) {
	l.mu.Lock()
	l.f = f
	l.mu.Unlock()
}

func (l *LazyFiles) get() (Files, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.f == nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the file store is not ready")
	}
	return l.f, nil
}

func (l *LazyFiles) Put(ctx context.Context, tenant, user uint64, name, contentType string, r io.Reader, max int64) (records.FileRef, error) {
	f, err := l.get()
	if err != nil {
		return records.FileRef{}, err
	}
	return f.Put(ctx, tenant, user, name, contentType, r, max)
}

func (l *LazyFiles) Uploader(ctx context.Context, tenant uint64, id string) (uint64, bool, error) {
	f, err := l.get()
	if err != nil {
		return 0, false, err
	}
	return f.Uploader(ctx, tenant, id)
}

func (l *LazyFiles) Open(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error) {
	f, err := l.get()
	if err != nil {
		return nil, records.FileRef{}, false, err
	}
	return f.Open(ctx, tenant, id)
}

// ServerFiles is this process's file store for the git types, set by
// serve.go once the record files' store exists.
var ServerFiles = &LazyFiles{}
