# Tools: function calling for chat and agents

## Context
The chat composer's **Tools** switch ([ChatCompletion.md](ChatCompletion.md)) and Cognition's `Agent` node ([Cognition.md](Cognition.md)) both need the model to call functions: search the KB, read a file, update a Chronicle entry, write to a Nexus stream.

Built in slice 8: tool calling in `chatservice`, `tools.Loop`, the first server tools, confirmation cards, the client tools and their pause and resume, and the `Agent` node. MCP, web tools, tenant policies, the Nexus tools and the rest are in [plans/Tools.md](plans/Tools.md).

## Decisions
1. **The first tools**: all the candidates except Nexus's: `kb.search`, `kb.read`, `kb.createNote`, `kb.edit`, `chronicle.search`, `chronicle.getEntry`, `chronicle.updateEntry` and `time.now`, plus the client tools `workbench.getState`, `workbench.open` and `chat.askChoice`.
2. **Confirmation is per call**: each `write` or `external` call shows a card (*Approve*, *Deny*, *Always allow for this conversation*). Unattended runs with pre-approved write tools wait for background runs and RBAC policies.

Question 3 (citing tool results, built on its proposal) and question 4 (MCP) are open; see [plans/Tools.md](plans/Tools.md).

## Tool definition
Tools are a backend extension point, `tools.tool` (`tools.Tools`, [Contributors.md](Contributors.md)). A tool is a struct with function fields, like Cognition's node types:

```go
type Tool struct {
    extensions.Descriptor            // ID: the tool's name, e.g. kb.search
    Title       string               // the card's name, e.g. "Search Volumes"
    Description string               // for the model
    Input       json.RawMessage      // the arguments' JSON Schema
    Effect      Effect               // none | read | write | external | ui
    Location    Location             // server | client
    Wait        time.Duration        // client tools: how long to wait; WaitForUser waits for the user
    Run         func(call Call) (Result, error)
}
```

- **`name`**: dotted and stable, for example `kb.search` or `chronicle.updateEntry`. It is sent to the model with `.` as `_` (`kb_search`), which every provider allows.
- **`Effect`** drives confirmation. `none` and `read` run directly; `write` and `external` ask the user unless the conversation allows them. `ui` (client tools only) changes only what the user sees and runs directly.
- **Location**: `server`, or `client` for tools that run in the browser and return their result through `POST /conversation/answer` ([WorkbenchContext.md](WorkbenchContext.md)). The loop pauses for a client tool the same way it pauses for a confirmation.
- **`Call`**: the arguments (checked against `Input` first), plus the `ServiceArgs` (tenant and user) with `Meta` naming the round or run, and `Feature` set to `tool.<name>`.
- **`Result`**: `Content`, a JSON object for the model; `Summary`, one line of markdown for the card; and `Segments`, which make the result citable.
- **Per-call limits**: a timeout, and a maximum result size (a larger result is cut, with a note to the model), both in the `tools` section.

## Provider support
- **`chatservice`**:
  - `CompleteArgs` has `Tools []ToolSpec` and `ToolChoice` (`""` auto, `none`, `required`).
  - `Message` has assistant `ToolCalls` and a `tool` role carrying `Results`.
  - The response's `Message.ToolCalls` are the calls the model asked for. Tool calls are not streamed: each step's text streams, and its calls come whole at the end.
- **Provider mappings**:
  - Gemini: `FunctionDeclaration` (with `ParametersJsonSchema`), `FunctionCall` and `FunctionResponse`, with each call's thought signature sent back with it. Built.
  - The service stays provider-neutral.

## The tool loop
- **Where it lives**: one implementation, `tools.Loop`, run by chat's `Send` and by Cognition's `Agent` node.
  1. Call the model with the allowed tools.
  2. Run the returned calls. Consecutive calls that neither ask nor run in the browser run in parallel; the rest run one at a time.
  3. Append the results and repeat. The loop stops when the model answers without calls. After `maxSteps` steps with calls, the model is called once more with `ToolChoice: none` and must answer.
- **What it records**: every call is a `ToolCall` record (tool, arguments, effect, status, approval, summary, result, duration, citations), linked to the round or the Cognition run and node. It is stored when the call ends.

