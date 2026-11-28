# Tasks

Tasks and boards, forge's iteration 30 (`forge/documentation/plans/030-Tasks.md`). Foundation holds the backend: `features/tasks` (the `tasks` library app, from part 30c), the `tasks.tracker@1` contract, its suite, the Tasks test server and the tracker connector types. Ordered positions (part 30a, the `rank` field type and `record.move`) are the records' own: [Records.md](Records.md) › *Built in forge 30a*.

## Built in forge 30c

The task model: the **`tasks` library app** (forge's 5g sample, once `tasks` too, is `tasksdemo` since then, so the two never meet in a tenant), one *Task* every app puts its to-dos in (Mail, the Nexus, the CRM; Forge's Tasks app of 30e holds the pages and requires it). Its parts are embedded in `features/tasks/app` (`app.json`, `entities/`, `graphs/`) and are blueprints only.

- **Seeding**: `tasks.Document()` is the app as an export document (its node types listed as primitives) and `tasks.SeedApp(args, tenant, store, appService, bus)` imports and installs it in a tenant once, as its service principal, as `controlplane.SeedApp` seeds `infrastructure`. Not yet called from `serve.go` (wired through `features/wiring` once that lands).
- **Entity types** (owner `app:tasks`): *Project* `tasksProject` (`name`, `description` markdown, `key` for task numbers, `admins`/`members`/`guests` users, `defaultView`, `connection` to a `tasks.tracker@1` connection, `archived`); *Status* `tasksStatus` (`project`, `name`, `category` todo/doing/done, `color`, `rank` scoped by `project`); *Task* `taskItem` (`title`, `number` autoNumber, `description` richText, `project`, `status`, `rank` scoped by `status`, `assignees`, `due`, `start`, `priority`, `labels`, `parent`, `attachments`, `source {link, title, kind}`, `state` proposed/active/archived, `proposedBy`, `outside {id, version, updatedAt}` for 30h, `completedAt`, `createdAt`; a large table, `storage.projection: sql`, indexed by project, status and rank, by assignee and due, by state and assignee); *Checklist item* `taskCheck` (`task`, `project`, `text`, `done`, `rank` scoped by `task`); *Comment* `taskComment` (`task`, `project`, `body` markdown, `mentions`, `createdAt`, `author`); *Label* `taskLabel`.
- **Access**: roles *Tasks manager* (installer, everything), *Tasks user* (makes projects and sees those whose `admins`, `members` or `guests` name them, by a row rule on the fields themselves), and *Project admin*, *Member* and *Guest*, each granted **on one project record** by the app; their row rules on the other types are `principal.hasRole('<role>', record.project)`, so a guest sees one project and nothing else. The trigger `onProjectPeople` enqueues the job `tasks-syncAccess` (keyed per project) whenever a project's people change or it is deleted, which grants and revokes to match, as Requirements' grant job does.
- **Commands** (`tasks.<type>` installed): `project.create {name, key?, description?, members?, guests?}` (its maker the admin and a member; the key made from the name), `project.setup` (the app's only, from the trigger `onProject`: To do, Doing, Review, Done with ranks), `task.create {project, title, status?, description?, assignees?, due?, start?, priority?, labels?, parent?, source?}` (last in its status, by *Rank between* after the column's last task; plain text becomes a richText doc; made by an agent whose record's `autonomy` is `suggest` or unset it is `proposed`, with `draft` `active`, and `proposedBy` is `agent:<id>`), `task.update` (only the fields given; a new status moves it there, last), `task.move {status, after?, before?}` (the new node *Move record*; a done status sets `completedAt`, any other clears it; another project's status is refused), `task.complete` (last into the first done status), `task.accept`/`task.dismiss` for proposals.
- **Queries**: `projectTasks {project}` (the project, statuses in order, active tasks in rank order with their `KEY-n` reference, labels and proposals) and `myDay {today?}` (the viewer's unfinished tasks due by today, the others, and the proposals for them).
- **Tools** (manifest `tools`, so chat and agents get them): `tasks.create`, `tasks.update`, `tasks.list` (by project, assignee `me`, state; with the caller's projects and their statuses) and `tasks.comment`. `update` and `comment` keep to an agent run's `reach` (the task itself, or a project it holds). The Agents app's `agentOptions` tool comes with Forge's Tasks app (30e), since the Agents app is Forge's.
- **New node *Move record*** (`records.move`, command graphs): `record.move` decided as *Update record* is, `after`/`before`/`set` pins.
- **Import fix**: an app import that first brings an entity type without its relations (one relating to a type not imported yet) also leaves out a `rank` scoped by one of them, until the second pass (`appservice` `Import.go`).
- **List fields in queries**: `where` on a many relation wants a whole list; filter for one item with `{field, op: "contains", value}` (the graphs here do).
- Test with `go test ./features/tasks/`: seeding twice; Alice (member of *Website relaunch*) and Gus (guest of *Billing*) see only their projects; a move to Done sets `completedAt` and back clears it; removing Alice from the project revokes her grant; `myDay`, `projectTasks` and `tasks.list`; an agent with autonomy `suggest` proposes and `draft` makes a task, a run confined to one task changes no other; `TestTenThousandTasks` filters tasks by assignee in SQL: 1 000 by default, `TASKS_N=10000` for the acceptance (21 ms reading 101 rows; making them takes about five minutes, each create taking the task number counter).

## Changed in forge 30f
For action items from mail (Forge's Mail triage agent proposing tasks):

- *Task* has a new text field `sourceLink`, indexed by `bySource`. `task.create` sets it to `source.link`.
- **An agent proposes one task per source.** A `task.create` sent by an agent whose `source.link` matches a task it can read (in any state, including dismissed, which archives) is refused: *a task from this source was already proposed*. A person may make more than one task from the same source.
- The read nodes now check `record.read` (foundation#32), so the 30c known gap is closed.

## Built in forge 30g

One connector contract for outside task trackers, so the two-way sync of 30h is the same for Trello, Asana, Linear and Jira, and an in-memory test server, so sync is built and tested with no outside account (the pattern of `infra.compute@1` and `infratest`, [Infrastructure.md](Infrastructure.md)).

### `tasks.tracker@1` (`features/tasks/tracker`)

- **Operations**: `board.list {archived?}`, `board.get {id}` (the board with its `lists` in order, `labels` and `members`), `card.list {board, cursor?, limit?, archived?}` (pages of at most `limit`, 100 by default; the pages together hold every card once, the last with an empty `cursor`), `card.get {id}`, `card.create {key, board, list, position?, title, …}` (keyed by our task id: a create with a key already made gives that card), `card.update {id, version?, set}` and `card.move {id, list, position?, version?}` (given `version`, refused with `conflict` once the card moved on), `card.archive {id}`, `comment.list {card}`, `comment.create {key, card, text}` (keyed), `member.list {board}` and `check`.
- **A card** (`tracker.Card`): `id`, `key` (ours, when we made it), `board`, `list`, `title`, `description` (Markdown), `assignees` (member ids), `due`, `start`, `labels` (label ids), `checklist` (`[{id, text, done}]`), `position` (a number sorting its list), `archived`, the tracker's `version` (changed by every change) and `updatedAt` by its clock, `url`. `tracker.CardFields` are the fields `card.update` sets.
- **Events** (delivery `watch`): `card.changed {id, card, board, change: created|updated|moved|archived, version, updatedAt, fields}` and `comment.created {id, card, board, comment, version, createdAt}`; each carries the card's version after the change, so a sync drops the echo of its own change.
- Errors as connectors' (`rateLimited`, `unavailable` retryable; `conflict` for a stale update or move). It passes `connectors.CheckContract`; its tool shapes are golden files in `tasks/tracker/testdata/golden` (`go test ./features/tasks/tracker -update` after a deliberate change).

### The contract suite (`tasks/tracker/trackertest`)

`trackertest.Run(t, call, Options{Board, KeyPrefix, Events})` is what every tracker type passes, against a board with two lists or more: `check`; `board.list` and `board.get`, `notFound` for no board; `card.create` keyed; seven cards listed three to a page, each once; `card.update` against the version shown gives a new version, a stale `card.update` or `card.move` is a `conflict`; `card.move` changes the list; `comment.create` keyed and listed; `card.archive` hides a card from `card.list`; given `Events`, the last event for a card carries its current version.

### The Tasks test server (`tasks/tracker/trackerfake`)

- The builtin type **`tasks.fake`** implements `tasks.tracker@1` over boards and cards kept in memory per tenant and connection (in the process serving the type: give it one adapter instance; `trackerfake.Default`, served by `serve.go`'s `connectorImpls`). Card ids are `c1`, `c2`… in the order made; a seeded board's lists are `<board>-l1`…, labels `<board>-lb1`…, members `<board>-m1`…. Its `Watch` reports every event from a cursor (the last event's sequence).
- **`tasks.fake.control@1`**: `seed {board {id, name, lists, labels, members}, cards, count}` (a board with its cards, `count` more made up as *Card 1*…, spread over the lists; seeding raises no event; 500 for the iteration's acceptance test), `change {card, set, comment?}` (an outside edit of the `card.update` fields plus `list`, `position` and `archived`, with a new version and `card.changed`; a comment raises `comment.created`), `fail {operation, times, kind}`, `slow {operation, ms}` and `state` (boards with card counts, events raised, faults pending).
- The live call **`tasks.fake.control {connection, operation, args}`** runs a control operation on a connection through the dispatcher, as the caller (who must be able to use the connection), ending with one `result` event; Forge's `forge trackertest` uses it.
- Test with `go test ./features/tasks/...`: the suite on the fake with its events, the type and control contract checks, 500 seeded cards listed by pages, an outside change's events and conflict, injected failures, the control call.

## Built in forge 30h
Two-way sync between a project and one board of a `tasks.tracker@1` tracker. It is built and tested on the Tasks test server (`tasks.fake`, 30g). Every part is a blueprint of the `tasks` library app, and runs as the app's principal.

- **Connecting.** A project admin sets the project's `connection` (a tracker connection they may use) and `board` (the outside board's id) with `record.update`. Setting them, or setting `sync.state` to `retry`, starts an import through the trigger `onConnect` (`tasks-onConnect`). The app's own writes never start one. Clearing the connection stops syncing and keeps the tasks.
- **Import** (`tasks-syncImport`, a job keyed `sync-<project>`):
  - Its first run maps the board's open lists to the project's statuses. A status matches a list by its `outsideList`, or else by name. Each list with no match gets a new status, placed after the last one, with a category guessed from its name. The run then marks `sync` `importing`.
  - Each later run imports one page of `card.list` (100 cards) through `tasks.task.create`, passing `outside`. Cards already imported (`outsideId`) are skipped.
  - The last page marks `sync` `ok`, with the count passed from page to page as `imported`.
  - A non-retryable tracker error marks `sync` `failed` with its message. A retryable error fails the run, so the job retries.
- **Push** (`tasks-onTaskChanged` → `tasks-syncPush`, keyed `task-<id>`):
  - It fires when a task of a connected project is made, or its title, description, due date, status or state changes, by anyone but the app. So pulled changes never echo back.
  - A task with no card gets `card.create`, keyed by the task's id, in its status's list.
  - A task with a card gets `card.update` with the version last seen. When its status's list differs from the card's, it also gets `card.move`.
  - The card's new version is kept on the task (`outside`), and the push is skipped when the task's last change was the app's own.
  - A `conflict` enqueues the card's pull. Any other refusal writes a *Sync log* entry.
- **Pull** (`tasks-onCardChanged` on `card.changed` → `tasks-syncPull`, keyed `card-<id>`):
  - An event whose version the task already holds is the echo of our own push, and is dropped.
  - A card with no task, on a board a project of that connection is connected to, becomes a task.
  - Otherwise the job reads the card (`card.get`) and takes its title, description, due date, list (as status) and archiving.
- **Conflicts.** "Changed here since the sync" means the task's last writer is not the app (`_updatedBy` is not 0).
  - When both sides changed, the later change wins (`_updatedAt` against the card's `updatedAt`), field by field.
  - The losing values go to a `taskSyncLog` record: `kind: conflict`, `fields`, `kept` (`ours` or `theirs`), `lost` as JSON, and both times.
  - When ours wins, the task is pushed again with `force`.
- **New fields:**
  - *Project*: `board`, and `sync {state, lastSync, imported, error}`.
  - *Status*: `outsideList`.
  - *Task*: `outsideId` (index `byOutside`), and `list` inside `outside`.
- **New type** *Sync log entry* `taskSyncLog`. Project admins and members read it on their projects. Managers may change it.
- `task.create` takes `outside` from the app's own principal only.
- **Not built yet:**
  - mapping members by email (cards import unassigned);
  - labels, the checklist and comments;
  - removing a status's list;
  - archiving a card when its task is deleted;
  - the browser walk;
  - two runners in two simulated processes;
  - the 500-card import's timing at scale.
- Test with `go test ./features/tasks/ -run TestTwoWaySync`, on the test server:
  - 500 cards import once, and a list with no status gets one;
  - an outside change arrives once and is not pushed back;
  - our change reaches the card once, and its echo is dropped;
  - both sides changed, ours later: ours is kept and pushed back, and theirs logged;
  - both sides changed, theirs later: theirs is kept, ours logged, and the retried push sends nothing;
  - a task made here gets a card in its status's list.

