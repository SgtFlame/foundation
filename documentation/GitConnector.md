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

## Built in forge 24b: `git.github`

`git/github` implements `git.repo@1` and `git.review@1` against GitHub's REST API (version 2022-11-28), served by `serve.go`'s in-process adapter with the record files' store (`git.ServerFiles`, a `git.LazyFiles` set once the store exists).

**Settings**: `owner`, `repo`, `token` (a secret: a fine-grained personal access token with *Contents* and *Pull requests* read and write on the chosen repositories), `baseUrl` (`https://api.github.com`, or `https://<host>/api/v3` for GitHub Enterprise Server), `pollSeconds` (60, at least 15) and `allowDefaultBranch` (read by 24d's tools and nodes). *Sign in with GitHub* comes with 26a-2.

**Reads**: `check` is `GET /repos/{o}/{r}` (the default branch, `permissions.push` as `canWrite`); `listRefs` pages `git/matching-refs`; a ref resolves through `GET commits/{ref}`; `readTree` reads the commit's tree recursively once and pages it with `git.List` (a truncated tree is `invalid`); `readFile` reads `contents/{path}?ref=`, and the blob (`git/blobs/{sha}`) when GitHub gives no content (over 1 MB); `compare` diffs the two commits' trees exactly, not from their merge base as GitHub's compare endpoint does.

**Commit without cloning** (the Git Data API): read the branch's head (`git/ref/heads/{b}`); with a key, look for it in the head's last 50 commits; check `expectHead`; make a blob only for binary content (text goes inline in the tree); `POST git/trees` on the parent's tree (a `null` sha deletes a path); a tree equal to the parent's is `unchanged`; `POST git/commits`; then `PATCH git/refs/heads/{b}` with `force: false`, or `POST git/refs` for a new branch. A racing push makes that refusal (`not a fast forward`, `already exists`) a `conflict`.

**Pull requests**: `openPullRequest` first lists open ones from `owner:head` into base and gives the first; `state` is `merged` for a closed pull request with `merged_at`; `listPullRequests {state: merged}` lists closed ones and keeps the merged.

**Errors**: 429, or 403 with `X-RateLimit-Remaining: 0` or a rate-limit message, is `rateLimited`, retryable, after `Retry-After` or `X-RateLimit-Reset`; a protected branch is `invalid`; other 401 and 403 are `auth`; 404 `notFound` (GitHub answers 404 for a repository the token cannot see); 409 and the fast-forward refusals `conflict`; other 422 `invalid`; 5xx and no answer `unavailable`, retryable.

**Events** are polled every `pollSeconds`: the branch heads (`matching-refs/heads/`) against the cursor's (a moved or new branch is `pushed`, its paths from comparing the trees; a new branch's from its last commit), and the 30 pull requests updated last against the newest `updated_at` seen (`pullRequestChanged`). The first look only notes where things are; the cursor is stored with an event. Webhooks come with 26b-2.

**Tests**: `githubtest` is a stand-in for the endpoints above over one repository in memory, behind an `httptest` server (token check, outside pushes, protected branches, injected failures, request counts); the suite runs against it, as do the error mapping and the batch shape of a commit. `TestRealRepository` runs the suite against a real repository when `GIT_GITHUB_REPO=owner/repo` and `GIT_GITHUB_TOKEN` are set; its branches start `suite/` and end with the run's time.

## Built in forge 24c: `git.https`

`git/https` implements `git.repo@1` against any host that speaks git's smart HTTP(S) protocol (Gitea, GitLab, Bitbucket, a self-hosted server), through go-git (`github.com/go-git/go-git/v5`, pinned in `go.mod`), entirely in memory: nothing is cloned to disk. Served by `serve.go`'s in-process adapter with the record files' store (`git.ServerFiles`). It has no `git.review@1`: pull requests are each host's own API (use `git.github` on GitHub).

**Settings**: `url` (the HTTPS clone URL; `http://` is refused unless `Options.AllowHTTP`, tests only), `username` (`git` when left out with a token), `token` (a secret: an access token or password), `pollSeconds` (60, at least 15) and `allowDefaultBranch`. HTTPS only: SSH keys are not supported (question 4 of the plan).

**Limits** are the adapter's `Options.Limits` (`MaxBytes` 200 MB of objects one fetch brings, `MaxFiles` 20000 files in a tree), not a configuration section, since adapters never read configuration; a repository over either is `invalid`.

