# Live: the multi-user CQRS core

The live core is what every multi-user editor and application stands on. Objects change only by **commands**, which commit **events** to NATS JetStream. **Projectors** fold the events into KV buckets, and **queries** read those buckets. Every query is a **subscription**: it sends a snapshot, then changes. **Presence** shows who is in an object. A browser window holds **one WebSocket**, `/api/v1/live`, and everything above is multiplexed over it, along with UI sessions ([ServerUI.md](ServerUI.md)), collaboration rooms ([MarkdownEditor.md](MarkdownEditor.md)), chat's streams and configuration's change events.

It was designed in Forge's plans ([forge/documentation/plans/Architecture.md › Multi-user and CQRS](../../forge/documentation/plans/Architecture.md#multi-user-and-cqrs)) and built as Forge's iteration 1.

## Built in slice 13

### Decisions
Answered before building, in [forge 01-ServerUI.md](../../forge/documentation/plans/01-ServerUI.md#todo):
- **A stream per area** (question 5). The streams are `EVT_<tenant>_def` (definitions), `EVT_<tenant>_rec` (records) and `EVT_<tenant>_app_<appId>` (an app's own aggregates). Each is made the first time its area is used.
- **Events are kept forever**, on every stream (question 6). Retention comes once real volumes are known.
- **Yjs rooms ride the live connection** (question 8): a room is a channel on it, and its own WebSocket route is gone.
- **Chat and configuration moved off SSE** (question 9): chat's two streams are live **calls**, and configuration's change events are the `config.changes` query. The Log Explorer's tail stays on SSE.

Decided while building:
- **Sequences are per stream.** A position is a map from stream to sequence (`live.Position`), and an ack names its stream. A subscription resumed with `after` gets a fresh snapshot, not a replay of changes.
- **Commands on one target are decided one at a time in each process.** Before deciding, the bus waits up to 2 seconds for the projection to reflect the target's last commit, so a command reads its predecessor's state. Committing still uses JetStream's *expected last subject sequence* over the target's subjects (`evt.<t>.<area>.<type>.<id>.*`). On a conflict (another instance, or a projection behind after a restart) it waits for the projection to catch up and decides again, three times at most, then fails with `409 conflict`.
- **Duplicates are dropped by command id** (`Nats-Msg-Id`, a 10-minute window). A resent command that was already committed is acknowledged with its original sequence and `duplicate: true`. A resent command whose Decide finds nothing to change commits nothing and is acknowledged with the current sequence.
- **A command's several events are chained, not atomic.** Each expects the previous one's sequence. *Replaced in slice 22* by atomic batches ([Built in slice 22](#built-in-slice-22)).
- **Projections are idempotent.** Each value is stored as `{stream, seq, value}`, and an event whose sequence is not above the stored one is skipped. A deleted value keeps its sequence. Projectors run as durable consumers with `MaxAckPending: 1`, so events fold strictly in order. A projector starts the first time its bucket is used in a tenant.
- **Who is present comes from a query**, `live.presence`, not from a dedicated `presence {object, users}` frame as the plan drew it, so it resumes like every other subscription. Expired keys send no update, so the query also prunes entries older than the time to live.
- **Calls** are the one transport for request-and-stream work: `call {id, call, args}` → `event {id, event, data}`… → `end {id, code}`, and `cancel {id}`. Chat's `ai.chat.stream` and `conversation.send` are calls.
- **Extensions** own their frames. A `live.extension` contribution names frame types and gets each connection's frames of those types. UI sessions (`ui.*`) and collaboration rooms (`collab.*`) are extensions, so `live` depends on neither.
- **The handshake.** The route sits under `rest.RequireUser`, so a browser whose session expired gets 401 and cannot open the socket. A refused handshake looks like a server that is down, so the browser makes one authenticated REST request (`GET /authz/me`). A 401 there refreshes the session through the shared client, and a server that is down leaves the session alone.

### Backend
- **`features/live`** holds the types and points:
  - `live.command`: `live.Command`, with `Decide(Decision) → events | problems`, its args schema, its area, target type and projection, and its authz action;
  - `live.projector`: `live.Projector`, which folds `live.Fold{Key, State, Event}`;
  - `live.query`: `live.Query`, run as `Run(live.Feed)`. The helpers `live.ValueOf` (one key) and `live.ListOf` (the keys matching a pattern) build most queries;
  - `live.call` and `live.extension`.

  Subjects are `evt.<tenant>.<area>.<type>.<id>.<event>`, and a target's projection key is `<type>.<id>`. The `live.Store` interface (`Get`, `Watch`) is what queries read.
- **`live/natsd`** starts the embedded server: JetStream in `<data-dir>/nats`, no listener (or `Listen` for other processes), and an in-process connection. `--nats-url` (or `$FOUNDATION_NATS_URL`) uses an external server instead. `natsd.StartTest(t)` gives a test its own server in a temporary directory. `serve` starts NATS after the databases, and drains it after the HTTP server has shut down.
- **`live/project`** (`project.Projections`) does the following:
  - makes streams (`Duplicates` 10 minutes, `AllowDirect`, no age limit) and buckets (`PRJ_<tenant>_<projector>`);
  - runs the projectors;
  - implements `live.Store`: `Watch` sends the current values, then `nil`, then each change;
  - provides `WaitFor(key, seq, timeout)`, which waits until a value reflects a sequence (read-your-writes for the bus).
- **`live/bus`** (`bus.Bus.Execute`): finds the command, checks its id, target and `authz` action, checks its args with `tools.CheckArgs`, then decides and commits as above. A rejection is 422 `command_rejected` with `problems [{path, message, fix}]`.
- **`live/subscribe`** (`subscribe.Subscriptions`): `Open` checks the query's action and params and runs its feed until the context ends, and `Once` is a one-time read.
- **`live/presence`**: the `presence_<tenant>` bucket (memory storage, keys live 30 seconds), `Set` and `Clear`, and the `live.presence` query (`{object: "<type>.<id>"}`). It returns a list of `{connection, userId, name, color, state, at}`, and a user's colour is picked by their id.
- **`services/liveservice`** and **`rest/livehandlers`**: `GET /api/v1/live` upgrades with `coder/websocket`, and `Serve` runs one connection.
  - Text frames are JSON with a `type`; binary frames are a 4-byte channel and a payload.
  - Commands from one connection are decided in order, by a goroutine of their own.
  - Presence is refreshed every 10 seconds and cleared when the connection ends.
  - A client too slow for its 4096-frame queue is dropped and reconnects.
  - Each frame that causes work may carry a `trace`, which its work logs under.
- **`live/settings`**: the `live` section (System › Live connection) holds `maxSubscriptions` per connection (500).
- **`live/tools`** (`livetools.Contribute`, after every feature's commands): `live.listCommands`, `live.listQueries`, `live.query` (a one-time read), and one tool per command with effect `write`, input `{target, args}`.

### The frames
| Direction | Frame |
|---|---|
| ← | `hello {connection, userId, color}` first |
| → | `command {id, command, target, args, request, trace}` |
| ← | `ack {id, stream, seq, duplicate?}` / `rejected {id, code, message, errorCode, problems}` |
| → | `subscribe {sub, query, params, after?}` / `unsubscribe {sub}` |
| ← | `snapshot {sub, pos, data}` / `change {sub, pos, delta {op: replace\|upsert\|remove, key, value}}` / `subError {sub, code, message}` |
| → | `presence {object, state}` or `{object, leave: true}` |
| → | `call {id, call, args}` / `cancel {id}` |
| ← | `event {id, event, data}` / `end {id, code, message?, errorCode?}` |
| ↔ | `ping` / `pong` (the browser pings every 20 s and reconnects after 30 s without an answer) |
| → | `ui.open`, `ui.event`, `ui.close` ([ServerUI.md](ServerUI.md)) |
| ← | `ui.tree`, `ui.patch`, `ui.error` |
| → | `collab.join {channel, fileId}`, `collab.leave {channel}`; binary frames on the channel ([MarkdownEditor.md](MarkdownEditor.md)) |
| ← | `collab.closed {channel, code, message}` |

### Browser
- **`src/live/LiveConnection.ts`**: `live`, the window's connection, opened on first use.
  - It reconnects with backoff (0.5 to 15 seconds, at once when the browser comes back online). It then resends unacknowledged commands, resubscribes each query `after` its position, sends its presence again, and calls `onReopen` listeners (UI sessions, rooms).
  - Its API:
    - `command(command, target, args)` → `Promise<Ack>`, rejecting with `CommandRejected` (a `ServiceError` with `problems`);
    - `subscribe(query, params, handlers)` → unsubscribe;
    - `setPresence(object, state | undefined)`;
    - `call(call, args, onEvent, signal)`;
    - `on(type, listener)` for extension frames;
    - `openChannel(onMessage)` for binary channels;
    - `send(frame)`;
    - `getStatus`, `onStatus` and `getHello`.
- **Hooks** (`src/live/index.ts`):
  - `useQuery(query, params)` → `{data, pos, loading, error}`;
  - `useCommand()` → `{send, pending(pos)}`, the commands not yet reflected at a position, to lay over a query's data;
  - `usePresence(object, state)` → `{all, others, me}`;
  - `PresenceAvatars` (one avatar per user in their colour, a count of their windows in the tooltip).
- **Clients moved onto it**:
  - `Chat.stream` and `Conversation.send` are live calls, with the same signatures as before;
  - `Config.events(onChange, onResync)` follows `config.changes`. After a reconnect it calls `onResync`, and `configStore` refetches every section it holds;
  - `CollabProvider` takes the file id and joins the room on a channel.
- **DevTools › Live** (`tabs/devtools/live/LiveViewer.tsx`): the connection's status and its last 500 frames (`src/live/frames.ts`), newest first, with a filter, pause and clear, and the selected frame as JSON. Pings are not recorded, and binary frames show only their size.

### Verification of slice 13
- `backend/scripts/build.sh --test ./features/...` passes.
  - `live/bus`: 100 concurrent increments from two goroutines leave the counter at exactly 100, and a resent command is a duplicate with its first sequence.
  - `ui/hello`: two users on in-memory live connections see each other's increments as `props` patches. It also covers acks, duplicates, rejections with problems, a presence change, and a session's view state resumed on a new connection.
- `cd frontend/common && yarn build` passes. `yarn lint` gives one warning, React Compiler's `incompatible-library` on `useVirtualizer` in `mui.table`. `frontend/example`'s `yarn build && yarn lint` pass.
- Seen in headless Chromium against `serve` and the Vite dev server:
  - Two users, Ada and Bob, each see the other's increments, avatars and selections. Bob's selected node is outlined in his colour on Ada's canvas.
  - An edge Ada connects and a node she moves appear for Bob. Over ten increments, Bob saw Ada's click after a median of 75 ms (slowest 138 ms), Playwright's overhead included.
  - Two users edited one markdown file over the live connection's channels.
  - A setting changed elsewhere reached an open window as a `config.changes` change.
  - With no model key, a chat message failed visibly ("AI provider is not configured") through the call's `end`.
  - With `serve` stopped under two open windows, a click made while it was down applied exactly once after it restarted. Subscriptions and sessions resumed without a reload, and no console errors appeared.

### Decisions to review after seeing it
- The browser resumes a subscription with a fresh snapshot rather than the changes since its position.
- One stream per area. An app's aggregates go to its own `app_<appId>` stream, while its records will go to `rec`.
- Commands on one target are serialized in-process, and the bus waits for the projection before deciding.
- Presence is a query, and selections travel in presence `state` (`{selected: [...]}`).

## Changed in slice 15
For blueprints ([Blueprints.md](Blueprints.md)):
- `bus.Bus.Resolve` and `subscribe.Subscriptions.Resolve` find commands and queries no feature registered: a tenant's published command and query graphs (`livenodes.CommandFor`, `QueryFor`). `Bus.CommandFor(args, type)` looks in both.
- `live.Command.Report`: what a command's tool returns after a commit, besides the sequence (a graph draft's problems).
- The tool `live.command {command, target, args}` sends any command, including graph-defined ones.
- `presence.Presence.Here(object)`: whether anyone is in an object, e.g. a graph open in an editor.

## Changed in slice 16
For entity types ([Records.md](Records.md)):
- `project.Projections.Follow(ctx, tenant, area, name, subjects, fn)` runs a side effect on each matching event of a tenant's area, in order, as the durable consumer `fol_<name>`: work outside the projections, such as copying a published entity type into SQLite. `fn` must be idempotent, and an error retries the event a second later. Like a projector, a follower starts the first time it is asked for in a tenant and catches up from where it stopped.

## Changed in slice 17
For records ([Records.md](Records.md#built-in-slice-17)):
- `live.Command.TypeOf(args)` reads the target's type from the arguments, for a command that serves many types: the `record.*` commands take it from `entity`. `Command.TargetFor(id, args)` gives the target either way.
- `live.Command.NewTarget()` makes the target's id when a command is sent without one. The result's `Target` and the ack frame's `target` carry it. A browser makes its own ids, so a resend stays a duplicate.
- `live.Command.Internal` marks a command only the server sends (`record.convert`): it gets no tool.
- `live.Problem.Code`, when set on the first problem, is the rejection's error code in place of `command_rejected` (`record_changed`).

## Changed in slice 18
For record indexes ([Records.md](Records.md#built-in-slice-18)):
- `live.Projector.Keep(live.Keep)` keeps keys outside the projection in step with each value, in the same fold: it runs after `Fold` and before the value is written, given the keys it kept for the old value, and returns those it keeps for the new one. `live.Projected.Kept` stores them with the value. An error retries the event. The records' index entries are kept this way.
- **A fold writes with the revision it read** (`Create` for a new key, `Update` otherwise). A value changed meanwhile (by `Rekeep`) fails the write, and the event is folded again from the new value.
- `project.Projections`:
  - `Rekeep(ctx, tenant, projector, key, fn)` runs a Keep again for one key outside a fold, when what values keep has changed (an index added). Its write expects the revision too, and `fn` runs again on a conflict.
  - `Side(ctx, tenant, name)` is a bucket beside the projections (`PRJ_<tenant>_<name>`), made on first use, for a Keep's keys.
  - `Updates(ctx, tenant, projector, pattern)` watches changes from now on, without the keys as they are, for a query that reads its snapshot another way.
  - `Settle(ctx, tenant, projector)` waits until the projector has folded every event its stream held when called.
  - `Context()` ends when the projections close.
- A follower's function reports work in progress every 10 seconds while it runs, so long work (rebuilding an index) is not redelivered.

## Changed in slice 19
For live record queries ([Records.md](Records.md#built-in-slice-19)):
- **Ordered lists.** `live.Delta` has `List`, `Index` and `From`. With `List`, a change applies to the ordered list at that key of the result: `insert` (the value at `Index`), `update` (the value at `From`), `move` (the item at `From` taken out, and the value put at `Index`) or `remove` (the item at `From`). Each index counts the list as the changes before it left it.
- **`reached`** (`live.Reached`) changes nothing: the subscription has reached the change's position. A query sends it when an event it followed showed nothing, so a client sees its own write applied.
- `live.Apply(data, delta)` applies a change to a result held as decoded JSON. UI sessions keep their queries with it.
- `project.Projections.Folded(ctx, tenant, projector)` is the position the projector has folded, its consumer's ack floor. A snapshot read after it reflects at least that position.
- In the browser, `Delta` has the same ops and fields, `applyDelta` applies list changes, and `useQuery` moves its `pos` on `reached` without changing its data.

## Built in slice 22
Forge's part 3h ([forge 03h-Transactions.md](../../forge/documentation/plans/03h-Transactions.md)): a command's several events commit as one **atomic batch**, on its own target and on other targets it read, so a command can change several records together or not at all.

### Decisions
The plan had no open questions before building (its question 4, whether a batch is enough for a ledger, waits for forge's QuickBooks iteration). Decided while building:
- **One event is published alone, several as a batch.** A commit of one event is atomic already, and keeps the plain publish with its duplicate ack. Two or more use JetStream's atomic batch publish (NATS 2.12 and later; the embedded server is 2.15.2): all are stored, one after another, or none is.
- **nats.go has no batch API yet**, so `project.PublishBatch(ctx, conn, msgs)` sends the `Nats-Batch-Id`, `Nats-Batch-Sequence` and `Nats-Batch-Commit` headers itself, one request per message, and returns a refusal as a `*jetstream.APIError`. A batch holds at most 1000 messages (`project.MaxBatch`, the server's limit); a command deciding more is rejected with a problem.
- **Events may name other targets in the command's area** (`live.NewEvent.Target`); a batch is one stream, so another area is refused. `live.Decision.Read(projection, target)` reads another target's state, waiting like the command's own target for this process's last commit on it.
- **Every event carries its target's expected sequence**: the sequence its state was read at, or for a target the decision did not read, the target's last event in the stream when committing. If any check fails, nothing is stored and the command is decided again, after each target's projection has caught up with its actual last event.
- **A retry after a conflict holds every target the batch changes.** The first decision locks only the command's own target, as before. After a conflict the bus locks all its targets, in key order (all again when one more is needed), so commands crossing each other (a transfer from A to B against one from B to A) are decided one at a time in this process instead of failing with `409 conflict` after three tries. A command holding one lock never waits for another, so this cannot deadlock.
- **A resent batch is a duplicate.** Every event keeps its message id (`<command id>`, then `#1`, `#2`…), and JetStream refuses a batch that holds a stored id (error 10201) without saying where it was stored. The ack is then `duplicate: true` with the last sequence of the batch's targets, which a client waiting for its write reaches no earlier than the original.
- **Each event of a batch says so**: `live.Event.Batch {index, count}`. The events are stored in one run, so `Batch.Range(seq)` is the batch's first and last sequence.
- **Projectors write a batch at once.** When a projector folds a batch's first event it reads the rest of the batch it folds from the stream (`stream.GetMsg`, skipping types it does not fold), folds them in order, runs `Keep` for each key, and writes every changed key in one atomic batch on the bucket's stream, each expecting the revision it read. A read never sees half a batch, and a crash cannot leave one half written; the batch's later events are then skipped as already folded. Each value keeps the sequence of its own last event, so a command's expected sequence still matches the stream. A batch that spans two projectors is written atomically by each, not across them.
- **Streams and buckets allow atomic batches**: `Stream` makes event streams with `AllowAtomicPublish`, and `Bucket` turns it on for a projection's `KV_` stream, upgrading those made before.

### Backend
- `live`: `NewEvent.Target`, `Decision.Read`, `Event.Batch` and `Batch.Range`.
- `live/project`: `PublishBatch`, `BatchAck`, `MaxBatch`, `IsConflict` (wrong last sequence, also the constant form), the error codes `ErrCodeBatchDuplicate` and `ErrCodeAtomicDisabled`; the batch fold.
- `live/bus`: `commit` builds the messages with their headers and publishes one alone or all as a batch; `hold`/`release` lock targets in key order; conflicts read each target's last event with `GetLastMsgForSubject`.

### Verification of slice 22
- `backend/scripts/build.sh --test ./features/...` passes, so every existing live core, blueprint (drafts commit several events when editing a built-in) and records test passes over batch commits. Forge's `backend/scripts/build.sh` builds.
- `live/bus/batch_test.go`, over a bank of accounts whose `bank.transfer` debits its target and credits another it reads:
  - a transfer commits two events marked `batch {1, 2}` and `{2, 2}`, the ack names the second, both balances move, and a resent transfer is a duplicate that stores nothing;
  - **acceptance**: while a transfer is decided, another command deposits into the account it credits; the batch fails its sequence check and nothing of it is stored, then it is decided again and committed whole (the stream holds the deposit, then both events of the transfer);
  - a batch whose second message expects a stale sequence stores neither message;
  - a subscriber watching both accounts during 60 concurrent transfers in both directions sees each transfer's two changes together, and when the first arrives the other account already shows it. With the projector writing a batch's values one by one instead, this fails in 5 runs of 5;
  - a stream and bucket made without atomic batches are upgraded on first use.

### Decisions to review after seeing it
- A subscription's changes for a batch's records go out as separate `change` frames, one after another with nothing between. A browser could render between two of them; grouping a batch's changes into one frame waits until a page shows it matters (the task list, forge 3i).
- A resent batch's ack names its targets' last sequence rather than its own.
- A retry locks all of a command's targets; the first decision still locks only its own.
- A batch spanning two projectors is atomic within each, not across them.

## Changed in slice 23
- **`live.Apply` copies** the list or map it changes and leaves the data it was given as it was. A UI session's rendered props share its held query's data, so a row updated in place left the bound prop equal to itself and no patch went out ([Records.md › Built in slice 23](Records.md#built-in-slice-23)).
- **A command graph's host carries its `Decision`** (`livenodes.CommandHost.Decision`), so the record nodes read and write other records in the command's area, and **a `data.query` graph can watch a key pattern** (`livenodes.Read.Pattern`), run again, debounced, when any matching key changes.
