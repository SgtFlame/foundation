# Chat: the AgentOS Chat editor

AgentOS › Chat is a chat editor for general conversations with long-term memory through Chronicle ([Chronicle.md](Chronicle.md)), built on the provider-neutral `chatservice` and the hybrid search index ([AIChatEmbed.md](AIChatEmbed.md)). It is extensible through extension points ([Contributors.md](Contributors.md)):
- the composer's ⚙ settings come from `chat.contextSettings` contributions, each paired with a server `chat.contextProvider`;
- levels and defaults come from configuration ([Configuration.md](Configuration.md));
- every call's tokens and cost are recorded for the Dashboard ([Usage.md](Usage.md)).

Terminology: memory entries are **Chronicles**, and knowledge base collections are **Volumes**.

What is built, slice by slice: slice 4 built Chat, thin (conversations and rounds, the levels, context providers and the composer); slice 5 added RAG Search, inline citations, the Sources row, the Context inspector and feedback; slice 6 added Chronicle memory (the window, stage 1, Chronicle chips and the memory update); slice 8 made the reply the tool loop. What remains, including the Markdown Editor's Chat tab, is in [plans/ChatCompletion.md](plans/ChatCompletion.md).

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