**Reads**: every call lists the remote's refs (`ls-remote`; the default branch from `HEAD`'s symbolic target, else `main` or `master`), then fetches what it needs into a fresh in-memory repository: a branch or tag one commit deep; a commit sha (or a prefix of at least 7) by fetching every branch `git.KeyDepth` (50) deep and finding it there, else `notFound`. `readTree` pages the commit's flattened tree with `git.List`; `readFile` reads the blob; `compare` diffs the two trees exactly. `check`'s `canWrite` is whether a token is set, since only a push tells.

**Commit**: with a key, the branch's last 50 commits are fetched and searched for its `Axiom-Key` trailer; `expectHead` is checked against the advertised head; the parent is fetched one deep, the blobs and nested trees are written in memory (git's entry order, existing file modes kept), and the commit is pushed **without force**, with go-git's `RequireRemoteRefs` naming the head it was built on, so a branch that moved meanwhile is a `conflict` (as is a racing push's `non-fast-forward`). Changes that change nothing are `unchanged`; a protected branch or a declined pre-receive hook is `invalid`.

**Events**: `pushed` by polling the advertised heads every `pollSeconds` (the first look only notes them), each move's paths from comparing the two commits' trees (the new head's parent when the old one is out of reach). Webhooks come with 26b-2.

**Errors**: refused credentials are `auth`; an unknown repository `notFound`; timeouts and every other transport failure `unavailable`, retryable.

**Tests**: the suite runs against a bare repository in `t.TempDir()` served by git's own `git-http-backend` through `net/http/cgi` behind a basic-auth check (skipped where `git-http-backend` is not installed), with outside pushes made by the `git` command; also wrong tokens, plain http refused, a stale head, a commit on a shallow history checked with `git fsck --strict`, and a sha ref. The plan had the suite run over `gittest` served by smart HTTP; git's own server is used instead, which is also what real hosts run.

## Built in forge 24d: nodes and tools

`git/nodes` (`gitnodes`) gives graphs and the chat the repository operations, calling `git.repo@1` and `git.review@1` through the dispatcher **as their caller** (so a connection's `use` and a user's own connections apply as for *Call connector*). `gitnodes.Deps{Call, Repositories}`: `Call` is the dispatcher's `Call`; `Repositories` lists the connections implementing `git.repo@1` the caller may read (`connections.List`, wired in `serve.go`).

**Nodes** (category *Git*; each `Effects` and `External` with a `Sample`, so sandboxed runs stub them; a typed error leaves by *Error* with `error`, a retryable one fails the run so a job retries it):
- *List repository tree* (`git.tree`): connection, ref, path, recursive → entries, sha, truncated (pages gathered, at most `MaxTreeEntries`, 10000).
- *Read repository file* (`git.readFile`): connection, ref, path → content, encoding, file (over 1 MB), sha, size, path.
- *Commit files* (`git.commit`): connection, branch, message, changes (`[{path, content, encoding} | {path, file} | {path, delete}]`), base, expectHead, author, key → sha, branch, before, created, unchanged, url.
- *Open pull request* (`git.pullRequest`): connection, head, title, base, body, draft → number, url, state, pullRequest.

**Tools**: `git.repositories` (read; the connections to pass as `connection`), `git.tree` (read, at most 2000 entries), `git.read` (read, text cut at 100 KB), `git.commit` and `git.pullRequest` (`write`: they ask in the chat; an agent's run is decided by its roles, slice 51).

**Keys**: a write is keyed by `gitnodes.Key`: the job's id inside a job (so it is stable across a job's retries, whose runs differ), else the run and round; the node (`Meta.NodeID`) or tool call id; and what is written (connection, branch, message, changes; for a pull request its head and base), so a loop writing different things writes each once and a retry writes nothing new. *Commit files* takes `key` to override it.

**The default branch** (question 5): before a commit the nodes and tools call `check`, whose result now carries the connection's `allowDefaultBranch` (every type reports its setting; the Git test server has the setting too), and refuse a commit to the default branch without it as `invalid`, naming the way out (another branch and a pull request). Direct *Call connector* calls of `commit` are not refused: the rule is the tools' and nodes'.

**Tests**: `go test ./features/connectors/git/nodes` runs the nodes and tools against the Git test server behind a `Caller` (retried commits by run and by job, the default branch with and without the setting, trees, files, a pull request opened once, a retryable failure failing the run); `apps/tools`' `TestMetadata` covers their metadata.
