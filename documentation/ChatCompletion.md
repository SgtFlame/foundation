# Chat: the AgentOS Chat editor

AgentOS › Chat is a chat editor for general conversations with long-term memory through Chronicle ([Chronicle.md](Chronicle.md)), built on the provider-neutral `chatservice` and the hybrid search index ([AIChatEmbed.md](AIChatEmbed.md)). It is extensible through extension points ([Contributors.md](Contributors.md)):
- the composer's ⚙ settings come from `chat.contextSettings` contributions, each paired with a server `chat.contextProvider`;
- levels and defaults come from configuration ([Configuration.md](Configuration.md));
- every call's tokens and cost are recorded for the Dashboard ([Usage.md](Usage.md)).

Terminology: memory entries are **Chronicles**, and knowledge base collections are **Volumes**.

What is built, slice by slice: slice 4 built Chat, thin (conversations and rounds, the levels, context providers and the composer); slice 5 added RAG Search, inline citations, the Sources row, the Context inspector and feedback; slice 6 added Chronicle memory (the window, stage 1, Chronicle chips and the memory update); slice 8 made the reply the tool loop; slice 44 made every round its chat mode's graph ([Built in slice 44](#built-in-slice-44)). What remains, including the Markdown Editor's Chat tab, is in [plans/ChatCompletion.md](plans/ChatCompletion.md).

