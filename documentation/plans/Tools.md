# Tools: what remains

Tool calling in `chatservice`, `tools.Loop`, the first server tools, confirmation cards, the client tools and the `Agent` node are built; see [Tools.md](../Tools.md).

## MCP
- **MCP client**: consume external MCP servers as tool sources, configured per tenant, with their tools mapped onto `tools.Tool`.
- **MCP server**: expose Foundation's tools to external agents.
- A **Tools and MCP manager** editor: tenant tool policies and the registry of MCP servers ([Backlog.md](Backlog.md)).

## Web tools
Fetch and search, with an `external` effect and stricter policy.

## Policy
- **Tenant allow and deny rules**, through `tool.execute` and `tool.approveWrite` ([RBAC.md](RBAC.md)); today the `tools` section's `disabled` list turns tools off for everyone.
- **Rate limits** per tool.
- **Unattended pre-approved writes**, for scheduled graphs (question 2's later part), once background runs and RBAC policies exist.

## More tools
| Tool | Effect | Backed by |
|---|---|---|
| `nexus.read` / `nexus.write` | read / write | the Nexus ([Cognition.md](Cognition.md)); the Nexus slice |
| `editor.getSelection` | read | client: the selected text or rows of an editor ([WorkbenchContext.md](WorkbenchContext.md)) |

- Every tool as a Cognition node, and `Wait for user` on the confirmation mechanism.
- Provider mappings for OpenAI and Azure (`tools` / `tool_calls`), with those providers ([AIChatEmbed.md](AIChatEmbed.md)).

## TODO
### Open questions
3. **Should tool results be cited inline like retrieval segments?** *Proposal*, built in slice 8: yes. The passages `kb.search`, `kb.read`, `chronicle.search` and `chronicle.getEntry` return become the round's next citations, after its context. *Decision*: (after seeing) built on the proposal in slice 8.
4. **When is MCP support needed?** *Decision*:

(Questions 1 and 2 are decided; see [Tools.md › Decisions](../Tools.md#decisions).)

### Review after seeing it ★
- **The tool cards, the approval flow and *Ask you to choose* with a real model** (slice 8). Everything was seen against a stand-in for the Gemini API that calls tools from a script, so how a real model chooses tools, phrases arguments, recovers from a failed call and cites tool passages is unseen; Gemini's thought signatures are passed back but untested against the real API. Set a key in Settings › AI › Providers, then try: "find what my notes say about X", "fix the typo in <file>", "add a note about…", and a question about "him" with two people open.
- **The decisions made while building**, in [Tools.md › Decisions to review after seeing it](../Tools.md#decisions-to-review-after-seeing-it).

### Not built yet
- MCP (question 4), web tools, tenant allow and deny rules, rate limits, and unattended pre-approved writes.
- `nexus.read`/`nexus.write`; every tool as a Cognition node; `editor.getSelection` and `workbench.subjectDescriber`.
- **Showing calls elsewhere**: tool calls in Cognition's Log Viewer (the records carry `RunID` and `NodeID`), and a link from a call to its usage records (`Feature` is `tool.<name>`).
- **Robustness**: waiting calls that survive a server restart or run on several servers (today they are held in memory, one server only); streamed tool-call arguments.
- **Seen while verifying**: the lazily loaded *Ask you to choose* card grows after the transcript has scrolled to it, so its last line can sit just below the fold until the next update.
