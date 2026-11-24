# Recent activity

What a person does by hand, kept briefly and readable only by them: the tabs they focus, their clicks on app pages, the record and app commands they send and their definition changes, grouped by request into **gestures** with plain-language summaries. The chat will show it and automate it ([forge 15d](../../forge/documentation/plans/15d-AutomateWhatIDid.md)). Forge's Activity tabs, an object's operation log, are a different thing ([forge Activity.md](../../forge/documentation/Activity.md)).

## Built in forge 15c
Forge's part 15c ([plans/15c-ActivityHistory.md](../../forge/documentation/plans/15c-ActivityHistory.md)). The package is `features/activity`.

### Where it is kept
- A **per-tenant JetStream stream** `ACT_<tenant>`, one subject per person, `act.<tenant>.<userId>` (`activity.Store`). The stream's own limits are the settings' maximums: a day (`MaxAge`) and 1000 entries per person (`MaxMsgsPerSubject`).
- Each entry is published with a **time to live** (`Nats-TTL`, the stream's `AllowMsgTTL`) of the person's `keepHours`. After each write the person's subject is purged down to their `keepCount` (`WithPurgeKeep`).
- **Every write is idempotent by `Nats-Msg-Id`**: `ui-<request>` for a click, `evt-<stream>-<seq>` for an event, the browser's id for a focus. Several runners writing the same entry store it once. No state is held in a process.
- **The `activity` section** (*Chat › Recent activity*, tenant and user scopes) has three fields:
  - `record`: on by default (question 11).
  - `keepHours`: 4, at most 24.
  - `keepCount`: 300, at most 1000.

  With `record` off, nothing is written, and any write attempt purges the person's subject. `activity.WatchSettings` also purges, on any change to the section, everyone under that scope whose recording is now off (`config.Changes`).

### What is recorded
Only **people's** gestures. `activity.Person` takes users only, and events count only when they have a user, no agent and no actor. So an agent's run, an app's trigger, a service and the chat's tool calls (actor `chat`) record nothing.

| Entry (`kind`) | Source | Holds |
|---|---|---|
| `focus` | the browser's `activity.focus` call, sent by `startActivityFocus` (`workbench/context/activityFocus.ts`, started by `Workbench.tsx`) once the focused tab's workbench subject has stayed focused for 1.5 s, and only when it changes | the subject's type, id, label and `details` |
| `ui` | `ui.Session.Event` calls its `ui.Recorder` (`Options.Recorder`, `uiimpl.WithRecorder`) for every handler event of a person on a page that is not dry; `window:` events (scrolling a virtual list) return before it | page, app, the tab's title and subject, handler (a prefab instance's graph name), the component event that names the handler on the node, the node's key path, type and **visible label** (`ui.VisibleLabel`: its `label`, `text`, `title`, `primary`, `tooltip`, `header`, `placeholder` or `name` prop, else a descendant's within three levels, 80 characters). **Never the payload** (question 5) |
| `command` | the follower `activity.rec` (`FollowNew`, one shared durable consumer per tenant) on the `rec` stream | command type and title, app, the target's entity type (and name), id, record title, the event (`created`, `updated`…), and the changed fields by name with their new values |
| `op` | the follower `activity.def` on the `def` stream, on an op's first event | the op's summary as `ops.list` words it (`ops.SummaryOf`) |

- **Values, never content.** A field's value is copied for these types:
  - short text (80 characters, one line);
  - numbers, yes/no, dates, durations, users and tags;
  - choices, by their option names;
  - relations, by the related records' titles (ten at most).

  Long text, Markdown, rich text, files, JSON, lists and objects are named with `omitted: true`. A sent reply records its subject and recipients, not its body. Computed and metadata values are the decision's, not the command's, and are left out.
- **One gesture is one request.** `Session.Event` makes the request id first, records the click under it and runs the handler with it. `Handle.Command` sends it with the page's `app` (`ui.CommandRequest`), and the bus puts the command type and app on every event (`live.Event.Command`, `App`; [Live.md › Changed in forge 15c](Live.md#changed-in-forge-15c)). A page's own commands (Mail marking a thread read on opening) are a gesture with no click.
- **Summaries** are made when an entry is written, from metadata the platform already has. For example:
  - `opened "Refund for the second charge?"`
  - `clicked "Billing" in Desk`
  - `changed "Labels" in Refund for the second charge? → Label a thread "Refund for the second charge?" (Labels: Billing)`
  - `created Task "Call the printer repair shop" (Done: no, Title: …)`

  A gesture's summary is its focus or click, then `→` and its commands' summaries. A summary repeated in one gesture shows once with a count.

### Reading it
- **`useQuery<Activity>('activity.mine', {after?, limit?, sendable?})`** gives the caller's own gestures, newest last, as `{recording, gestures: [{id, start, time, app, subject, summary, repeats, dismissed, entries}]}`. Types are in `@foundation/common/services/Activity.ts`.
  - It follows the person's subject with an ordered consumer, and sends the whole result again (`replace`) on each entry and each minute, as entries expire.
  - The query has **no principal parameter**. Its params are decoded strictly, so `{user}` or `{principal}` is refused, and a principal that is not a user (an agent, an app) is refused.
  - `activity.read` and `activity.write` are `self` actions on `tenant:<t>/user:<u>/activity`, so no grant reaches anyone else's: not an administrator's, not an Owner's.
- **Grouping** (`activity.Group`):
  - A `clear` entry drops what came before it, and `dismiss` entries mark gestures. Entries older than `keepHours` are left out even before their TTL removes them.
  - In a gesture the focus and the click come first, then the commands.
  - The same handler on the same node, with no commands, within 30 seconds of the last is merged as `repeats` (typing in a search field). `limit` keeps the newest gestures (50 by default).
