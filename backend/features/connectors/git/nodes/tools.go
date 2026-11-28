package gitnodes

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Tool names.
const (
	ToolRepositories = "git.repositories"
	ToolTree         = "git.tree"
	ToolRead         = "git.read"
	ToolCommit       = "git.commit"
	ToolPullRequest  = "git.pullRequest"
)

// MaxToolText is how much of a file git.read gives the model.
const MaxToolText = 100 << 10

// MaxToolEntries is how many entries git.tree gives the model.
const MaxToolEntries = 2000

const connectionArg = `"connection":{"type":"string","description":"The git connection's id (git.repositories lists them)"}`

func content(v any) map[string]any {
	var out map[string]any
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &out)
	return out
}

// toolErr is a connector error as a tool's error: its text reaches the
// model and the user, and a connector's messages name nothing private.
func toolErr(e *connectors.Error) error {
	return fmt.Errorf("%s: %s", e.Code, e.Message)
}

// ContributeTools registers the git tools for the chat and agents.
func ContributeTools(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: ToolRepositories, Source: source}, Title: "List git repositories", Effect: tools.EffectRead,
		Description: "Lists the git repositories you may use: each connection whose type implements git.repo@1, with its id (what the other git tools take as connection), name, type (git.github, git.https, gittest.repo), status and settings (owner and repo, or url; allowDefaultBranch). Secrets are never shown.",
		Input:       json.RawMessage(`{"type":"object","properties":{}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			d := deps()
			if d.Repositories == nil {
				return tools.Result{}, fmt.Errorf("there are no connections here")
			}
			repos, err := d.Repositories(call.ServiceArgs)
			if err != nil {
				return tools.Result{}, err
			}
			if repos == nil {
				repos = []Repository{}
			}
			return tools.Result{Content: map[string]any{"repositories": repos}, Summary: fmt.Sprintf("Found %d git repositories", len(repos))}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: ToolTree, Source: source}, Title: "List repository files", Effect: tools.EffectRead,
		Description: "Lists the files and folders of a git repository's folder at a branch, tag or commit: the folder's children, or every file under it with recursive. Gives {sha, entries: [{path, kind, size, sha}], truncated} (at most 2000 entries).",
		Input: json.RawMessage(`{"type":"object","properties":{` + connectionArg + `,
  "ref":{"type":"string","description":"A branch, tag or commit sha; the default branch when left out"},
  "path":{"type":"string","description":"The folder from the repository's root; the root when left out"},
  "recursive":{"type":"boolean","description":"Every file under the folder, not only its children"}},"required":["connection"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Connection, Ref, Path string
				Recursive             bool
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			t, e := deps().Tree(call.ServiceArgs, a.Connection, a.Ref, a.Path, a.Recursive, MaxToolEntries)
			if e != nil {
				return tools.Result{}, toolErr(e)
			}
			where := a.Path
			if where == "" {
				where = "the root"
			}
			return tools.Result{Content: content(t), Summary: fmt.Sprintf("Listed %d entries under %s", len(t.Entries), where)}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: ToolRead, Source: source}, Title: "Read a repository file", Effect: tools.EffectRead,
		Description: "Reads one file of a git repository at a branch, tag or commit. Gives {path, sha, size, encoding, content}: text as it is (cut at 100 KB, with truncated), binary as base64; a file over 1 MB is stored and given as file instead of content.",
		Input: json.RawMessage(`{"type":"object","properties":{` + connectionArg + `,
  "ref":{"type":"string","description":"A branch, tag or commit sha; the default branch when left out"},
  "path":{"type":"string","description":"The file's path from the repository's root"}},"required":["connection","path"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Connection, Ref, Path string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			f, e := deps().ReadFile(call.ServiceArgs, a.Connection, a.Ref, a.Path)
			if e != nil {
				return tools.Result{}, toolErr(e)
			}
			if s, ok := f["content"].(string); ok && len(s) > MaxToolText {
				f["content"], f["truncated"] = s[:MaxToolText], true
			}
			return tools.Result{Content: f, Summary: "Read `" + a.Path + "`"}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: ToolCommit, Source: source}, Title: "Commit to a repository", Effect: tools.EffectWrite, Timeout: 3 * time.Minute,
		Description: "Commits files to a branch of a git repository as one commit: each change writes a file's text content (or base64 with encoding), or deletes it. A missing branch is made from base (the default branch when left out). Commit to a branch of your own, such as docs/<topic>, and then open a pull request: the repository's default branch is refused unless its connection allows it. Asks the user first. Gives {sha, branch, before, created, unchanged, url}.",
		Input: json.RawMessage(`{"type":"object","properties":{` + connectionArg + `,
  "branch":{"type":"string","description":"The branch to commit to"},
  "base":{"type":"string","description":"What a missing branch starts from; the default branch when left out"},
  "message":{"type":"string","description":"The commit message"},
  "changes":{"type":"array","description":"The changes","items":{"type":"object","properties":{
    "path":{"type":"string","description":"The file's path from the repository's root"},
    "content":{"type":"string","description":"The file's new content"},
    "encoding":{"type":"string","enum":["text","base64"],"description":"How content is written; text when left out"},
    "delete":{"type":"boolean","description":"Delete the file"}},"required":["path"]}}},"required":["connection","branch","message","changes"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[CommitInput](call)
			if err != nil {
				return tools.Result{}, err
			}
			a.ExpectHead, a.Author = "", nil
			a.Key = Key(call.ServiceArgs, "tool:"+call.ID, []any{a.Connection, a.Branch, a.Message, a.Changes})
			res, e := deps().Commit(call.ServiceArgs, a)
			if e != nil {
				return tools.Result{}, toolErr(e)
			}
			summary := fmt.Sprintf("Committed %d changes to `%s` (%s)", len(a.Changes), res.Branch, short(res.SHA))
			if res.Unchanged {
				summary = "Nothing changed on `" + res.Branch + "`"
			}
			return tools.Result{Content: content(res), Summary: summary}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: ToolPullRequest, Source: source}, Title: "Open a pull request", Effect: tools.EffectWrite,
		Description: "Opens a pull request from a branch into another (the default branch when base is left out) on a git repository whose type has pull requests (GitHub); gives the open one from that head into that base instead of a second. Asks the user first. Gives {number, url, state, title, head, base}.",
		Input: json.RawMessage(`{"type":"object","properties":{` + connectionArg + `,
  "head":{"type":"string","description":"The branch with the changes"},
  "base":{"type":"string","description":"The branch to merge into; the default branch when left out"},
  "title":{"type":"string","description":"The pull request's title"},
  "body":{"type":"string","description":"The description, in Markdown"},
  "draft":{"type":"boolean","description":"Open it as a draft"}},"required":["connection","head","title"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[PullRequestInput](call)
			if err != nil {
				return tools.Result{}, err
			}
			a.Key = Key(call.ServiceArgs, "tool:"+call.ID, []any{a.Connection, a.Head, a.Base})
			pr, e := deps().OpenPullRequest(call.ServiceArgs, a)
			if e != nil {
				return tools.Result{}, toolErr(e)
			}
			summary := fmt.Sprintf("Pull request #%d from `%s` into `%s`", pr.Number, pr.Head, pr.Base)
			if pr.URL != "" {
				summary = fmt.Sprintf("[Pull request #%d](%s) from `%s` into `%s`", pr.Number, pr.URL, pr.Head, pr.Base)
			}
			return tools.Result{Content: content(pr), Summary: summary}, nil
		},
	})
}

func short(sha string) string {
	return strings.TrimSpace(sha[:min(7, len(sha))])
}
