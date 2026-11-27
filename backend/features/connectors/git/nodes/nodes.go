package gitnodes

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Node types.
const (
	NodeTree        = "git.tree"
	NodeReadFile    = "git.readFile"
	NodeCommit      = "git.commit"
	NodePullRequest = "git.pullRequest"
	NodeMirror      = "git.mirror"
)

// Failed is the nodes' exec output for a typed error.
const Failed = "failed"

func connectionPin() cognition.Pin {
	return cognition.Pin{ID: "connection", Name: "Connection", Type: "string",
		Description: "The git connection (a connection whose type implements git.repo@1: git.github, git.https, the Git test server)"}
}

func opt(id, name, typ, description string) cognition.Pin {
	return cognition.Pin{ID: id, Name: name, Type: typ, Optional: true, Description: description}
}

func req(id, name, typ, description string) cognition.Pin {
	return cognition.Pin{ID: id, Name: name, Type: typ, Description: description}
}

func errorPin() cognition.Pin {
	return cognition.DataPin("error", "Error", "json", "The typed error when it leaves by Error: {code, message, retryable, retryAfter}")
}

func execs(outs ...cognition.Pin) []cognition.Pin {
	return append([]cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.ExecPin(Failed, "Error")}, append(outs, errorPin())...)
}

// outcome leaves by Error with a typed error, or fails the run with a
// retryable one, so a job retries it.
func outcome(e *connectors.Error) (cognition.Values, string, error) {
	if e.Retryable() {
		return nil, "", e
	}
	var ev map[string]any
	b, _ := json.Marshal(e)
	_ = json.Unmarshal(b, &ev)
	return cognition.Values{"error": ev}, Failed, nil
}

func str(in cognition.Values, pin string) string {
	s, _ := cognition.Get[string](in, pin)
	return s
}

func flag(in cognition.Values, pin string) bool {
	b, _ := cognition.Get[bool](in, pin)
	return b
}

func asValues(v any) cognition.Values {
	var out cognition.Values
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &out)
	return out
}