- **Calls** (not commands: a command commits to an area stream kept forever, and none of this is kept). None of them is a tool, since no agent or chat round reads or changes a person's history. Each one refuses principals that are not people.
  - **`activity.focus {id?, subject}`** records a focused tab (`ActivityCalls.focus`).
  - **`activity.dismiss {ids}`** marks gestures so they are never sent to a model; `sendable: true` leaves them out.
  - **`activity.clear`** purges the person's subject and writes a `clear` entry, so their open views empty too.
- **Go**: `activity.Read(ctx, deps, tenant, user, params)` and `Store.Read`, for the chat (15d) and the developer tools. Forge's `forge activity` and the MCP tool `activity` read it as the signed-in person, and dismiss or clear it first ([forge DevTools.md](../../forge/documentation/DevTools.md)).

### Wiring
`serve.go` (and Forge's copy):
- `activity.ContributeAccess`, `activity.ContributeSettings` and `activity.Contribute(registry, recorder)`.
- The recorder from `activity.NewRecorder(func() activity.Deps)` with `{Projections, Store: activity.NewStore(js), Config, Bus}` (the bus names commands by title, an installed app's too).
- `go recorder.Start(ctx, tenantIDs)` follows every tenant as it comes.
- `activity.WatchSettings(ctx, configChanges, …)`.
- `uiimpl.WithRecorder(recorder)`.

### Decided while building
1. **Recording on by default** (question 11): the user's answer, 2026-11-24, on the proposal. The composer's indicator comes with 15d.
2. **No searches outside app pages** (question 12): the user's answer, 2026-11-24, on the proposal. `activity.note` is left for later.
3. **Calls, not live commands.** The plan said `activity.focus` was a live command, but a command commits to an area stream kept forever.
4. **The settings' limits are enforced per person** by the message TTL and a purge-keep after each write. The stream's limits are only the maximums.
5. **The `rec` and `def` streams only.** An app's own area (`app_<appId>`, such as the Hello counter's `hello.increment`) is not followed. Its click is recorded, but its command is not.
6. **The follower starts at the stream's end** (`FollowNew`): what happened before it was first made is nobody's recent activity.
7. **Field values that are content are named, not copied**, by field type, to keep the plan's rule that no body or attachment is ever copied.
8. **The graph renderer no longer sends an empty `select`** each time React Flow mounts or shows its canvas ([ServerUI.md › Changed in forge 15c](ServerUI.md#changed-in-forge-15c)). It was a click no one made, and filled the history.

### Verification
- `go test ./features/activity` (Foundation, also with `-race -count=2`):
  - `TestAClickAndItsCommandsAreOneGesture`: a focus, then a click whose handler updates a record. The result is one gesture with the label, the changed fields (the Markdown body named, not copied), the page's app and a shared request. The payload and `window:` events are not kept, three searches merge, and `activity.mine` gives the same.
  - `TestOnlyThePersonsOwnGestures`: an agent's run, an app's trigger and the chat's tool call record nothing. Bo reads none of Ana's history, and `{user}`, `{principal}` and an agent are refused.
  - `TestDismissAndClear`: a resent focus is a duplicate, a dismissed gesture leaves the `sendable` view, and a clear empties the history.
  - `TestRecordingOffAndTheLimits`: `keepCount` trims, recording off clears what was kept and records nothing, and entries past `keepHours` are gone from the stream (an hour shortened to a second).
  - `TestTwoRunnersWriteEachGestureOnce`: two processes over one JetStream write each click and change once.
  - `TestHandlerWords`.
- The packages these changes touch pass: `live/...`, `ui/...`, `apps/ops`, and `apps/tools`' `TestMetadata`. The full suite had two known parallel-load flakes, recorded again in forge's `plans/99-Cleanup.md`. `frontend/common` `yarn build` passes.
- Forge: `go test ./features/starter/mail -run TestAnasActivityInMail`, the Mail fixture. Ana opens a thread (the page marks it read), labels it Billing, starts a reply and sends it. Her history holds five gestures in plain words, with the thread's subject and the reply's subject, and no message or reply body. Bo has none.
- In the browser (forge `documentation/walks/15c.json`, `forge serve` and Vite, made multi-user with `forge users`: maya the administrator, ana and ben):
  - Ana's history had four gestures: Hello's *Increment* and *Open task list* clicks, *Add* on the task list as one gesture with its `record.create`, and the focus on *Hello tasks*, as `forge activity --username ana` printed them.
  - Ben's history was empty, and Maya's (the administrator's) held none of Ana's.
  - `activity.mine` with `{user}` was refused. A `record.create` sent for Maya with actor `chat` left no gesture.
  - `--dismiss` hid the *Add* gesture from `--sendable`.
  - Turning *Record my recent activity* off in Settings (user scope) showed `recording is off`. Turning it on again showed an empty history: what was kept had been purged.
  - No console errors.
  - Before the graph renderer's fix, every click on the Hello page brought six to eight phantom `select` gestures. After it, none.

### Decisions to review after seeing it
- A crafted parameter (`{user}`) is refused by the query's own strict decoding, so it comes back as `500 invalid_params`: the shared params check (`tools.CheckArgs`) does not enforce `additionalProperties`.
- The summaries' wording (`clicked "Add" in Tasks → created Task "…" (Done: no, Title: …)`), in the chat once 15d shows it.
- A page's own commands (marking a thread read on opening) as a gesture of their own, before the focus that the browser reports 1.5 s later.
- Field values copied by type (decision 7).
