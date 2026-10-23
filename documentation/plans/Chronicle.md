# Chronicle memory system

## Context
**AgentOS › Chat** ([ChatCompletion.md](ChatCompletion.md)) needs memory that outlives one context window. Feeding the model the whole chat history drifts and does not scale. Searching every past message by similarity alone is unpredictable, and the user cannot steer it.

Chronicle, the **Categorized Context Graph Memory (CCGM)** described in [Architecting the Chronicle Memory System](../../../code-and-castles/www/content/code/20260908-chronicle-memory/index.md), replaces both with two things:
- structured memory entries, connected in a graph;
- a deterministic, multi-stage pipeline that decides which entries a turn needs. The user can see and override that choice.

This document records the design and how it maps onto Foundation's services. It is the memory half of the AgentOS Chat plan.

Status: planned, not implemented.

## The design (from the article)

### 1. Active conversation window
- Each turn sees a **window** of the last N rounds (user message + assistant reply). It defaults to **15** and the user can configure it. The window is used to read immediate intent without dragging in the whole history.
- The user can **edit** a past prompt or **delete** a whole round.
- Every round carries **feedback**. This was refined by the follow-up article, "Deterministic Context Control"; see [ContextRetrieval.md](ContextRetrieval.md).
  - **Neutral** (default): recorded normally.
  - 👍 **Correct**: the reply was grounded. The segments it used gain weight in future retrieval.
  - 👎 **Incorrect**: a retrieval or generation failure. The segments it used lose weight and are flagged for review. The round stays in the window, so the user can correct it in the next turn.
  - 🚫 **Ignore**: the pipeline **drops both its user message and its reply** from the sliding window and from the memory-update queue.

### 2. Two-stage pipeline (deterministic, not an autonomous agent)
Each stage is a fixed step with its own system prompt. No open-ended agent loop decides what happens next.

1. **Stage 1: context matching**
   - *Inputs:* the active window, the currently active entities, the entries adjacent to them in the graph, and the entries the user has checked.
   - *Goal:* decide whether the user is continuing the current topic or shifting focus, and select the **top k** relevant entries (configurable, default **5**).
2. **Stage 2: response generation**
   - *Inputs:* the user's prompt plus the top-k entries from stage 1.
   - *Goal:* answer from that knowledge.

### 3. Memory structure: the Categorized Context Graph (CCG)
Entries form a graph the user can browse as a **tree** (hierarchy) or a **network** (graph view).
- **Active checkboxes:** the user can check entries to make them active for the conversation.
- **Parent-child rule:** checking a child also includes its parents, which keeps the foundational context. Unchecked siblings and cross-linked nodes stay dormant unless stage 1 picks them.

**Anatomy of an entry**
- Common fields:
  - **Short summary**: 1–2 sentences, which is what stage 1 reads.
  - **Full summary**: the detailed core facts.
  - **Full context**: the raw, detailed payload.
  - **Linked conversation rounds**: the exact rounds that created or updated the entry.
- **Typed entries**. Predefined types:
  - General-purpose markdown
  - Karpathy-style wikis
  - **Entities:** people, places and things
  - **Timelines:** concrete (dated events) or ordinal-abstract (sequence only), across history, fiction and future planning

### 4. Custom types and System-1 processing
- Users define their own entry types, each with **summarizing modes** that control how its entries are created and updated.
- A summarizing mode is a **blueprint execution graph**, a visual logic graph like Unreal Engine Blueprints. In Foundation these are Cognition `summarizer` graphs ([Cognition.md](Cognition.md)).
- The blueprint is deterministic "System 2" flow that orchestrates fast, template-driven "System 1" LLM calls. Those calls extract facts, format front matter and assign graph relationships.

## Mapping onto Foundation

### Data (SQLite through GORM)
Every table embeds `db.Model`, so primary keys are TSIDs; uint64 IDs travel as JSON strings. All of these tables are migrated by `serve` at startup.