// Contribute registers the git nodes.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	sha := "1f2e3d4c5b6a79881f2e3d4c5b6a79881f2e3d4c"
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeTree, Source: source, Order: 960}, Title: "List repository tree", Category: "Git",
		Effects: true, External: true,
		Description: "Lists the files and folders of a git repository's folder at a branch, tag or commit, through a git connection: the folder's children, or every file under it when Recursive is on, sorted by path, gathered over pages up to 10000 entries. A typed error (auth, notFound, invalid) leaves by Error; a retryable one fails the run, so a job retries it.",
		Example:     "List repository tree of the site's connection at main under www/content/code, recursively, to find the posts",
		Tags:        []string{"git", "code", "repository", "files"},
		Sample:      cognition.Values{"sha": sha, "entries": []any{map[string]any{"path": "README.md", "kind": "file", "size": 12, "sha": sha}}, "truncated": false},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), connectionPin(),
				opt("ref", "Ref", "string", "A branch, tag or commit sha; the default branch when empty"),
				opt("path", "Path", "string", "The folder from the repository's root; the root when empty"),
				opt("recursive", "Recursive", "bool", "Every file under the folder, not only its children")},
			Out: execs(
				cognition.DataPin("entries", "Entries", "json", "The entries: [{path, kind (file or dir), size, sha}]"),
				cognition.DataPin("sha", "Commit", "string", "The commit the ref named"),
				cognition.DataPin("truncated", "Truncated", "bool", "True when there were more than 10000 entries")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			t, e := deps().Tree(x.ServiceArgs, str(in, "connection"), str(in, "ref"), str(in, "path"), flag(in, "recursive"), MaxTreeEntries)
			if e != nil {
				return outcome(e)
			}
			entries := make([]any, 0, len(t.Entries))
			for _, en := range t.Entries {
				entries = append(entries, map[string]any(asValues(en)))
			}
			return cognition.Values{"entries": entries, "sha": t.SHA, "truncated": t.Truncated}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeReadFile, Source: source, Order: 961}, Title: "Read repository file", Category: "Git",
		Effects: true, External: true,
		Description: "Reads one file of a git repository at a branch, tag or commit, through a git connection. Text comes as Content; binary content as base64 (Encoding base64); a file over 1 MB is stored in the tenant's file store and given as File instead. A typed error (notFound for a missing file) leaves by Error; a retryable one fails the run.",
		Example:     "Read repository file www/hugo.toml at main, to read the site's settings",
		Tags:        []string{"git", "code", "repository", "files"},
		Sample:      cognition.Values{"content": "# Site\n", "encoding": "text", "sha": sha, "size": 7, "path": "README.md"},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), connectionPin(),
				opt("ref", "Ref", "string", "A branch, tag or commit sha; the default branch when empty"),
				req("path", "Path", "string", "The file's path from the repository's root")},
			Out: execs(
				cognition.DataPin("content", "Content", "string", "The file's content: text, or base64 when Encoding is base64; empty when it is stored as File"),
				cognition.DataPin("encoding", "Encoding", "string", "text or base64"),
				cognition.DataPin("file", "File", "json", "The stored file {id, name, size, contentType} for a file over 1 MB; null otherwise"),
				cognition.DataPin("sha", "Blob", "string", "The file's blob sha"),
				cognition.DataPin("size", "Size", "number", "The file's size in bytes"),
				cognition.DataPin("path", "Path", "string", "The file's path")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			f, e := deps().ReadFile(x.ServiceArgs, str(in, "connection"), str(in, "ref"), str(in, "path"))
			if e != nil {
				return outcome(e)
			}
			return cognition.Values{"content": f["content"], "encoding": f["encoding"], "file": f["file"], "sha": f["sha"], "size": f["size"], "path": f["path"]}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeCommit, Source: source, Order: 962}, Title: "Commit files", Category: "Git",
		Effects: true, External: true,
		Description: "Commits files to a branch of a git repository as one commit, through a git connection: each change writes a file's content (text, or base64), a stored file, or deletes the file. A missing branch is made from Base (the default branch when empty). The commit is keyed by the job (or run), this node and what it writes, so a retried job commits once; Key overrides it. The repository's default branch is refused unless the connection allows it (allowDefaultBranch): commit to another branch and open a pull request. Expect head refuses a branch that moved (conflict). Changes that change nothing make no commit (Unchanged).",
		Example:     "Commit files www/content/code/20261127-git/index.md and its images to branch docs/git-post, then Open pull request into main",
		Tags:        []string{"git", "code", "repository", "publish", "commit"},
		Sample:      cognition.Values{"sha": sha, "branch": "docs/post", "created": true, "unchanged": false, "url": "", "before": ""},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), connectionPin(),
				req("branch", "Branch", "string", "The branch to commit to"),
				req("message", "Message", "string", "The commit message"),
				req("changes", "Changes", "json", "The changes: [{path, content, encoding (text or base64)} | {path, file: {id, heldBy?}} | {path, delete: true}]"),
				opt("base", "Base", "string", "What a missing branch starts from: a branch or commit; the default branch when empty"),
				opt("expectHead", "Expect head", "string", "The commit the branch must still point at, else conflict"),
				opt("author", "Author", "json", "The commit's author {name, email}; the connection's when empty"),
				opt("key", "Key", "string", "The idempotency key; made from the job, this node and the changes when empty")},
			Out: execs(
				cognition.DataPin("sha", "Commit", "string", "The new commit (the head when nothing changed)"),
				cognition.DataPin("branch", "Branch", "string", "The branch committed to"),
				cognition.DataPin("before", "Before", "string", "The branch's head before; empty for a new branch"),
				cognition.DataPin("created", "Created", "bool", "True when the branch was made"),
				cognition.DataPin("unchanged", "Unchanged", "bool", "True when the changes changed nothing, so no commit was made"),
				cognition.DataPin("url", "URL", "string", "The commit's page at the host, when it has one")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			c := CommitInput{Connection: str(in, "connection"), Branch: str(in, "branch"), Message: str(in, "message"),
				Base: str(in, "base"), ExpectHead: str(in, "expectHead"), Key: str(in, "key")}
			b, _ := json.Marshal(in["changes"])
			if err := json.Unmarshal(b, &c.Changes); err != nil {
				return outcome(connectors.Errorf(connectors.CodeInvalid, "Changes must be a list of {path, content | file | delete}"))
			}
			if a, ok := in["author"]; ok && a != nil {
				b, _ := json.Marshal(a)
				_ = json.Unmarshal(b, &c.Author)
			}
			if c.Key == "" {
				c.Key = Key(x.ServiceArgs, "node:"+x.ServiceArgs.Meta.NodeID, []any{c.Connection, c.Branch, c.Message, c.Changes})
			}
			res, e := deps().Commit(x.ServiceArgs, c)
			if e != nil {
				return outcome(e)
			}
			return cognition.Values{"sha": res.SHA, "branch": res.Branch, "before": res.Before, "created": res.Created, "unchanged": res.Unchanged, "url": res.URL}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodePullRequest, Source: source, Order: 963}, Title: "Open pull request", Category: "Git",
		Effects: true, External: true,
		Description: "Opens a pull request from a branch into another (the default branch when Base is empty) through a git connection whose type has pull requests (git.github, the Git test server); an open pull request from that head into that base is given instead of a second. Keyed by the job, this node and its head and base. A typed error (notSupported for a plain git host) leaves by Error.",
		Example:     "Open pull request from docs/git-post into main titled Publish the git post, after Commit files",
		Tags:        []string{"git", "code", "review", "pull request"},
		Sample:      cognition.Values{"number": 1, "url": "", "state": "open", "pullRequest": map[string]any{"number": 1, "state": "open", "title": "Publish", "head": "docs/post", "base": "main"}},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), connectionPin(),
				req("head", "Head", "string", "The branch with the changes"),
				req("title", "Title", "string", "The pull request's title"),
				opt("base", "Base", "string", "The branch to merge into; the default branch when empty"),
				opt("body", "Body", "string", "The description, in Markdown"),
				opt("draft", "Draft", "bool", "Open it as a draft")},
			Out: execs(
				cognition.DataPin("number", "Number", "number", "The pull request's number"),
				cognition.DataPin("url", "URL", "string", "Its page at the host"),
				cognition.DataPin("state", "State", "string", "open, closed or merged"),
				cognition.DataPin("pullRequest", "Pull request", "json", "The whole pull request {number, url, state, draft, title, body, head, base, headSha, updatedAt}")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			p := PullRequestInput{Connection: str(in, "connection"), Head: str(in, "head"), Base: str(in, "base"), Title: str(in, "title"), Body: str(in, "body"), Draft: flag(in, "draft")}
			p.Key = Key(x.ServiceArgs, "node:"+x.ServiceArgs.Meta.NodeID, []any{p.Connection, p.Head, p.Base})
			pr, e := deps().OpenPullRequest(x.ServiceArgs, p)
			if e != nil {
				return outcome(e)
			}
			return cognition.Values{"number": pr.Number, "url": pr.URL, "state": pr.State, "pullRequest": map[string]any(asValues(pr))}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: NodeMirror, Source: source, Order: 964}, Title: "Sync repository mirror", Category: "Git",
		Effects: true, External: true,
		Description: "Brings a mirrored Volume (a read-only Volume under mirrors/ that only this node writes) to a branch's head: a git repository's folder copied as files, which the index then chunks and searches, so kb.search and agents cite them by path and line. With From (the commit last synced) only what changed since is copied and deleted files removed; without it everything is copied and the Volume's other files removed. Skips files over 256 KB, dotted names and vendored folders (Ignore). Makes the Volume on first use; runs only in the default tenant, as an app or background work, never as a person. Give it the last synced commit and keep its Commit output for the next run.",
		Example:     "Sync repository mirror of the site's connection at main, path www/content, into Volume code-and-castles, from the mirror record's last synced commit, in the job a push enqueues",
		Tags:        []string{"git", "mirror", "volume", "index", "search"},
		Sample:      cognition.Values{"sha": sha, "volumeId": "1", "written": 3, "removed": 0, "skipped": 0, "full": true},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), connectionPin(),
				req("ref", "Ref", "string", "The branch to mirror"),
				req("volume", "Volume", "string", "The mirrored Volume's folder name under mirrors/ (lower-case letters, digits, - and _), made on first use"),
				opt("path", "Path", "string", "The repository folder to mirror; the whole repository when empty"),
				opt("from", "From", "string", "The commit last synced; everything is copied when empty"),
				opt("ignore", "Ignore", "json", "Folder names to skip anywhere in the tree; node_modules, vendor, dist, build, public, resources, target and out when empty")},
			Out: execs(
				cognition.DataPin("sha", "Commit", "string", "The commit now mirrored: keep it as the next run's From"),
				cognition.DataPin("volumeId", "Volume id", "string", "The mirrored Volume's id"),
				cognition.DataPin("written", "Written", "number", "Files written"),
				cognition.DataPin("removed", "Removed", "number", "Files removed"),
				cognition.DataPin("skipped", "Skipped", "number", "Files skipped (too large, dotted, or in an ignored folder)"),
				cognition.DataPin("full", "Full", "bool", "True when everything was copied")),
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			s := SyncInput{Connection: str(in, "connection"), Ref: str(in, "ref"), Path: str(in, "path"), Volume: str(in, "volume"), From: str(in, "from")}
			if v, ok := in["ignore"]; ok && v != nil {
				b, _ := json.Marshal(v)
				_ = json.Unmarshal(b, &s.Ignore)
			}
			res, e := deps().Sync(x.ServiceArgs, s)
			if e != nil {
				return outcome(e)
			}
			return asValues(res), cognition.Then, nil
		},
	})
}
