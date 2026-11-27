// Package git is the git connector (forge iteration 24,
// documentation/GitConnector.md): repositories as connector contracts.
// git.repo@1 reads a repository's refs, trees and files, compares two
// commits, commits changes to a branch and reports pushes; git.review@1
// opens and reads pull requests where the host has them. The types that
// implement them are gittest.repo (the Git test server, package gittest),
// git.github (24b) and git.https (24c); the suite every type passes is
// package contracttest.
package git

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The contracts' ids and references.
const (
	RepoID     = "git.repo"
	RepoRef    = "git.repo@1"
	ReviewID   = "git.review"
	ReviewRef  = "git.review@1"
	KeyID      = "git.key"
	KeyRef     = "git.key@1"
	EventPush  = "pushed"
	EventPR    = "pullRequestChanged"
	Source     = "git"
	MaxInline  = 1 << 20 // readFile gives content inline up to this many bytes
	MaxPage    = 1000    // readTree's largest page
	KeyTrailer = "Axiom-Key"
)

var tags = []string{"git", "code", "repository"}

func j(s string) json.RawMessage { return json.RawMessage(s) }

// fileRef is a file reference into the tenant's file store; heldBy lets a
// caller that did not upload it use it when it may read the record.
const fileRef = `{"type":"object","description":"%s","properties":{
  "id":{"type":"string","description":"The file's id in the tenant's file store"},
  "name":{"type":"string","description":"The file's name"},
  "size":{"type":"integer","description":"Its size in bytes"},
  "contentType":{"type":"string","description":"Its media type"},
  "heldBy":{"type":"object","description":"The record holding the file, for a caller that did not upload it but may read the record","properties":{
    "entity":{"type":"string","description":"The entity type"},
    "record":{"type":"string","description":"The record's id"},
    "field":{"type":"string","description":"The file field holding it"}}}},"required":["id"]}`

const author = `{"type":"object","description":"Who the commit is by; the connection's account when left out","properties":{
  "name":{"type":"string","description":"The author's name"},
  "email":{"type":"string","description":"The author's email address"}}}`

const commitResult = `{"type":"object","properties":{
  "sha":{"type":"string","description":"The branch's head after the call: the new commit, or the head as it was when nothing changed"},
  "branch":{"type":"string","description":"The branch committed to"},
  "before":{"type":"string","description":"The branch's head before the commit; empty when the commit created the branch"},
  "created":{"type":"boolean","description":"True when the commit created the branch"},
  "unchanged":{"type":"boolean","description":"True when the changes left every file as it was, so no commit was made"},
  "url":{"type":"string","description":"The commit's page at the host, when it has one"}},"required":["sha","branch"]}`

const pullRequest = `{"type":"object","properties":{
  "number":{"type":"integer","description":"The pull request's number"},
  "url":{"type":"string","description":"Its page at the host"},
  "state":{"type":"string","enum":["open","closed","merged"],"description":"open, closed or merged"},
  "draft":{"type":"boolean","description":"True while it is a draft"},
  "title":{"type":"string","description":"Its title"},
  "body":{"type":"string","description":"Its description, Markdown"},
  "head":{"type":"string","description":"The branch it merges from"},
  "base":{"type":"string","description":"The branch it merges into"},
  "headSha":{"type":"string","description":"The head branch's commit it was last seen at"},
  "updatedAt":{"type":"string","description":"When it last changed, RFC 3339"}},"required":["number","state","head","base","title"]}`

// examplePR is a pull request as the examples show it.
const examplePR = `{"number":7,"url":"https://github.com/acme/site/pull/7","state":"open","draft":true,"title":"Publish: Castles in the air","body":"A new post from Docs.","head":"docs/castles-in-the-air","base":"main","headSha":"5b1f0c2e9d8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c","updatedAt":"2026-11-27T18:04:11Z"}`

