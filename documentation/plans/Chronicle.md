# Chronicle memory: what remains

Entries, the Chronicles side view, the entry editor, stage 1 and the memory update (run as the built-in *Personal* graph), and sharing are built; see [Chronicle.md](../Chronicle.md). This plan holds custom types, the graph view, the remaining round controls, and the TODO.

## Custom types and System-1 processing
- Users define their own entry types, each with **summarizing modes** that control how its entries are created and updated.
- A summarizing mode is a **blueprint execution graph**, a visual logic graph like Unreal Engine Blueprints. In Foundation these are Cognition `summarizer` graphs ([Cognition.md](Cognition.md)), edited in the Cognition Editor (the **type designer**).
- The blueprint is deterministic "System 2" flow that orchestrates fast, template-driven "System 1" LLM calls. Those calls extract facts, format front matter and assign graph relationships.
- **The memory update** then runs, for each affected type, its summarizing mode to decide whether to create or update entries, with System-1 `Complete` calls under JSON schemas. With no blueprint, the default mode (today's `Update`) updates the used entries and proposes new ones.
- **Storage**: built-in types are code today (`chronicle.Types`). Custom types need an `EntryType` table (`Name`, `Builtin`, `Blueprint` JSON) holding their summarizing-mode graph.

## The graph view
The Chronicles side view shows the memory as a tree. A **network** (graph) view, with `react-flow` (already in the stack), shows entries and their `related` links, with the same checkboxes.

## Rounds
- **Editing a past prompt** and **deleting a whole round** (a soft delete; the window skips deleted rounds).
- **Correcting stage 1 from a chip**: ✕ on a Chronicle chip excludes the entry next round, recorded as a correction.
- **Undoing a whole round's memory changes** at once, rather than restoring summaries entry by entry.
- **Correct selections as few-shot examples** for stage 1, later.

## The first mapping onto Foundation
The data model and services as first planned, kept for reference; slice 6 built them differently ([Chronicle.md › Built in slice 6](../Chronicle.md#built-in-slice-6)).

| Table | Fields | Notes |
|---|---|---|
| `Conversation` | `Title`, `WindowSize` (15), `TopK` (5), `Model` | one Chat editor tab |
| `Round` | `ConversationID`, `Seq`, `UserMessage`, `AssistantMessage`, `Feedback`, `EditedAt`, `DeletedAt` (soft delete) | the window is the last `WindowSize` rounds not `ignored` and not deleted |
| `RoundSegment` | `RoundID`, `SourceID`, `SegmentKey`, `Rank`, `Score`, `Pinned` | every segment a round injected |
| `SegmentFeedback` | `SourceID`, `SegmentKey`, `Up`, `Down` | 👍/👎 counters behind the grounding multiplier |
| `EntryType` | `Name`, `Builtin`, `Blueprint` (JSON) | still planned, for custom types |
| `ContextEntry` | `TypeID`, `ShortSummary`, `FullSummary`, `FullContext`, `Fields` | built as `Entry` |
| `EntryLink` | `FromID`, `ToID`, `Kind` (`parent`, `related`, …) | built with `related` only; parents are a column |
| `EntryRound` | `EntryID`, `RoundID`, `Action` | built |
| `ActiveEntry` | `ConversationID`, `EntryID`, `Source` | not built: checked entries are conversation settings |

- **`chronicleservice`** was to have `CreateConversation`, `Send`, `EditRound`, `DeleteRound`, `SetFeedback` and `ListRounds`; chat's `conversationservice` holds them instead, and `EditRound` and `DeleteRound` are still to build.
- **`memoryservice`** was to have `ListEntries` (as a tree or a graph), `GetEntry`, `CreateEntry`, `UpdateEntry`, `LinkEntries`, `SetActive`, `ListTypes` and `SaveType`; `chronicleservice` holds the built ones, and `ListTypes` for custom types and `SaveType` are still to build.

## TODO
### Open questions
1. **Volumes as memory**: should knowledge base volumes be memory too? For example, a markdown or wiki entry could point at a volume file, with its context fetched through `indexservice.Find`.
   - *Proposal*: not as entries. RAG Search already searches Volumes and Chronicles together, weighed by the Chronicles ↔ Library dial, so a Volume reaches the reply without being copied into memory.
   - *Decision*: (after seeing) slice 6 built the proposal: no entry points at a Volume file.

### Review after seeing it ★
- **The side view and the entry editor with a real model** (slice 6). The build environment had no Gemini API key; a stub picked the first candidates for stage 1 and canned the memory update, so how a real model chooses entries, rewrites summaries and proposes entries is unseen. Set a key in Settings › AI › Providers, chat for a while, then review the Chronicles view, the proposals and the entry editor.
- **The decisions made while building**, in [Chronicle.md › Decisions to review after seeing it](../Chronicle.md#decisions-to-review-after-seeing-it), and those about sharing in [MultiTenant.md › Built in slice 11](../MultiTenant.md#decisions-to-review-after-seeing-it-1).

### Not built yet
- Custom types with their summarizing modes, and the `EntryType` table (above).
- The graph (network) view.
- Correcting stage 1 from a chip; editing a past prompt and deleting a round; undoing a round's memory changes at once.
- **Saving checks at once**: a checkbox changes the conversation's settings in the browser, and they are stored with the next message, so checks made before a reload with no message since are lost.
- **Showing the memory update's line after a reload**: the `followed` event is not stored (the entry editor's rounds list still shows every change).
- **Sharing**: flagged entries in the moderation queue (it holds global shares only); updating a global copy when its entry changes; sharing an entry's children with it.
- Caching the query embedding that stage 1's candidate retrieval and RAG Search each make ([ContextRetrieval.md](ContextRetrieval.md)).
