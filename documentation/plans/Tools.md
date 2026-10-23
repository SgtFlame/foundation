# Tools: function calling for chat and agents

## Context
The chat composer's **Tools** toggle ([ChatCompletion.md](ChatCompletion.md)) and Cognition's `Agent` node ([Cognition.md](Cognition.md)) both need the model to call functions: search the KB, read a file, update a Chronicle entry, write to a Nexus stream. `chatservice` has no tool calling today.

Status: draft. Not fully defined; this collects the shape and the open questions.

## Tool definition
Tools are a backend extension point, `tools.Tool` ([Contributors.md](Contributors.md)):

```go
type Tool interface {
    Meta() ToolMeta // name, description, input JSON Schema, output JSON Schema
    Effect() Effect // none | read | write | external
    Run(ctx context.Context, call Call) (Result, error)
}
```

- **`name`**: dotted and stable, for example `kb.search` or `chronicle.updateEntry`. It is sanitized to the provider's allowed characters when sent.
- **`Effect`** drives confirmation. `none` and `read` run directly; `write` and `external` ask the user unless a policy allows them. `ui` (client tools only) changes only what the user sees and runs directly.
- **Location**: `server`, or `client` for tools that run in the browser and return their result over the chat stream ([WorkbenchContext.md](WorkbenchContext.md)). The loop pauses for a client tool the same way it pauses for a confirmation.
- **`Call`**: the arguments, plus the `ServiceArgs` (tenant and user), and the conversation, run and node references for tracing.
- **`Result`**: JSON for the model, plus optional `Segment`s and citations for the UI.
- **Per-call limits**: a timeout, a maximum result size (results are truncated with a note to the model), and a rate limit.

## Provider support
- **`chatservice`**:
  - `CompleteArgs` gains `Tools []ToolSpec` and `ToolChoice` (`auto` | `none` | `required` | a name).
  - `Message` gains assistant `ToolCalls` and `tool` role results.
  - `CompleteResponse` returns the `ToolCalls` the model requested.
  - Streaming yields tool-call events as well as text deltas.
- **Provider mappings**:
  - Gemini: `FunctionDeclaration` / `FunctionCall` / `FunctionResponse`.
  - OpenAI and Azure: `tools` / `tool_calls`.
  - The service stays provider-neutral.

## The tool loop
- **Where it lives**: one implementation, `tools.Loop`, used by the chat mode graph's `Agent` node.
  1. Call the model with the allowed tools.
  2. Run the returned calls. Independent `read` calls run in parallel.
  3. Append the results and repeat. The loop stops when the model answers without calls, or at `maxSteps`.
- **What it records**: every call is a `ToolCall` record (tool, arguments, result size, effect, duration, status, approval) linked to the round or run, and appears in the Cognition trace.

## Permissions and confirmation
- **Allow-lists**: a mode or agent graph lists the tools it may use. The composer's toggle turns them all off for a conversation.
- **Confirmation**: `write` and `external` calls pause the loop and show a confirmation card in the transcript, with the arguments rendered. The choices are *Approve*, *Deny* and *Always allow for this conversation*. Cognition's `Wait for user` uses the same mechanism.
- **Policy**: tenant-level allow and deny rules, in configuration and [MultiTenant.md](MultiTenant.md).

## Candidate first tools
| Tool | Effect | Backed by |
|---|---|---|
| `kb.search` | read | `indexservice.Find` |
| `kb.read` | read | a volume file, optionally a line range |
| `kb.createNote` | write | `volumeservice.CreateFile` + content |
| `kb.edit` | write | find/replace edits applied to the collab `Y.Text`, as planned for the document chat |
| `chronicle.search` / `chronicle.getEntry` | read | Chronicle |
| `chronicle.updateEntry` | write | Chronicle |
| `nexus.read` / `nexus.write` | read / write | the Nexus ([Cognition.md](Cognition.md)) |
| `time.now` | none | – |

## Later
- **MCP client**: consume external MCP servers as tool sources, configured per tenant, with their tools mapped onto `tools.Tool`.
- **MCP server**: expose Foundation's tools to external agents.
- **Web tools**: fetch and search, with an `external` effect and stricter policy.

## Open questions
1. What are the first tools, beyond KB search and read? *Decision*:
2. Is confirmation per call or per conversation by default, and can an agent run unattended with pre-approved write tools (scheduled graphs)? *Decision*:
3. Should tool results be cited inline like retrieval segments? *Decision*:
4. When is MCP support needed? *Decision*:
