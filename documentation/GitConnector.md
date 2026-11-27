# Git connector

Repositories as connector contracts (forge iteration 24, planned in Forge's `documentation/plans/024-GitConnector.md`): reading a repository's refs, trees and files, committing changes to a branch, opening pull requests, and noticing pushes. Docs publishes to Hugo sites and `docs/` folders through it, and agents read and change code through it. Everything at runtime is in `backend/features/connectors/git`; Forge holds the Git test server's page and `forge gittest`.

## Built in forge 24a: the contracts and the Git test server

### `git.repo@1`

`git.RepoContract()`. Every operation is `read` except `commit` (`write`, `destructive`, keyed by `key`).

| Operation | Args | Result |
|---|---|---|
| `check` | — | `{ok, defaultBranch, canWrite}` |
| `listRefs` | `{prefix?}` | `{refs: [{name, sha}], defaultBranch}`, names full (`refs/heads/main`) |
| `readTree` | `{ref, path?, recursive?, cursor?, limit?}` | `{sha, entries: [{path, kind: file\|dir, size, sha}], next}`: a page (at most 1000) sorted by path; `next` is the last path, the cursor of the next page |
| `readFile` | `{ref, path}` | `{path, sha, size, encoding: text\|base64, content}`, or `{…, file}` over 1 MB: stored in the tenant's file store **as the caller** |
| `compare` | `{base, head}` | `{base, head, changes: [{path, status: added\|modified\|deleted}]}` |
| `commit` | `{branch, base?, expectHead?, message, author?, changes: [{path, content \| file \| delete, encoding?}], key?}` | `{sha, branch, before, created, unchanged, url}` |

A ref is a branch name, a full ref or a commit sha. Paths are from the repository's root (`git.CleanPath`: no leading `/`, no `.`/`..` segments, never `.git`), branch names follow `git check-ref-format`'s main rules (`git.BranchName`).

**Commit** writes every change as one commit. A missing branch is created from `base` (the default branch when left out). `expectHead` refuses with `conflict` when the branch moved. Changes that leave every file as it was make no commit (`unchanged`, `sha` the head). A change's `file` is a file of the tenant's file store the caller may use, by the record commands' rule (`git.ReadFile`: one it uploaded, any for a service principal, one the dispatcher vouches for through `heldBy`); to anyone else it does not exist (`notFound`). Errors: `conflict`, `notFound`, `invalid` (a bad path or branch, a protected branch: never `auth`, which would mark the connection failed), `auth`, `rateLimited`, `unavailable`.

**Idempotency** is double. The ledger (forge 07c) answers a retried call with the same key from any process; and a type also adds the key to the commit message as a trailer, `Axiom-Key: <key>` (`git.WithKey`), and looks for it in the branch's last 50 commits (`git.HasKey`, `git.KeyDepth`) before committing, so a crash between the push and the ledger write still makes one commit.

**Event** `pushed {ref, before, after, paths}` (`git.Pushed`), ids `ref|after`, delivery `watch` and `callback`: a type polls under a leased `Watch` now; the hook endpoint comes with 26b-2. Every move of a branch is a push, Axiom's own commits too (a host polled by heads cannot tell them apart; the mirror of 24e needs them).

### `git.review@1`

`git.ReviewContract()`: `openPullRequest {head, base?, title, body?, draft?, key?}` (keyed; an open pull request from head into base is given instead of a second), `getPullRequest {number}`, `listPullRequests {state?: open|closed|merged|all, head?, base?}` (newest first), and the event `pullRequestChanged` (the pull request, ids `number|state|headSha|updatedAt`). GitHub implements it; plain git hosts do not.

`git.Contribute(reg)` registers both contracts (source `git`); the types register themselves. Shared helpers for the types: `git.Decode` (a commit's args checked and its changes read), `git.FileResult` (readFile's result, inline or stored), `git.Diff`, `git.List` (a readTree page from a snapshot) and the result types.

### The Git test server

`gittest.repo` (`git/gittest`) implements `git.repo@1`, `git.review@1` and its own `gittest.control@1` over a repository in memory per connection (the `infratest` pattern): a fresh repository has one commit, *Initial commit* with `README.md`, on its default branch (the connection's `defaultBranch`, `main`). Blob shas are git's; commit shas are made the same way from the tree, parents, author, time and message. Its state lives in the process that serves it (`gittest.Default`, served by `serve.go`'s in-process adapter, given the record files' store with `SetFiles`): give it one adapter instance.

`gittest.control@1`:
- `push {branch?, message?, changes?}` commits as someone outside (`pushes/<n>.md` without changes), raising `pushed`;
- `fail {operation, kind?, times?}` fails the next calls of a `git.repo` or `git.review` operation;
- `protect {branch, protected?}` refuses commits to a branch (`invalid`);
- `merge {number}` merges an open pull request: a merge commit on the base holding the head's changes since it forked, raising `pullRequestChanged` and `pushed`;
- `log {branch?, limit?}` gives the branches, the branch's commits newest first with the paths each changed, the pull requests and the pending faults.

`gittest.ContributeLive` adds the call `gittest.control {connection, operation, args}` (one control operation through the dispatcher, as the caller; one `result` event) and the query `gittest.log {connection, branch?, limit?}` (the log, polled each second and sent again when it changes), for Forge's page and `forge gittest`.

### The suite

`git/contracttest.Run(t, call, Options{Prefix, Push, Watch, Review, Run})` is what every `git.repo@1` type passes: a commit creates a branch and `readFile` gives back text and binary content; a path outside the repository is `invalid`; `readTree` lists children, recursively, and one entry per page; `compare` names what a second commit modified and deleted; a stale `expectHead` is `conflict`; a retried keyed commit makes one commit; `listRefs` finds the branch; changes that change nothing make no commit; with `Push` and `Watch`, an outside push arrives once as `pushed` with its paths; with `Review`, `openPullRequest` twice opens one, and it reads and lists. `Run` names a run's branches apart on a real host. `go test ./features/connectors/git/...` runs it on the test server.