| Table | Fields | Notes |
|---|---|---|
| `Conversation` | `Title`, `WindowSize` (15), `TopK` (5), `Model` | one Chat editor tab |
| `Round` | `ConversationID`, `Seq`, `UserMessage`, `AssistantMessage`, `Feedback` (`neutral`/`correct`/`incorrect`/`ignored`), `EditedAt`, `DeletedAt` (soft delete) | the window is the last `WindowSize` rounds not `ignored` and not deleted |
| `RoundSegment` | `RoundID`, `SourceID`, `SegmentKey`, `Rank`, `Score`, `Pinned` | every segment a round injected (entries, past rounds, library chunks), from [ContextRetrieval.md](ContextRetrieval.md); for entries, `Pinned` marks user-checked ones |
| `SegmentFeedback` | `SourceID`, `SegmentKey`, `Up`, `Down` | 👍/👎 counters behind the grounding multiplier |
| `EntryType` | `Name`, `Builtin`, `Blueprint` (JSON) | the predefined types are seeded rows; custom types carry their summarizing-mode graph |
| `ContextEntry` | `TypeID`, `ShortSummary`, `FullSummary`, `FullContext`, `Fields` (JSON front matter, e.g. a timeline's date or order) | the memory node |
| `EntryLink` | `FromID`, `ToID`, `Kind` (`parent`, `related`, …) | the graph; `parent` edges drive the tree view and the parent-inclusion rule |
| `EntryRound` | `EntryID`, `RoundID`, `Action` (`created`/`updated`) | "linked conversation rounds" |
| `ActiveEntry` | `ConversationID`, `EntryID`, `Source` (`user`/`stage1`) | the checkboxes and the currently active entities |

### Services (a new `chronicle` feature: `backend/features/chronicle/`)
- **`chronicleservice`**: `CreateConversation`, `Send` (async, streams the reply), `EditRound`, `DeleteRound`, `SetFeedback`, `ListRounds`.
- **`memoryservice`**: `ListEntries` (as a tree or a graph), `GetEntry`, `CreateEntry`, `UpdateEntry`, `LinkEntries`, `SetActive` (the checkbox, which applies the parent rule), `ListTypes`, `SaveType`.

**`Send` pipeline**, using `chatservice` and `embedservice` from [AIChatEmbed.md](AIChatEmbed.md).

This pipeline is **not hard-coded**. It is the built-in *Personal* mode graph, run by the Cognition Engine ([Cognition.md](Cognition.md)). Each step below is a node: `Window`, `Retrieve context`, `Match context`, `Stream reply` and `Update entry`. Users copy and change the graph to make new modes. The steps describe what the built-in graph does:
1. Build the window: the last `WindowSize` rounds, excluding ignored and deleted ones.
2. **Retrieve** with the context retrieval engine ([ContextRetrieval.md](ContextRetrieval.md)). It runs vector and BM25 search over the Chronicle sources (entries, past rounds) and the Library (Volumes), fused by RRF and weighted by the user's two axes and by feedback. Its Chronicle-axis results join the **candidates** for stage 1:
   - the active entries
   - their graph neighbours (one hop over `EntryLink`)
   - the user-checked entries, with their parents
   - the Chronicle segments retrieval ranked highest

   That last group keeps stage 1's prompt small when the graph is large. Entries live in a LanceDB table `entries` (vector plus FTS), beside the index's `chunks` table, and are re-embedded when their summaries change. They are kept by hash, as in the index. A mode may skip stage 1 and use retrieval's selection directly.
3. **Stage 1** is `chatservice.Complete`, at the level `ai.chat.stages` names ([Configuration.md](Configuration.md)), with the stage-1 system prompt, the window and the candidates' short summaries. `JSONSchema` constrains the reply to `{ "shift": bool, "entries": [{ "id": string, "rank": int }] }`. The user-checked entries are always kept, and stage 1 fills the rest up to `TopK`. The selection updates `ActiveEntry`, and the chosen entries are **pinned** in the final assembly. The assembly is stored in `RoundSegment`.
4. **Stage 2** is `chatservice.Stream` with the stage-2 system prompt, the assembled segments (pinned entries' full summaries, falling back to the full context when that is short, then library chunks, numbered for citation), the window and the prompt. The deltas are forwarded to the client.
5. **Memory update**, after the reply, in the background: for each affected type, its summarizing mode decides whether to create or update entries, with System-1 `Complete` calls under JSON schemas. The changes are recorded in `EntryRound`. With no blueprint, the default mode updates the active entities' summaries and proposes new entities found in the round.

**Feedback effects**
- **Correct / Incorrect** adjust `SegmentFeedback` for every segment in the round's `RoundSegment`, which changes their grounding multiplier in later retrieval. Changing the vote moves it rather than adding a second one. Correct selections can later serve as few-shot examples for stage 1.
- **Ignore** drops the round from every later window and from memory-update inputs. Entries it created or updated are flagged for review, not deleted.

### Frontend
In the UI, entries are called **Chronicles** (the earlier app called them *Topics*). These parts live in the workbench and the AgentOS Chat editor (see [ChatCompletion.md](ChatCompletion.md)):
- The **Chronicles** side view (a `workbench.sideView` contribution, see [Contributors.md](Contributors.md)). It shows the CCG as a tree (MUI `TreeView`) or a graph (`react-flow`, already in the stack). Each entry has a checkbox for the focused conversation, and checking a child ticks its parents.
- An **entry editor** for the short summary, full summary, full context, typed fields and linked rounds.
- **Per-round controls**: edit, delete and 👍 / 👎 / 🚫 (neutral when none is set). Each round also shows which entries stage 1 chose, and that choice can be corrected.
- **Type designer**: the Cognition Editor ([Cognition.md](Cognition.md)) edits summarizing modes as `summarizer` graphs.

## Open questions
- Should knowledge base volumes be memory too? For example, a markdown or wiki entry could point at a volume file, with its context fetched through `indexservice.Find`.
- Entries belong to a user and can be shared with the tenant, or with the global tenant after moderator approval; see [MultiTenant.md](MultiTenant.md#sharing-chronicles).
- The node vocabulary and how runs execute have moved to [Cognition.md](Cognition.md).