## Decisions
- **Default models per level**: Fast = `gemini-3.5-flash-lite`, Standard = `gemini-3.8-flash` (the default level) and Deep = `gemini-2.5-pro`, with Chronicle's internal calls on Fast. These are the `ai.chat.levels` defaults seeded in slice 2.
- **Tool calling**: built in slice 8, with the first tools and per-call confirmation decided before building; see [Built in slice 8](#built-in-slice-8) and [Tools.md](Tools.md).

## Built in slice 4
**Backend: the `conversation` feature** (`features/conversation`), not `chronicle`.
- **Where `Send` lives**: `conversationservice.Send`, not `chronicleservice`. Conversations and rounds belong to chat whatever the mode. Chronicle's stages (slice 6) then run inside `Send` as context providers and steps, so the rounds and the stream stay where they are.
- **Tables**
  - `Conversation`: `db.Tenanted`, `db.Owned`, `Title`, `ContextSettings` (JSON, by contribution id, as last sent) and `LastRoundAt`.
  - `Round`: `db.Tenanted`, `db.Owned`, `ConversationID`, `Text`, `Reply`, `Level`, `model`, `Status`, `FinishReason`, `Error`, `Workbench` (the snapshot), the four token counts, `CostMicros`, `Currency` and `LatencyMs`.
- **Privacy**: conversations are private to their owner. Every query filters on `owner_id`, so another user's conversation is 404 `conversation_not_found`. Sharing waits for RBAC. The authz actions are `conversation.read` and `conversation.write`.
- **`conversationservice`** under `/api/v1/conversation`
  - `POST list` (`before`, `limit`; newest first, with `more`).
  - `POST rounds` (oldest first).
  - `POST send`.
  - `GET context-providers`.
- **`Send`**
  - **When the round is stored**: as soon as the message is sent, with status 0, so the call's `Meta` carries the `RoundID` and the usage record names it. The round is completed when the reply ends.
  - **Round status**:
    - **200**: the reply is complete.
    - **499 (stopped)**: the user pressed Stop. The reply so far is kept, and the writes use `context.WithoutCancel`.
    - **Any other status**: the reply failed. The status and its public message are stored and shown in the transcript, so with no API key the round shows the 503 message.
  - **Refused messages**: a message refused before its round exists (empty, too long, unknown level, a snapshot over 16 KB) gets a plain JSON error.
  - **SSE events**: `started` (the conversation and round), `delta`, `done` (the completed round) and `title`. `error` is used only if the stream itself fails.
  - **No memory yet**: the last `chat.historyRounds` rounds (50 by default) are sent as plain messages. Only rounds with status 200 or 499 and a reply are sent. The Chronicle window replaces this in slice 6.
  - **The footer's numbers**: tokens and cost are copied onto the round, priced from the level's catalog model by the same `Pricing.CostMicros` that prices usage records. The footer therefore needs no query over `usage_records`, which stay the Dashboard's source.
  - **Titles**: after a conversation's first successful round, a call at `chat.titleLevel` (Fast) with `Feature = chat-title` writes the title. It arrives as a `title` event after `done`.
- **Levels drive `chatservice`**
  - `CompleteArgs.Level` (and `level` on `/ai/chat/complete` and `/stream`) resolves through `ai.chat.levels` and `ai.models`.
  - The level fills the model, temperature, max output tokens and the thinking budget (Gemini's `ThinkingConfig`). Fields the caller sets explicitly win.
  - The level sets `Meta.Level`.
  - An unknown level or model is 400 (`unknown_level`, `unknown_model`).
  - `--chat-model` is now only the model for calls that name no level.
- **Context providers**: the backend point `chat.contextProvider` (`conversation.ContextProviders`).
  - **The contribution**: a struct `{Descriptor, Title, Schema, Defaults, Gather}`, rather than an interface (as first sketched). That matches `config.Section` and keeps it in the registry.
  - **Running them**: `Send` runs every enabled provider concurrently. A provider is enabled unless its settings say `"enabled": false`, and one that fails is skipped for the round.
  - **Which settings apply**: the conversation's own, else the `chat.context.defaults.<id>` section, else the provider's `Defaults`.
  - **Placing the context**: items go after the system prompt under a `# Context` heading that says they are data, not instructions. They are ordered by score and capped at 48 KB until levels give stage 2 a token budget.
  - **The first provider** is `workbench` ([WorkbenchContext.md](WorkbenchContext.md#built-in-slice-4)).
- **Settings**
  - **`chat`**: `systemPrompt`, `historyRounds` and `titleLevel`, at the app, tenant and user scopes.
  - **`chat.context.defaults.<id>`**: declared on the **server** from each provider's `Schema` and `Defaults` by `conversationsettings.Contribute`, which `serve.go` calls after the providers register. Browser-only sections cannot be stored yet, so a `chat.contextSettings` contribution carries no `schema` or `defaults` of its own.

**Frontend**
- **Client**: `src/services/Conversation.ts` (`Conversations.list`, `rounds`, `contextProviders`, `send`). `send` streams over `fetch` with `onStarted`, `onDelta`, `onDone` and `onTitle`, and resolves with the completed round.
- **`src/editors/chat/`**
  - `ChatObject` (`chat:main`) and `ChatEditor`.
  - `points.ts` declares `chat.contextSettings` and `chat.command`.
  - `contributions.ts` contributes the editor and `/new`, `/level <level>` and `/help`.
- **`src/components/chat/`**
  - **`useChatStream`**: the conversations, oldest first, the last being current; the streaming round; `loadEarlier`; `newConversation`; `say` (a local note); and `setContextSetting`.
  - **`ChatTranscript`**: virtualized and keyed by round id, with a divider per conversation. The current conversation's divider has ✕.
    - It follows the reply while at the bottom.
    - Scrolling to the top loads three earlier conversations at a time, keeping the view in place.
  - **`RoundView`**: the user bubble, the reply, "Stopped", the output-limit warning, the error, and the hover footer (level, model, tokens, cost, latency).
  - **`ChatComposer`**: Enter sends, Shift+Enter adds a line, `/` lists the commands, and Stop aborts.
  - **`LevelToggle`**: hovering a level shows its model and an estimate of the input tokens and their cost: the system prompt, the history the server will send and the snapshot, at about four characters a token.
  - **`ChatSettingsPopover`**: one control per `chat.contextSettings` contribution.
  - **`useContextSettings`**: hides a contribution whose server provider is missing and reads the defaults sections.
- **Reply rendering**: `src/components/markdown/MarkdownText.tsx` renders replies with Live Preview's renderer, read-only. The renderer takes `livePreview({ revealSelection: false })`, so no line shows raw markdown.
- **Opening Chat**: it opens on the **latest conversation and continues it**. ✕ or `/new` starts a new one.
- **Sidebar**: an **AgentOS** section with Chat.

### Decisions to review after seeing it
Each is described above. They were made while building, not asked beforehand.
1. Chat is a new `conversation` feature, and `Send` lives in `conversationservice` rather than in `chronicleservice`.
2. Conversations are private to their owner until RBAC.
3. Context-control defaults are server sections (`chat.context.defaults.<id>`), not frontend `schema`/`defaults`.
4. Rounds copy their tokens and cost instead of reading their usage records.
5. Until Chronicle, the last `chat.historyRounds` (50) rounds are sent as plain history.
6. Chat opens on the latest conversation and continues it.

### Verification of slice 4
**Done**
- Backend tests (`backend/scripts/build.sh --test ./...`, all passing).
  - Level resolution in `chatservice` (`impl/Level_test.go`).
  - `Send`: storing, streaming, pricing, history, context settings per conversation, titles, owner isolation, unknown levels and stopped rounds (`conversationservice/impl/Send_test.go`).
  - Snapshot rendering and quoting (`workbench/context/Provider_test.go`).
  - Capture and purge (`usage/Providers_test.go`).
- Frontend: `yarn tsc -b`, `yarn lint` and `yarn build` are clean.
  - `ChatTranscript` suppresses oxlint's `react/incompatible-library` warning for `useVirtualizer`, because the project does not use the React Compiler.
- **Smoke test against a real server**: `/conversation/*`, `/usage/capture` and the new settings sections.
- **Headless Chromium against `yarn dev`**:
  - Chat opens and continues the latest conversation.
  - `/` lists the commands, and `/help` adds a note.
  - Sending stores a round, and the 503 message shows without an API key.
  - The ⚙ popover shows the Context control.
  - The stored snapshot names the Volumes tab as `previous`.

## Built in slice 5
What changed in Chat; the engine is in [ContextRetrieval.md › Built in slice 5](ContextRetrieval.md#built-in-slice-5).
- **RAG Search** is the second `chat.contextSettings` control (`src/retrieval/`), paired with the server's `rag` provider. It is on by default, at the neutral dials and 8 segments.
- **Context items can be cited**: `ContextItem.Segment` marks an item as a segment. `Send` numbers such items `[1]…[n]` in the prompt, in the order they fit, and asks the model to cite them inline.
- **SSE**: a new `sources` event, carrying the round's segments, comes after `started` and before the first `delta`.
- **Rounds** carry `feedback` and `segments` (from `RoundSegment`). `POST /conversation/rounds` returns them, and `POST /conversation/feedback {roundId, feedback}` sets the verdict (404 `round_not_found` for another user's round).
- **History** leaves out 🚫 rounds. The level toggle's estimate leaves them out too.
- **Listing by id**: `POST /conversation/list` takes `ids`, for the Dashboard's costliest conversations.
- **`RoundView`**
  - Citations render as chips inside the reply (`MarkdownText` `citations`).
  - The Sources row and the Context inspector are below the reply.
  - Feedback buttons sit beside the footer. They show on hover, and stay visible once set.
- **Opening a citation**: a chunk citation opens the Markdown editor with the cited lines selected (`openMarkdownAt`, `editors/markdown/reveal.ts`). It uses the line numbers stored when the round ran, which drift if the file has been edited since. Relocating by the `Head`/`Tail` anchors is not built.

## Built in slice 6
The memory itself is in [Chronicle.md › Built in slice 6](Chronicle.md#built-in-slice-6). What changed in Chat:
- **The window**: `chat.historyRounds` is the window (default 15, was 50), and `Send` passes it to every context provider as `GatherArgs.Window`.
- **Providers' calls** now carry the round's conversation and round in `Meta`, so stage 1 and query embeddings are attributed to the round.
- **Pinned items first**: context items whose segment is `Pinned` go first, in their provider's order; a segment found twice goes in once. The citation instruction now names memory as well as documents.
- **Round followers**: a new backend point, `chat.roundFollower` (`conversation.RoundFollowers`). After a successful reply (and its title), each follower runs on a context the browser cannot cancel, and what it did is a `followed` SSE event (`{follower, summary, data}`). A follower can also hear of a changed verdict (`Feedback`). Chronicle's memory update is the first.
- **The transcript**: Chronicle chips above the Sources row (each opens its entry), the memory update's line under the reply, and a *Why* column in the Context inspector. A citation of an entry opens the entry editor.
- **The ⚙ popover** gains the *Chronicles* control (`chronicle`), first, and RAG Search a *Chronicles* source switch.

## Built in slice 8
Tool calling is in [Tools.md › Built in slice 8](Tools.md#built-in-slice-8). What changed in Chat:
- **The reply is the tool loop**: `Send` runs `tools.Loop`, which with no tools is the one streamed call it was. Each step's text streams; steps are joined by a blank line.
- **The ⚙ popover** gains the **Tools** switch (`tools`), on by default. While it is on, the browser declares its client tools with each message and the system prompt gains a short *Tools* section.
- **The transcript**: a card per tool call, above the reply. A write waits on its card for *Approve*, *Always allow here* or *Deny*, and *Ask you to choose* waits for an answer. A tool's passages join the Sources row with the next citation numbers.
- **The SSE stream** gains `tool` events and may send `sources` again. `POST /conversation/answer` answers a waiting call, and `rounds` returns each round's `toolCalls`.
- **History**: past rounds are still sent as their message and reply text only, without their tool calls.

## Debugging a round's AI calls
- **The ⚙ popover's Debug checkbox** (`debug`, last, off by default) is a per-conversation setting like the Tools switch: `conversationsettings` registers a `debug` `chat.contextProvider` with no `Gather`, so its default is `chat.context.defaults.debug` (Settings › AI › Chat › Context › Debug), and the browser's control is `editors/chat/DebugControl.tsx`. Unlike the other providers' `enabled`, it is off unless set to `true`. While it is on, `Send` sets `features.Meta.Debug` on every call the round makes: the context providers' (Chronicle's match), the reply's tool loop, the title and the round followers' (the memory update).
- **What it logs**, at `info` and `warn` so the default `--log-level` shows it, under the `ai` domain:
  - `chatservice` (tag `chat`): each call's feature, level, model, temperature, max tokens, thinking budget, message roles, tools and JSON schema size; and a failure at `warn`, which `Fail` otherwise logs at `debug` for a 4xx.
  - the Gemini provider (tag `gemini`): the request config as JSON (without the system instruction's text, only its size) and each content's shape (`user[text:120]`, `model[call:kb_search+signature]`); a refusal's HTTP status, gRPC status and `details`; and a reply's finish reason, tokens, size and tool calls.
- Prompts and replies are never logged, as in [Usage.md](Usage.md); `usage.capture` is the way to see them.

## Since slice 13
`conversation.send` is a call on the live connection ([Live.md](Live.md)) rather than `POST /conversation/send` with Server-Sent Events. Its events (`started`, `sources`, `delta`, `tool`, `done`, `title`, `followed`) and their data are the same, and a message refused before its round is stored ends the call with its status. `Conversation.send` keeps its signature, and aborting its signal cancels the call, which stops the reply. `POST /conversation/answer` stays REST.

## Changed after slice 43
`conversationimpl.New(opts...)` makes a conversation service apart from the `CreateService` singleton, so Forge's golden tests (part 6h, [forge GoldenTests.md](../../forge/documentation/GoldenTests.md)) run a whole Builder round, through the real tool loop and Gemini provider, on a stack of their own per test.

## Built in slice 43
Forge's Builder mode, part 6g ([forge 06g-BuilderMode.md](../../forge/documentation/plans/06g-BuilderMode.md), [forge BuilderMode.md](../../forge/documentation/BuilderMode.md)), needed chat to run in a mode before chat modes exist, rounds long enough to build, and a round that survives a busy provider. Built in Foundation:
- **A context provider's mode.** `conversation.ContextProvider.Mode(GatherArgs) (*Mode, error)` runs beside `Gather`; a `conversation.Mode` is `{ID, Title, Instructions, MaxSteps, Tools}`. When providers give one, the first in `Order` wins: its instructions go in the system prompt as `# Mode: <Title>` after the `chat` section's prompt and before the context (so they read as instructions, unlike context items, which are data), `MaxSteps` replaces the `tools` section's steps for the round, and `Tools` (names, or prefixes ending in `.`; `Mode.Offers`) narrows the tools offered. The round stores its mode (`Round.Mode`, DTO `mode`) and the transcript shows it as a chip. Forge's `builder` provider is the only one; Foundation's chat modes, which replace the hook, are [forge 06i-ChatModes.md](../../forge/documentation/plans/06i-ChatModes.md).
- **`chat.roundEnd`** (`conversation.RoundEnds`, declared in `serve.go`): `RoundEnd{End(args, round)}` runs once a round ends however it ends (done, failed, stopped), after its `done` event, on a context the browser cannot cancel. `apps/tools` clears the Builder's presence with it.
- **The `retry` event** (`conversationservice.EventRetry`, DTO `SendRetry {try, waitMs, status}`, `Conversation.send`'s `onRetry`): the provider was busy and the step is tried again after a wait (the tool loop's retry, [Tools.md › Changed in slice 43](Tools.md#changed-in-slice-43)). The transcript shows *The model is busy (503); trying again in 2 s (try 1)…* while the round streams.
- **Links that open subjects.** A reply's `workbench:<type>/<id>?<where>` link opens the subject in its editor on a plain click ([WorkbenchContext.md › Changed in slice 43](WorkbenchContext.md#changed-in-slice-43)): `livePreview({onLink})`, `MarkdownText`'s `onLink`, and `RoundActions.onOpenLink`, which the Chat editor gives.

### Decisions
1. **A mode hook, not chat modes** (forge 06 question 1, answered 2026-11-08): a context provider can only add data, so it gains `Mode`; the composer has no mode picker, and a round is in a mode only while a provider says so. Chat modes are planned in forge 06i.
2. **One mode per round**: the first provider in `Order` that gives one; modes do not combine.
3. **Instructions before the context**: the mode's text follows the system prompt, and the *Tools* section still comes last.
4. **`chat.roundEnd` beside the followers**: followers run only after a successful reply, and presence must go however the round ends.

### Verification of slice 43
- Go: `TestSendRunsInAProvidersMode` (the mode's instructions in the system prompt, its tools only, its steps forcing the answer on the third call, `Round.Mode`, `chat.roundEnd` called once with the round).
- In headless Chromium against Forge's `serve` and a stub Gemini server: the Builder chip on the round, the retry note, a summary's links opening the Prefab Editor and the Data Designer ([forge BuilderMode.md › Verification](../../forge/documentation/BuilderMode.md#verification)).

### Decisions to review after seeing it
- The mode chip and the retry note in the transcript: their place and wording.
- A mode chosen per round from the workbench, rather than kept by the conversation (forge 06i question 1).

## Built in slice 44
Forge's chat modes, part 6i ([forge 06i-ChatModes.md](../../forge/documentation/plans/06i-ChatModes.md), [forge ChatModes.md](../../forge/documentation/ChatModes.md)), with the request that nothing about a round be hard-coded: **a round is its mode graph**. `Send` stores the round, resolves its mode and runs the mode's published graph (an editable built-in until a tenant edits it) from `On user message`; the graph gathers the context, builds the prompt and tools with Go templates, streams the reply, names the conversation and follows the reply up. Slice 43's mode hook, `RoundFollower.Follow` and the Go title and prompt code are gone. The slice 43 decisions above are superseded.
- **`chat.mode`** (`conversation.Modes`, declared in `serve.go`): `ModeEntry{ID, Label, Icon, GraphRef, Default, Focus{Kinds, Subjects}}`. `conversationsettings.Contribute` builds the **`chat.modes`** section's defaults from them (`{modes: [{id, label, icon, graphRef, default, focus}]}`, app, tenant and user scopes; Settings › AI › Chat modes), so a tenant or user can add modes (any `mode` graph) or drop them. Chronicle contributes *Personal* (🧠, default) and Forge the *Builder* (🛠️, focused on the Forge editors).
- **`Send`**: `SendArgs.Mode` (DTO `mode`) picks the mode, else the conversation's (`Conversation.Mode`, DTO `mode`, kept as last sent), else the default (400 `unknown_mode`, 503 `no_chat_modes`, 503 `chat_not_configured` without `WithEngine`). The run's inputs are `On user message`'s pins (`text`, `window`, `settings` by provider id, `workbench`, `conversation`, `title`, `level`, `round`); its host is the round (`conversation.Host`): `Turn`, `Settings`, `Gather`, `AddContext`, `ServerTools`/`ClientTools`, `Reply`, `Say`, `SetTitle`, `Followed`. The run outlives the browser (`context.WithoutCancel`, a two-hour wall, `engine.RunArgs.Wall`) while the reply itself stops with it. A graph that ends without replying completes the round with what it said (`Say`), else as failed (500, *The chat mode failed: …*, the run's error).
- **The chat nodes** (`conversation/nodes`, category Chat, mode graphs only): *Context settings*, *Gather context* (named providers, enabled ones only), *Add context* (the old prompt builder, its header and citing text templates; stores the segments and sends `sources`), *Messages* and *Add message* (`Message[]`), *Stream reply* (the tool loop, streamed, with the gate and citations; leaves by *Replied* or *Failed*, gives `reply`, `calls`, `round`, `segments`), *Say*, *Set title* (cleans the title) and *Emit followed*.
- **Context providers** now only declare settings and may gather: the `chronicle` provider has no `Gather` (Personal's nodes read its settings), and `chat.roundFollower` keeps only `Feedback`.
- **`GET /conversation/modes`** (`conversationservice.Modes`, `Conversations.modes()`): the `chat.modes` with each graph's description, its `settings` (the mode kind's spec, `cognition.ModeSpec`) and `missing`.
- **The composer** (`components/chat/ModePicker.tsx`, `modes.ts`): a mode select beside the level, showing the modes whose focus matches the editor the user was in (and the conversation's own); a new conversation starts in the first mode whose focus matches, else the default; a conversation keeps its mode. The ⚙ shows only the context settings the mode's graph lists.

### Decisions
1. **Offered by focus, chosen for new conversations** (forge 06i question 1): a mode with a focus is offered only from a matching editor and picked for a new conversation started there; an existing conversation keeps its mode until switched. Switching changes the next rounds' graph, not the history.
2. **Nothing hard-coded** (forge 06i question 3): the round's pipeline is the graph; `Send` keeps storage, the stream, approvals and accounting. The built-in graphs are editable built-ins (seeded on first edit), not seeded into each tenant.
3. **The run outlives the browser**, the reply does not: the title and memory update still run after *Stop* only when the reply succeeded, since the graph follows *Replied*.
4. **Round accounting counts *Stream reply*'s calls**, as before: other model calls (stage 1, the title, the memory update) are attributed to the round and the run's node in usage, not added to the round's tokens.

### Verification of slice 44
- Go: `TestSendRunsTheConversationsModeGraph` (the default mode's tools and prompt; switching to a builder-like mode narrows the tools, adds its instructions, forces the answer at its steps and keeps the history; the conversation keeps the mode; an unknown mode is 400), `TestAFailingModeFailsItsRound`, `TestModesListsTheModesGraphs`, and the earlier Send tests on a plain test mode graph; Chronicle's `TestPersonalRunsARound` and `TestPersonalWithMemoryOff` (the whole round through the Personal graph), `TestPersonalSeedsTheDefaultPrompts`.
- Forge: the builder package's `TestTheBuilderModeBuildsWithTheAgentGraph`, and every golden test passing in the Builder mode with the provider removed.
- In headless Chromium against Forge's `serve`: from the Data Designer the composer shows *🛠️ Builder*, the menu lists Personal and Builder with their graphs' descriptions, and the ⚙ hides Chronicles in the Builder mode. No round was sent against a model.

### Decisions to review after seeing it
- The mode select: its place beside the level, the descriptions in its menu, and hiding it where only one mode is offered.
- Builder rounds no longer use Chronicle memory (its graph gathers the workbench and Volumes only).
