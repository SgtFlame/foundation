// Package gitnodes is the git connector's nodes and tools (forge 24d):
// List repository tree, Read repository file, Commit files and Open pull
// request for graphs, and git.repositories, git.tree, git.read,
// git.commit and git.pullRequest for the chat and agents. They call
// git.repo@1 and git.review@1 through the connector bus as their caller;
// a commit is keyed, so a retried job or call commits once, and it goes
// to the default branch only when the connection's allowDefaultBranch
// says so (the plan's question 5).
package gitnodes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/jobs"
)

const source = "git"

// Caller calls a connector operation: the dispatcher's Call.
type Caller func(args features.ServiceArgs, c connectors.Call) (json.RawMessage, *connectors.Error)

// Repository is a git connection the caller may read.
type Repository struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Status   string         `json:"status"`
	Settings map[string]any `json:"settings,omitempty"`
}

// Deps are the nodes' and tools' dependencies, made after the registry.
type Deps struct {
	Call Caller
	// Repositories lists the connections implementing git.repo@1 that the
	// caller may read (connections.List); the git.repositories tool's.
	Repositories func(args features.ServiceArgs) ([]Repository, error)
}

// MaxTreeEntries bounds what one tree listing gathers across pages.
const MaxTreeEntries = 10000

func (d Deps) call(args features.ServiceArgs, contract, conn, op string, in, out any) *connectors.Error {
	if d.Call == nil {
		return connectors.Errorf(connectors.CodeUnavailable, "there is no connector bus here")
	}
	raw, _ := json.Marshal(in)
	res, e := d.Call(args, connectors.Call{Contract: contract, Operation: op, Connection: conn, Args: raw})
	if e != nil {
		return e
	}
	if out != nil {
		if err := json.Unmarshal(res, out); err != nil {
			return connectors.Errorf(connectors.CodeUnavailable, "the %s result cannot be read: %v", op, err)
		}
	}
	return nil
}

// TreeResult is a whole listing, its pages gathered.
type TreeResult struct {
	SHA       string      `json:"sha"`
	Entries   []git.Entry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// Tree lists a folder at a ref, following pages up to max entries.
func (d Deps) Tree(args features.ServiceArgs, conn, ref, path string, recursive bool, max int) (TreeResult, *connectors.Error) {
	out := TreeResult{Entries: []git.Entry{}}
	cursor := ""
	for {
		var page git.Tree
		in := map[string]any{"ref": ref, "path": path, "recursive": recursive, "limit": git.MaxPage}
		if cursor != "" {
			in["cursor"] = cursor
		}
		if e := d.call(args, git.RepoRef, conn, "readTree", in, &page); e != nil {
			return out, e
		}
		out.SHA = page.SHA
		out.Entries = append(out.Entries, page.Entries...)
		if page.Next == "" {
			return out, nil
		}
		if len(out.Entries) >= max {
			out.Entries, out.Truncated = out.Entries[:max], true
			return out, nil
		}
		cursor = page.Next
	}
}

// ReadFile reads one file at a ref.
func (d Deps) ReadFile(args features.ServiceArgs, conn, ref, path string) (map[string]any, *connectors.Error) {
	var out map[string]any
	e := d.call(args, git.RepoRef, conn, "readFile", map[string]any{"ref": ref, "path": path}, &out)
	return out, e
}

// CommitInput is a commit as the nodes and tools take it.
type CommitInput struct {
	Connection string           `json:"connection"`
	Branch     string           `json:"branch"`
	Base       string           `json:"base,omitempty"`
	ExpectHead string           `json:"expectHead,omitempty"`
	Message    string           `json:"message"`
	Author     *git.Author      `json:"author,omitempty"`
	Changes    []map[string]any `json:"changes"`
	Key        string           `json:"key,omitempty"`
}

// Commit checks the branch against the connection's default-branch rule
// and commits.
func (d Deps) Commit(args features.ServiceArgs, c CommitInput) (git.CommitResult, *connectors.Error) {
	if c.Branch == "" {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeInvalid, "a commit needs a branch")
	}
	if len(c.Changes) == 0 {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeInvalid, "a commit needs at least one change")
	}
	var chk struct {
		DefaultBranch      string `json:"defaultBranch"`
		AllowDefaultBranch bool   `json:"allowDefaultBranch"`
	}
	if e := d.call(args, git.RepoRef, c.Connection, "check", map[string]any{}, &chk); e != nil {
		return git.CommitResult{}, e
	}
	if b, err := git.BranchName(c.Branch); err == nil && b == chk.DefaultBranch && !chk.AllowDefaultBranch {
		return git.CommitResult{}, connectors.Errorf(connectors.CodeInvalid,
			"%s is the repository's default branch, and this connection does not allow committing to it (allowDefaultBranch): commit to another branch and open a pull request", b)
	}
	in := map[string]any{"branch": c.Branch, "message": c.Message, "changes": c.Changes, "key": c.Key}
	if c.Base != "" {
		in["base"] = c.Base
	}
	if c.ExpectHead != "" {
		in["expectHead"] = c.ExpectHead
	}
	if c.Author != nil && c.Author.Name != "" {
		in["author"] = c.Author
	}
	var res git.CommitResult
	e := d.call(args, git.RepoRef, c.Connection, "commit", in, &res)
	return res, e
}

// PullRequestInput is a pull request to open.
type PullRequestInput struct {
	Connection string `json:"connection"`
	Head       string `json:"head"`
	Base       string `json:"base,omitempty"`
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	Draft      bool   `json:"draft,omitempty"`
	Key        string `json:"key,omitempty"`
}

// OpenPullRequest opens a pull request (or gives the open one).
func (d Deps) OpenPullRequest(args features.ServiceArgs, p PullRequestInput) (git.PullRequest, *connectors.Error) {
	in := map[string]any{"head": p.Head, "title": p.Title, "draft": p.Draft, "key": p.Key}
	if p.Base != "" {
		in["base"] = p.Base
	}
	if p.Body != "" {
		in["body"] = p.Body
	}
	var pr git.PullRequest
	e := d.call(args, git.ReviewRef, p.Connection, "openPullRequest", in, &pr)
	return pr, e
}

// Key is the idempotency key of a write: the job (stable across a job's
// retries) or else the run, the node or tool call, and what is written,
// so a loop writing different things writes each once.
func Key(args features.ServiceArgs, where string, what any) string {
	h := sha256.New()
	if i, ok := jobs.InfoOf(args.Context()); ok {
		fmt.Fprintf(h, "job:%s|", i.ID)
	} else {
		fmt.Fprintf(h, "run:%d|round:%d|", args.Meta.RunID, args.Meta.RoundID)
	}
	fmt.Fprintf(h, "%s|%s|", where, strconv.FormatUint(args.Principal.TenantID, 10))
	b, _ := json.Marshal(what)
	h.Write(b)
	return "ax-" + hex.EncodeToString(h.Sum(nil))[:24]
}
