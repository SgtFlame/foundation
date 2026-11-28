# Docs

The docs library app: pages, each a document file in a Volume, in a tree per Volume. Forge's iteration 35 builds it in parts; the editor itself is the rich-text editor of [MarkdownEditor.md](MarkdownEditor.md) (35b), and the plans are forge's `documentation/plans/035-Docs.md` and its parts.

## Built in forge 35a-2

**Where**: `backend/features/docs` (the Go side: nodes and the save hook), `backend/features/docs/app` (`docsapp`: the library app as JSON under `files/`, laid out as a sample app, with `Files` and `Document()`), `backend/features/wiring/docs.go`. Forge's starter seeds it into every tenant beside the calendar app (`starter.LoadApps`).

### Pages and files
- A page is a `docsPage` record and a **flat file**: its document is `pages/<pageId>.doc.json` in its Volume (35a-1's document file). The tree lives only in the records, so moving a page never renames a file and two people moving pages never race on the file system (35a-2 question 1). Plain files elsewhere in the Volume stay as they are.
- `docsPage` fields: `title`, `icon` (an emoji) and `iconFile`, `cover`, `volume` (the Volume's id), `file` (`volumeFile`), `parent` (a page id, as text: a rank's scope must have an order, which a relation has not), `ancestors` (the ids above it, the top first), `childCount` (subpages outside the trash, for the tree's expand arrow), `rank` (scoped by `parent`), `template`, `database` (35c-3), `trashedAt`, `trashedWith`, `lastSavedAt`, `hash`. It is stored with `projection: sql` and indexed by volume, parent and rank, by ancestor, by trash and by `trashedWith`.
- `docsFavourite` (`user`, `page`, `rank` scoped by user) is each person's own by its row rules.

### Commands, tools and queries (graphs)
- `docs.page.add` (command, `cmd-docs-page-add`): the record of a page whose file was just made, at the end of its parent's subpages (*Rank between* after the last), with its ancestors, counted on its parent, in one batch. It checks the file is the page's own.
- `createPage` (tool, `fn-docs-createPage`): *Create page file* (empty, or a copy of a template page's document, whose title it also takes), then *Send command* `docs.page.add`; when the record is refused the file is removed again (*Remove page file*). A command decides purely and cannot write files, so making a page is a function graph, one request for the caller.
- `docs.page.move` (`cmd-docs-page-move`): *Move record* (`records.move`, new in Foundation's record nodes: `record.move` inside a command graph) sets parent, ancestors and rank; every subpage's ancestors follow and both parents' counts change, in one batch. A new parent that is the page or one of its subpages (in its `ancestors`) is refused. **Two moves at once cannot make a cycle**: a move that changes parent writes the new parent's count with its expected sequence, and a move of any of that parent's ancestors rewrites the parent's ancestors, so the two batches share a target; the later conflicts, is decided again and refused (`TestConcurrentMoves`). A subtree past about 1 000 pages is refused as one batch too large (`project.MaxBatch`).
- `docs.page.trash` / `docs.page.restore`: the page and its subpages not yet trashed get `trashedAt` and `trashedWith` (the page trashed) in one batch, and leave their parent's count; restoring brings back those trashed with it, to where it was, or to the top of its Volume (*Move record*, ancestors cut) when its parent is in the trash or gone.
- `purgePage` (tool, `fn-docs-purgePage`): deletes a trashed page and those trashed with it for good, file then record. The `onPurge` trigger (`docs-onPurge`, daily, as the app) does the same for pages trashed longer than the app setting `trashDays` (30); a file already gone is no error, so a run cut short is finished by the next.
- `page.saved`: `docs.FollowSaves` (the feature's `Start`) watches the Volumes' file events and, for a written `pages/<id>.doc.json`, sends `record.update {lastSavedAt, hash}` (SHA-256 of the file) as the local tenant's service principal; a file of no page is refused quietly.
- Queries: `docs.tree {volume, parent?}` (one level in rank order, outside the trash; lazy, a subscription per open node), `docs.favourites` (the caller's, with each page's title and icon, leaving out trashed or deleted pages), `docs.trash {volume}` (the pages trashed themselves, newest first, each with how many went with it).

### Access
A Volume's grants are the floor: *May use Volume* (`docs.mayUseVolume`, pure, allowed in commands and queries) checks `volume.read`/`volume.write` on `tenant:<t>/volume:<id>`; every page command refuses without write and the queries give nothing without read. *Create page file* and *Remove page file* check the same as the run's principal through the volume service, and allow the docs app's own principal (the purge trigger). The app's roles (`admin`, the installer's; `user`) hold the record actions; per-page sharing is 35a-3's. Raw `record.*` commands on `docsPage` are still allowed to holders of those roles and do not keep `ancestors` or `childCount`: change pages through the commands above.

### Wiring
- `wiring.Feature.Start(ctx, env)` (new): background work started by `wiring.StartAll` once serve's services are made (foundation's and forge's `serve.go` call it after `connections.Serve`). `wiring.Env` gained `KB` (`kbtools.Deps`: the knowledge base root and services) and `FileEvents`.
- `volumeimpl.New` makes a volume service that is not the singleton, for tests with their own root.

### Tests
`go test ./features/docs/...`: `TestVolumeFloor` (a policy installed: a Volume viewer reads but does not change its pages, no grant neither, the docs app both), and over embedded NATS and SQLite with the app imported and installed (`docs/app/stack_test.go`): `TestCreatePages` (file and record in one request, order, counts, a template copied, a refused record's file removed), `TestMove` (subtree, counts, order, refusals into its own subtree), `TestConcurrentMoves`, `TestTrashAndRestore` (subtree, counts, the trash query, restore home and to the top, purge), `TestSaved`, `TestManyPages` (1 000 pages listed per parent within two seconds).
