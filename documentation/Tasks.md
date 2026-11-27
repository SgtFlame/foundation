# Tasks

Tasks and boards, forge's iteration 30 (`forge/documentation/plans/030-Tasks.md`). Foundation holds the backend: `features/tasks` (the `tasks` library app, from part 30c), the `tasks.tracker@1` contract, its suite, the Tasks test server and the tracker connector types. Ordered positions (part 30a, the `rank` field type and `record.move`) are the records' own: [Records.md](Records.md) › *Built in forge 30a*.

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