// RepoContract is git.repo@1: a repository's refs, trees and files, and
// commits to its branches.
func RepoContract() connectors.Contract {
	ref := `"ref":{"type":"string","description":"A branch name (main), a full ref (refs/heads/main, refs/tags/v1) or a commit sha"}`
	return connectors.Contract{
		ID: RepoID, Version: 1,
		Meta: connectors.Meta{Title: "Git repository", Description: "A git repository at a host: list its branches and tags, read its tree and files at any ref, compare two commits, and commit changes to a branch (creating the branch from a base when it is missing). Reports each push to a branch. Use it to read code or documentation, or to publish files (a Hugo page bundle, docs/ Markdown) as a commit on a branch that a pull request then merges.", Tags: tags},
		Operations: []connectors.Operation{
			{
				ID: "check", Title: "Check the connection", Description: "Checks that the connection reaches the repository with its credentials, and gives the repository's default branch and whether the connection allows committing to it.",
				Effect: connectors.EffectRead,
				Args:   j(`{"type":"object","properties":{}}`),
				Result: j(`{"type":"object","properties":{
  "ok":{"type":"boolean","description":"True when the repository answered"},
  "defaultBranch":{"type":"string","description":"The repository's default branch"},
  "canWrite":{"type":"boolean","description":"True when the credentials may push"},
  "allowDefaultBranch":{"type":"boolean","description":"True when the connection lets the git tools and nodes commit straight to the default branch (its allowDefaultBranch setting); they commit to other branches otherwise"}},"required":["ok"]}`),
				Examples: []connectors.Example{{Args: j(`{}`), Result: j(`{"ok":true,"defaultBranch":"main","canWrite":true,"allowDefaultBranch":false}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeNotFound, connectors.CodeUnavailable},
			},
			{
				ID: "listRefs", Title: "List branches and tags", Description: "Lists the repository's refs (branches under refs/heads/, tags under refs/tags/) with the commit each points at, and names the default branch.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{
  "prefix":{"type":"string","description":"Only refs starting with this, such as refs/heads/docs/; every ref when left out"}}}`),
				Result: j(`{"type":"object","properties":{
  "refs":{"type":"array","description":"The refs, sorted by name","items":{"type":"object","properties":{
    "name":{"type":"string","description":"The full ref, refs/heads/main"},
    "sha":{"type":"string","description":"The commit it points at"}},"required":["name","sha"]}},
  "defaultBranch":{"type":"string","description":"The default branch's name"}},"required":["refs"]}`),
				Examples: []connectors.Example{{Args: j(`{"prefix":"refs/heads/"}`), Result: j(`{"refs":[{"name":"refs/heads/main","sha":"9fceb02d0ae598e95dc970b74767f19372d61af8"}],"defaultBranch":"main"}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
			{
				ID: "readTree", Title: "Read a tree", Description: "Lists the files and folders under a path at a ref, a page at a time (next gives the cursor for the following page). recursive lists every file below the path, not only its children.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{` + ref + `,
  "path":{"type":"string","description":"A folder, relative to the repository's root without a leading /; the root when left out"},
  "recursive":{"type":"boolean","description":"List every file below the path; only its children when left out"},
  "cursor":{"type":"string","description":"The next of the previous page"},
  "limit":{"type":"integer","minimum":1,"maximum":1000,"description":"The most entries in the page; 1000 when left out"}},"required":["ref"]}`),
				Result: j(`{"type":"object","properties":{
  "sha":{"type":"string","description":"The commit the ref resolved to"},
  "entries":{"type":"array","description":"The entries, sorted by path","items":{"type":"object","properties":{
    "path":{"type":"string","description":"The path from the repository's root"},
    "kind":{"type":"string","enum":["file","dir"],"description":"file or dir"},
    "size":{"type":"integer","description":"A file's size in bytes"},
    "sha":{"type":"string","description":"A file's blob sha"}},"required":["path","kind"]}},
  "next":{"type":"string","description":"The cursor of the next page; empty on the last"}},"required":["sha","entries"]}`),
				Examples: []connectors.Example{{Args: j(`{"ref":"main","path":"www/content/code"}`), Result: j(`{"sha":"9fceb02d0ae598e95dc970b74767f19372d61af8","entries":[{"path":"www/content/code/_index.md","kind":"file","size":212,"sha":"e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"},{"path":"www/content/code/20260924-lxd-vs-docker","kind":"dir"}]}`)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeAuth, connectors.CodeUnavailable},
			},
			{
				ID: "readFile", Title: "Read a file", Description: "Reads one file at a ref. Text up to 1 MB comes inline (base64 for binary content); a larger file is stored in the tenant's file store as the caller and given as a file reference.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{` + ref + `,
  "path":{"type":"string","description":"The file's path from the repository's root"}},"required":["ref","path"]}`),
				Result: j(`{"type":"object","properties":{
  "path":{"type":"string","description":"The file's path"},
  "sha":{"type":"string","description":"Its blob sha"},
  "size":{"type":"integer","description":"Its size in bytes"},
  "encoding":{"type":"string","enum":["text","base64"],"description":"How content is written: text for UTF-8, base64 otherwise"},
  "content":{"type":"string","description":"The content, when it is at most 1 MB"},
  "file":` + sprintf(fileRef, "The content in the tenant's file store, when it is over 1 MB") + `},"required":["path","sha","size"]}`),
				Examples: []connectors.Example{{Args: j(`{"ref":"main","path":"www/hugo.toml"}`), Result: j(`{"path":"www/hugo.toml","sha":"3b18e512dba79e4c8300dd08aeb37f8e728b8dad","size":44,"encoding":"text","content":"baseURL = \"https://codeandcastles.com/\"\n"}`)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeAuth, connectors.CodeUnavailable},
			},
			{
				ID: "compare", Title: "Compare two commits", Description: "Lists the paths that differ between two refs or commits (base to head), each added, modified or deleted.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{
  "base":{"type":"string","description":"The ref or commit to compare from"},
  "head":{"type":"string","description":"The ref or commit to compare to"}},"required":["base","head"]}`),
				Result: j(`{"type":"object","properties":{
  "base":{"type":"string","description":"The commit base resolved to"},
  "head":{"type":"string","description":"The commit head resolved to"},
  "changes":{"type":"array","description":"The changed paths, sorted","items":{"type":"object","properties":{
    "path":{"type":"string","description":"The file's path"},
    "status":{"type":"string","enum":["added","modified","deleted"],"description":"added, modified or deleted"}},"required":["path","status"]}}},"required":["base","head","changes"]}`),
				Examples: []connectors.Example{{Args: j(`{"base":"main","head":"docs/castles-in-the-air"}`), Result: j(`{"base":"9fceb02d0ae598e95dc970b74767f19372d61af8","head":"5b1f0c2e9d8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c","changes":[{"path":"www/content/code/20261127-castles/index.md","status":"added"}]}`)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeAuth, connectors.CodeUnavailable},
			},
			{
				ID: "commit", Title: "Commit changes", Description: "Commits changes to files on a branch as one commit: each change writes a file's content (inline, or a file from the tenant's file store) or deletes it. A missing branch is created from base (the default branch when left out). expectHead refuses with conflict when the branch has moved since it was read. Changes that leave every file as it was make no commit. Calls with the same key make one commit.",
				Effect: connectors.EffectWrite, Destructive: true, Key: "key",
				Args: j(`{"type":"object","properties":{
  "branch":{"type":"string","description":"The branch to commit to, without refs/heads/"},
  "base":{"type":"string","description":"The ref or commit to create the branch from when it is missing; the default branch when left out"},
  "expectHead":{"type":"string","description":"The commit the branch must be at; conflict when it moved"},
  "message":{"type":"string","description":"The commit message"},
  "author":` + author + `,
  "changes":{"type":"array","description":"The files to write or delete","items":{"type":"object","properties":{
    "path":{"type":"string","description":"The file's path from the repository's root"},
    "content":{"type":"string","description":"The new content; give this, file or delete"},
    "encoding":{"type":"string","enum":["text","base64"],"description":"How content is written; text when left out"},
    "file":` + sprintf(fileRef, "The new content from the tenant's file store; give this, content or delete") + `,
    "delete":{"type":"boolean","description":"Delete the file"}},"required":["path"]}},
  "key":{"type":"string","description":"An idempotency key, such as the command or run id: calls with the same key make one commit"}},"required":["branch","message","changes"]}`),
				Result: j(commitResult),
				Examples: []connectors.Example{{
					Args:   j(`{"branch":"docs/castles-in-the-air","base":"main","message":"Publish: Castles in the air","changes":[{"path":"www/content/code/20261127-castles/index.md","content":"---\ntitle: Castles in the air\ndraft: false\n---\n\nHello.\n"}],"key":"run-7340031"}`),
					Result: j(`{"sha":"5b1f0c2e9d8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c","branch":"docs/castles-in-the-air","before":"","created":true}`),
				}},
				Errors: []string{connectors.CodeConflict, connectors.CodeNotFound, connectors.CodeInvalid, connectors.CodeAuth, connectors.CodeRateLimited, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{{
			ID: EventPush, Title: "Pushed", Description: "A branch moved: someone pushed to it, a pull request merged into it, or it was created. paths lists the files that changed.",
			Payload: j(`{"type":"object","properties":{
  "ref":{"type":"string","description":"The full ref that moved, refs/heads/main"},
  "before":{"type":"string","description":"Its commit before; empty when the push created it"},
  "after":{"type":"string","description":"Its commit after"},
  "paths":{"type":"array","items":{"type":"string"},"description":"The paths that changed, sorted"}},"required":["ref","after"]}`),
			Example:  j(`{"ref":"refs/heads/main","before":"9fceb02d0ae598e95dc970b74767f19372d61af8","after":"5b1f0c2e9d8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c","paths":["www/content/code/20261127-castles/index.md"]}`),
			IDFields: []string{"ref", "after"},
			Delivery: []string{connectors.DeliveryWatch, connectors.DeliveryCallback},
		}},
	}
}

// ReviewContract is git.review@1: pull requests, where the host has them.
func ReviewContract() connectors.Contract {
	return connectors.Contract{
		ID: ReviewID, Version: 1,
		Meta: connectors.Meta{Title: "Git pull requests", Description: "Pull requests at a git host that has them (GitHub): open one from a branch, read one, list them. Use it after committing to a branch with git.repo commit, so a person reviews and merges the change.", Tags: append([]string{"review"}, tags...)},
		Operations: []connectors.Operation{
			{
				ID: "openPullRequest", Title: "Open a pull request", Description: "Opens a pull request from head into base. When one is already open from head into base it is given instead, so a retry opens one.",
				Effect: connectors.EffectWrite, Key: "key",
				Args: j(`{"type":"object","properties":{
  "head":{"type":"string","description":"The branch with the changes"},
  "base":{"type":"string","description":"The branch to merge into; the default branch when left out"},
  "title":{"type":"string","description":"The pull request's title"},
  "body":{"type":"string","description":"Its description, Markdown"},
  "draft":{"type":"boolean","description":"Open it as a draft"},
  "key":{"type":"string","description":"An idempotency key, such as the command or run id"}},"required":["head","title"]}`),
				Result:   j(pullRequest),
				Examples: []connectors.Example{{Args: j(`{"head":"docs/castles-in-the-air","base":"main","title":"Publish: Castles in the air","body":"A new post from Docs.","draft":true}`), Result: j(examplePR)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeInvalid, connectors.CodeAuth, connectors.CodeRateLimited, connectors.CodeUnavailable},
			},
			{
				ID: "getPullRequest", Title: "Read a pull request", Description: "Reads one pull request by its number.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{
  "number":{"type":"integer","description":"The pull request's number"}},"required":["number"]}`),
				Result:   j(pullRequest),
				Examples: []connectors.Example{{Args: j(`{"number":7}`), Result: j(examplePR)}},
				Errors:   []string{connectors.CodeNotFound, connectors.CodeAuth, connectors.CodeUnavailable},
			},
			{
				ID: "listPullRequests", Title: "List pull requests", Description: "Lists pull requests, newest first: open ones when state is left out.",
				Effect: connectors.EffectRead,
				Args: j(`{"type":"object","properties":{
  "state":{"type":"string","enum":["open","closed","merged","all"],"description":"Which ones; open when left out"},
  "head":{"type":"string","description":"Only those from this branch"},
  "base":{"type":"string","description":"Only those into this branch"}}}`),
				Result: j(`{"type":"object","properties":{
  "pullRequests":{"type":"array","description":"The pull requests, newest first","items":` + pullRequest + `}},"required":["pullRequests"]}`),
				Examples: []connectors.Example{{Args: j(`{"state":"open"}`), Result: j(`{"pullRequests":[` + examplePR + `]}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{{
			ID: EventPR, Title: "Pull request changed", Description: "A pull request was opened, pushed to, marked ready, closed or merged.",
			Payload:  j(pullRequest),
			Example:  j(examplePR),
			IDFields: []string{"number", "state", "headSha", "updatedAt"},
			Delivery: []string{connectors.DeliveryWatch, connectors.DeliveryCallback},
		}},
	}
}

const keyStatus = `{"type":"object","properties":{
  "unlocked":{"type":"boolean","description":"Whether the key is unlocked in this process now"},
  "expiresAt":{"type":"string","format":"date-time","description":"When it locks again, while unlocked"},
  "encrypted":{"type":"boolean","description":"Whether the key needs a passphrase at all; one that does not is always unlocked"}},"required":["unlocked"]}`

// KeyContract is git.key@1 (forge 24f): unlocking a connection's
// passphrase-protected key for a while. The passphrase is never stored:
// unlock is a Secret operation, sent only by connection.secretCall, and
// the type keeps the decrypted key in memory alone.
func KeyContract() connectors.Contract {
	return connectors.Contract{
		ID: KeyID, Version: 1,
		Meta: connectors.Meta{Title: "Git key", Description: "A git connection's private key, protected by a passphrase that is never stored: a person unlocks it for some minutes, and while it is locked every call on the connection fails with auth (detail locked).", Tags: append([]string{"ssh", "key"}, tags...)},
		Operations: []connectors.Operation{
			{
				ID: "unlock", Title: "Unlock the key", Description: "Opens the connection's key with its passphrase and keeps it open in memory for minutes (15 when left out, at most 60). Only a person's form sends it; a graph or tool cannot.",
				Effect: connectors.EffectWrite, Secret: true,
				Args: j(`{"type":"object","properties":{
  "passphrase":{"type":"string","description":"The key's passphrase"},
  "minutes":{"type":"integer","minimum":1,"maximum":60,"description":"How long it stays unlocked; 15 when left out"}},"required":["passphrase"]}`),
				Result:   j(keyStatus),
				Examples: []connectors.Example{{Args: j(`{"passphrase":"correct horse battery staple","minutes":15}`), Result: j(`{"unlocked":true,"expiresAt":"2026-11-27T18:19:00Z","encrypted":true}`)}},
				Errors:   []string{connectors.CodeInvalid, connectors.CodeUnavailable},
			},
			{
				ID: "lock", Title: "Lock the key", Description: "Forgets the unlocked key at once.",
				Effect: connectors.EffectWrite,
				Args:   j(`{"type":"object","properties":{}}`), Result: j(keyStatus),
				Examples: []connectors.Example{{Args: j(`{}`), Result: j(`{"unlocked":false,"encrypted":true}`)}},
			},
			{
				ID: "keyStatus", Title: "Whether the key is unlocked", Description: "Tells whether the connection's key is unlocked now and until when.",
				Effect: connectors.EffectRead,
				Args:   j(`{"type":"object","properties":{}}`), Result: j(keyStatus),
				Examples: []connectors.Example{{Args: j(`{}`), Result: j(`{"unlocked":true,"expiresAt":"2026-11-27T18:19:00Z","encrypted":true}`)}},
			},
		},
	}
}

// Contracts are git.repo@1, git.review@1 and git.key@1.
func Contracts() []connectors.Contract {
	return []connectors.Contract{RepoContract(), ReviewContract(), KeyContract()}
}

// Contribute registers the git contracts (their types register
// themselves: gittest, git.github, git.https).
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, Source, Contracts(), nil)
}