## Permissions and confirmation
- **The switch**: the composer's ⚙ **Tools** control turns tools off for a conversation. A mode or agent graph lists the tools it may use (the `Agent` node's `tools`).
- **Confirmation**: `write` and `external` calls pause the loop and show a confirmation card in the transcript, with the arguments rendered (an edit as the text it replaces and the text that replaces it). The choices are *Approve*, *Always allow here* (for the rest of the conversation) and *Deny*.
- **Policy**: the `tools` section's `disabled` list turns tools off for everyone (app or tenant scope).

## The first tools
| Tool | Effect | Backed by |
|---|---|---|
| `kb.search` | read | `retrievalservice.Retrieve` over the `library` source; cites its passages |
| `kb.read` | read | a Volume file, or a line range (at most 400 lines); cites what it read |
| `kb.createNote` | write | `volumeservice.CreateFile` with its content |
| `kb.edit` | write | find/replace edits through `collabservice.EditFile`: applied to the open document by its writer, else to the file |
| `chronicle.search` / `chronicle.getEntry` | read | `retrievalservice.Retrieve` over the `chronicle` source; `chronicleservice.Get` |
| `chronicle.updateEntry` | write | `chronicleservice.Update`, recording the round with the summaries it replaced |
| `time.now` | none | – |
| `workbench.getState`, `workbench.open`, `chat.askChoice` | read, ui, ui | client tools ([WorkbenchContext.md](WorkbenchContext.md)) |

