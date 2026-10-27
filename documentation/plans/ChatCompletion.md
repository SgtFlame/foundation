# Chat: what remains

AgentOS › Chat is built (conversations, levels, context providers, RAG Search, citations, Chronicle memory, the tool loop); see [ChatCompletion.md](../ChatCompletion.md). This plan holds the Markdown Editor's Chat tab, modes and the rest of the transcript, and the TODO.

## The Markdown Editor's Chat tab
A fourth view beside Live Preview, Source and WYSIWYG, for asking about the open document. It is the first step toward editing the document with the AI.
- **Tab**: `MarkdownEditor.tsx`'s nested layout gains `{ name: 'Chat', component: 'chat' }`, lazy-loaded as `chat/ChatView.tsx`.
  - It reads the session's live `Y.Text`, so answers include unsaved edits and other users' edits.
  - It can be docked beside the document.
- **Components**: it uses the same `ChatTranscript`, `ChatComposer`, `LevelToggle` and `ChatSettingsPopover` (`src/components/chat/`). Contributions are filtered with `ctx.surface === 'document'`.
- **Context**
  - **Small documents**: the document is sent whole, with line numbers, up to about 200 KB.
  - **Larger documents**: its best chunks are sent instead, through `Index.find` with a new `Path` filter on `FindArgs`.
  - **Pinned**: the open document is a pinned item, ahead of retrieval's segments.
  - **System prompt**: it names the file and says to cite line ranges.
  - **Workbench context**: the Context control is forced on, with the `document` surface ([WorkbenchContext.md](WorkbenchContext.md)).
- **Navigation**: line citations (`L12–L18`) move the Live Preview or Source cursor to those lines.
- **Persistence**: the transcript is per session in v1 (see question 2).
- **Usage**: rounds are recorded with `Feature = 'doc-chat'` and the file id.

### Later: editing the document with the AI
- **Reply format**: the model returns structured edits through `JSONSchema`, as `{ explanation, edits: [{ find, replace }] }`. Anchoring on exact text survives concurrent edits better than offsets. (The `kb.edit` tool already applies find/replace edits through the open document's writer; see [Tools.md](../Tools.md).)
- **Applying edits**: they go to the shared `Y.Text` as a diff-match-patch patch, as `wysiwyg/yTextBridge.ts` does. They merge with collaborators' edits.
- **Review and undo**: proposed edits show as a diff to accept or reject, and each applied edit is one undo step.

## Modes
- A mode is a published Cognition `mode` graph ([Cognition.md](Cognition.md)) plus a label and icon, listed in the `chat.modes` configuration. *Personal* is the built-in graph that runs the Chronicle pipeline. Agents built in the Cognition Editor can be chosen as modes too.
- The conversation's mode shows as a chip above the composer; clicking it switches mode, and ✕ returns to the default.
- `Send` then runs the conversation's mode graph from `On user message`, and its SSE events come from the graph's `Stream reply` and `Emit …` nodes. A mode graph declares which context settings it uses, and the composer hides the others.

## The rest of the transcript
- **Editing a user bubble**: editing regenerates the reply from that round; later rounds are kept but marked stale.
- **Chronicle chip corrections**: ✕ on a chip records a correction (`RoundSegment`), and the next round excludes that entry.
- **Advanced sampling overrides**: Temperature, Top-P and sampling Top-K behind "Advanced" in the RAG Search popover, defaulting from the level.
- **The Volumes side view**: the volume browser as a `workbench.sideView`, beside Chronicles (Volumes is an editor today).
- **Conversation history**: renaming and deleting conversations, and jumping to an earlier conversation (so the Dashboard's costliest conversations can link to Chat).

## TODO
### Open questions
1. **Document chats**: are they persisted, and can a useful answer be promoted into a Chronicle entry? *Decision*:
2. **Manuscript**: a third side view for long-form writing. Is it in scope for later? *Decision*:
3. **Auto level routing**: choosing Fast or Deep automatically from the question. This could be a later contributor. *Decision*:
4. **"Top-K": sampling, context, or both?** The retrieval article uses "Top-K" both as a sampling setting (next to Temperature and Top-P) and as the number of context segments.
   - *Proposal*: **Segments** (context top-K, default 8) in the main popover; Temperature, Top-P and Top-K sampling under "Advanced", defaulting from the level.
   - *Decision*: (after seeing) slice 5 built **Segments** as the popover's Top-K (1…50, default 8). The Advanced sampling overrides are not built yet.

(Earlier numbering: these were gaps 2, 3, 4 and 6; gap 1, tool calling, and gap 5, default models per level, are decided and recorded in [the documentation](../ChatCompletion.md#decisions).)

### Review after seeing it ★
- **The transcript, composer and level toggle** with a real model (slice 4). The build environment had no Gemini API key, so these were never seen: a streamed reply rendered by `MarkdownText` as it grows; Stop during a real reply; the generated title arriving after `done`; the hover footer with real tokens and cost; the level toggle's cost estimate against real prices; a captured call in DevTools › Usage. Set a key in Settings › AI › Providers, then review.
- **The decisions made while building**, in [ChatCompletion.md › Decisions to review after seeing it](../ChatCompletion.md#decisions-to-review-after-seeing-it).

### Not built yet
- The Markdown Editor's Chat tab, and editing the document with the AI (above).
- Modes and `chat.modes` (above, and [Cognition.md](Cognition.md)).
- Editing a user bubble; Chronicle chip corrections; the Advanced sampling overrides; renaming and deleting conversations.
- The Volumes side view, and the command palette for `workbench.command` ([Contributors.md](Contributors.md)).
- Storing the memory update's `followed` line with its round, so it survives a reload ([Chronicle.md](Chronicle.md)).
- Refreshing the session from the `fetch` streaming clients on a 401 ([MultiTenant.md](MultiTenant.md)).
- Replaying past rounds' tool calls to the model (today only their reply text is sent).

### Verification, once the Chat tab and the rest are built
1. `yarn build` and `yarn lint` pass. Run `backend/scripts/serve.sh` and `yarn dev`.
2. **Chat**: messages stream, and Stop aborts; citations open the cited file at the right lines; removing a Chronicle chip excludes that entry next round; a 🚫 round no longer influences later turns; 👍/👎 change the grounding multipliers on the next similar question; moving the Chronicles ↔ Library slider visibly shifts the cited sources.
3. **Levels**: switching Fast/Deep changes the model in the round footer, and the Dashboard's cost reflects it.
4. **Settings**: changing the RAG default changes the popover's initial value for new conversations; adding a level in `ai.chat.levels` adds a button to the toggle.
5. **Document chat**: open a note's **Chat** tab and ask about it. Edit the note, ask again, and the answer reflects the unsaved edit.
6. **No API key**: both UIs show the 503 message.