## Built in slice 8
**Backend**
- **`chatservice`** (`features/ai/services/chatservice`): `ToolSpec`, `ToolCall` (with the provider's opaque `Signature`), `ToolResult`, the `tool` role and `ToolChoice`.
  - The last message may be the user's or tool results, and tool results must follow an assistant message with calls.
  - Gemini maps them as above. The usage debug capture keeps each message's calls and results, and the reply's calls.
- **`features/tools`**:
  - `Tool.go`: the point and the types.
  - `Args.go`: `CheckArgs`, a small JSON Schema check (types, required, enums, lengths, ranges) whose message tells the model what to correct; `Decode[T]`.
  - `Models.go`: the `ToolCall` table (tenanted and owned).
  - `Loop.go`: `Loop{DB, Chat}.Run(RunArgs)` returns an `Outcome` with the reply (every step's text, joined by a blank line), the calls, and one `Usage` per model call. A stopped or failed step keeps the text it streamed. A tool with no `Run`, or one that panics, fails its call, not the round.
- **The `Gate`** pauses a call. `Approve` waits for the user and `RunClient` for the browser.
  - Chat's gate lives in `conversationservice`: a call that pauses is registered (by round and call ID, with its owner) before its `tool` event is sent. `Answer` hands it the approval or result.
  - Approval has no timeout; it ends when the round is stopped, which denies the call. A client tool waits `clientTimeoutSeconds` (30), or until stopped for `chat.askChoice`.
  - The waiting calls are held in memory, so a round must be answered on the server that runs it.
  - A run without a gate (the `Agent` node) denies writes and has no client tools.
- **`tools/settings`** (`toolssettings`): the `tools` section (AI › Tools, app and tenant scope): `maxSteps` (8), `maxResultKB` (32), `timeoutSeconds` (30), `clientTimeoutSeconds` (30) and `disabled`.
- **`tools/context`** (`toolscontext`): the `tools` chat.contextProvider. It gathers nothing; its `enabled` setting (on by default) is the conversation's Tools switch, with its defaults section `chat.context.defaults.tools`.
- **`tools/builtin`** (`toolsbuiltin`): `time.now`.
- **`kb/tools`** (`kbtools`) and **`chronicle/tools`** (`chronicletools`): the first tools, each taking a function that returns its services (the late binding of `serve.go`).
  - **Paths** are relative to the knowledge base root with `/` (`veritas/notes.md`), as citations name them, and must lie inside a Volume.
  - **`kb.edit`**: each `find` must occur exactly once, so an edit never lands in the wrong place; edits apply in order.
    - `collabservice.EditFile` is Go-only (no route). While the file is open, it sends the room's writer a new `edit` control, and the browser applies the edits to the shared `Y.Text` (`editors/markdown/collab/edits.ts`) and answers `edited`; otherwise it edits the file atomically.
    - An empty room waiting to be dropped is dropped first.
  - **`volumeservice.CreateFile`** takes `Content`.
  - **`chronicleservice.UpdateArgs`** takes `RoundID`: the round is linked to the entry with the summaries before, so *Restore earlier summaries* and 🚫 flagging work for tool edits too.
- **Chat** (`conversationservice.Send`): the reply is always the loop; with no tools, that is one streamed call.
  - The tools offered are the server's and the browser's declared client tools, less `disabled`, while the switch is on. The system prompt gains a short *Tools* section.
  - **Citations from tools**: a result's segments are numbered after the round's context (a segment already cited keeps its number), stored as `RoundSegment`s, and sent in a new `sources` event.
  - The round's tokens and cost sum every step, each priced on its own.
  - *Always allow here* is stored on the conversation (`Conversation.AllowedTools`).
  - REST: the SSE stream gains `tool` events (a `ToolCall` each time its status changes: `running`, `awaiting`, `client`, then `done`, `failed`, `denied` or `timeout`); `POST /conversation/answer {roundId, callId, approval | result | error}`; `send` takes `clientTools`; `rounds` returns each round's `toolCalls`.
- **The `Agent` node** (`tools/nodes`, `toolsnodes`, `llm.agent`, category LLM): config `level` and `tools`, inputs `prompt` and `system`, outputs `reply` and `calls`.
  - With no tools listed it offers every tool that does not ask. A write listed by name is offered and denied, so the trace says why.
  - Its calls carry the run's `RunID` and `NodeID`, and `Meta.Feature` `agent`.

**Frontend**
- **The client**: `Conversation.ts` has `ToolCall`, `onTool`, `Conversations.answer` and `clientTools` on `send`.
- **`chat.clientTool`** (`editors/chat/points.ts`): `{ title, description, input, effect: 'read' | 'ui', run?(args, ctx), Card? }`. A tool with a `Card` waits for the user there instead of running. `ctx` is `{ registry, store, tabId }`.
- **`src/tools/`**: the ⚙ **Tools** switch, `workbench.getState`, `workbench.open` (a tab, a Volume file with lines, or a Chronicle entry), and `chat.askChoice` with its card (`AskChoiceCard`). `EditorRegistry.focus(tabId)` is new.
- **`ToolCallCard`** (`components/chat/`): one row per call, with its effect, title, summary or status, the citations it added, and its arguments and result behind the chevron.
  - A write awaiting approval shows its arguments in full, an edit as removed and added blocks, with *Approve*, *Always allow here* and *Deny*.
  - `useChatStream` runs client tools without a card and answers for them, and marks waiting calls stopped when the user stops. The transcript follows tool calls as it follows the reply.
- **Edits by a tool** (`'tool'` origin) do not count as the user's edits in the workbench context.

**Verification**
- **Tests**:
  - `tools/Loop_test.go`: the loop, argument checks, approval, always-allow, client calls and their timeout, citations, max steps, cut results, broken tools;
  - `tools/nodes`: the Agent;
  - `conversationservice/impl/Tools_test.go`: `Send` end to end with approval, a client call, citations, another user's answer refused, and the switch;
  - `collabservice`: `ApplyEdits` and `EditFile` on disk and through a writer.
- **In a browser**: seen in headless Chromium, light and dark, against a local stand-in for the Gemini API that calls tools. Flows checked:
  - search with its citation;
  - edit with the approval card, then approved;
  - *Ask you to choose*, answered;
  - `workbench.open` opening the file;
  - an edit while the file was open, applied live by its writer and saved;
  - a note denied.

  No browser errors.

### Decisions to review after seeing it
1. **One event and one answer.** A `tool` event per status change, and one `POST /conversation/answer` for approvals and client results. The plan had a `clientToolCall` event and `POST /ai/chat/rounds/:roundId/tool-results`.
2. **Client tools are declared with each message**, not once per session. The server accepts only `read` and `ui` effects and at most 20.
3. **Citing tool results** (Tools 3): built on its proposal, numbered after the context.
4. **`kb.search` and `chronicle.search` use retrieval** with one source, not `indexservice.Find`, so they get grounding, merging and citations.
5. **`kb.edit` is find/replace with a unique match**, through the open document's writer when there is one. `kb.read` returns at most 400 lines.
6. **The Tools switch is on by default.** It is a context provider with no `Gather`, so it has a ⚙ control and a defaults section for free.
7. **Approval waits until the round is stopped**, with no timeout.
8. **Past rounds are sent as their reply text only**: their tool calls and results are not replayed to the model.
9. **The cards sit above the reply text**, not between each step's text.
10. **`workbench.open` opens in Chat's tab set**, hiding the chat, as a citation click does.
11. **The Agent node leaves out writes** unless they are listed, and then denies them.
