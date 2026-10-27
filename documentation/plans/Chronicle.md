# Chronicle memory system

## Context
**AgentOS › Chat** ([ChatCompletion.md](ChatCompletion.md)) needs memory that outlives one context window. Feeding the model the whole chat history drifts and does not scale. Searching every past message by similarity alone is unpredictable, and the user cannot steer it.

Chronicle, the **Categorized Context Graph Memory (CCGM)** described in [Architecting the Chronicle Memory System](../../../code-and-castles/www/content/code/20260908-chronicle-memory/index.md), replaces both with two things:
- structured memory entries, connected in a graph;
- a deterministic, multi-stage pipeline that decides which entries a turn needs. The user can see and override that choice.

This document records the design and how it maps onto Foundation's services. It is the memory half of the AgentOS Chat plan.

Status: partly built. Slice 6 built entries, the Chronicles side view, the entry editor, the chronicle retrieval source, and stage 1 and the memory update as Go step functions; see [Built in slice 6](#built-in-slice-6). Slice 7 made the steps nodes of the built-in *Personal* graph, and slice 11 added sharing ([Built in slice 11](#built-in-slice-11)). Custom types and the graph view wait.

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
- **Volumes as memory**: should knowledge base volumes be memory too? For example, a markdown or wiki entry could point at a volume file, with its context fetched through `indexservice.Find`.
  - *Proposal*: not as entries. RAG Search already searches Volumes and Chronicles together, weighed by the Chronicles ↔ Library dial, so a Volume reaches the reply without being copied into memory.
  - *Decision*: (after seeing) slice 6 built the proposal: no entry points at a Volume file.
- Entries belong to a user and can be shared with the tenant, or with the global tenant after moderator approval; see [MultiTenant.md](MultiTenant.md#sharing-chronicles).
- The node vocabulary and how runs execute have moved to [Cognition.md](Cognition.md).

## Built in slice 6
**The feature** (`backend/features/chronicle`)
- **Tables** (all `db.Tenanted`; entries and links also `db.Owned`, private to their owner until RBAC):
  - `Entry`: `Type`, `Title`, `ShortSummary`, `FullSummary`, `FullContext`, `Fields` (JSON), `ParentID`, `Status` (`active` or `proposed`), `Review` and `IndexedHash`.
  - `EntryLink`: `related` links only, with no direction.
  - `EntryRound`: `created` or `updated`, with `Previous` (the summaries an update replaced, so it can be undone).
- **Differences from the plan above**
  - **Parents are a column, not a link.** `Entry.ParentID` makes the tree; `EntryLink` holds only `related` edges. One parent per entry keeps the tree view simple.
  - **Built-in types are code, not an `EntryType` table** (`chronicle.Types`): Entities (with a *kind*), Notes, Wikis, Timelines (dated or ordinal) and Events (a *date* or an *order*). Each has a `Fields` schema in the settings schema language. The table arrives with custom types and their summarizer graphs (Cognition).
  - **No `ActiveEntry` table.** The *checked* entries are the conversation's `chronicle` context settings (`checked`, a list of IDs), stored with the conversation like every control's settings. The *active* entries are those pinned in the window's last round (`RoundSegment`, source `chronicle`). So 🚫 on a round also drops what it made active.
  - **No `chronicleservice.Send`.** The pipeline runs inside `conversationservice.Send` (ChatCompletion.md's slice 4 decision): stage 1 is the `chronicle` context provider, and the memory update is the `chronicle` round follower.
  - **Titles.** Entries have a `Title`; there is no slug.
- **`chronicleservice`** (`/api/v1/chronicle`): `POST list|get|create|update|delete|link|reindex`, `GET types`. Actions are `chronicle.read` and `chronicle.write`.
  - `update` replaces what the user edits. `accept` makes a proposed entry active, and `reviewed` clears the flag.
  - `delete` moves the children up to the parent and removes the links and round records.
  - The parent must be the principal's own entry and never a descendant; unknown fields of a changed type are dropped.
- **The entries table** (`chronicle/store/entrystore`): LanceDB's `entries` table beside the index's `chunks`, with vector and BM25 search filtered to the owner.
  - Active entries are written after each change, in the background. Proposed entries are never indexed.
  - An entry that cannot be embedded (no API key) is stored with `embedded = false`: keyword search finds it, vector search skips it.
  - `Reindex` runs at start and whenever the provider settings change, so those entries are embedded once a key is set.
- **The `chronicle` retrieval source** (`chronicle/source`), on the Chronicle side of the dial. A segment's ID and feedback key are the entry's ID, so 👍/👎 follow the entry through edits.

**The pipeline as step functions** (seam 3, `chronicle/steps`), each with typed inputs and outputs, shaped like the Cognition node it becomes:
1. **Window**: `conversationservice.Send` passes the rounds it sends as history to every provider (`GatherArgs.Window`). The `chat.historyRounds` default is now **15** (the plan's `WindowSize`), titled *Window*.
2. **`Candidates`**: the checked entries with their parents (the parent-inclusion rule), the entries retrieval's `chronicle` source ranks highest, the active entries, and one hop from the checked and active ones (parent, children, related links).
3. **`Match`** (stage 1): one `Complete` call at the `ai.chat.stages` `match` level (Fast), with a JSON schema for `{shift, entries: [{id, rank}]}`. It reads the window's last 6 rounds, the message and the candidates' short summaries.
   - The checked entries are always kept, first; stage 1 fills up to `topK` (5).
   - **When stage 1 fails** (no API key) or is switched off, the candidates are kept in their order (retrieved, active, linked), so the round still has memory.
4. **Reply** (stage 2): the chosen entries are **pinned** context items, put before every other item in the prompt, in the order chosen, each as its title, short summary and full summary (or the full context when there is no full summary).
5. **`Update`** (the memory update): after the reply and the title, one `Complete` call at the `memory` level (Fast) with a JSON schema.
   - It may rewrite the summaries of the entries the round injected, and nothing else.
   - It may **propose** up to 3 new entries. They wait in the Chronicles view as *Proposed* until the user accepts or dismisses them, and are never used before.
   - Every change is an `EntryRound`. The browser hears of it through a `followed` event.
- **🚫 Ignore** flags every entry the round made or changed for review (`Review`), through the follower's `Feedback` hook. Nothing is deleted.

**The settings**
- `chat.context.defaults.chronicle` (AI › Chat › Context): `enabled`, `topK` (5), `match` and `update`.
- `ai.chat.stages` (AI › Chat): `match` and `memory`, both Fast.
- RAG Search searches the `chronicle` source too (a new *Chronicles* switch), so the Chronicles ↔ Library dial now has an effect.

**Frontend**
- **`workbench.sideView`**: the left side bar is now vertical tabs, each a contribution. The hand-written `LeftMenuSidebar` is gone.
  - The **Editors** view lists every `workbench.command` with a section (the new point); each editor contributes its own launcher.
  - The **Chronicles** view (`src/chronicle/ChroniclesView.tsx`) shows a tree per type (by parent), a *Proposed* group with ✓ and ✕, a filter, *Only entries in use*, and + for a new entry.
    - Each entry has a checkbox for the **focused chat's** current conversation (`chatFocus.ts`, published by `ChatEditor`). Checking a child shows its parents checked and disabled.
    - Marks: a dot for *used in the last reply*, ✎ for *changed by the last memory update*, ⚑ for *flagged for review*.
- **The entry editor** (`src/editors/chronicle/`, kind `chronicle`): title, type, parent, the type's fields, the short summary, the full summary, the full context, related entries (link and unlink), and the conversation rounds that made or changed it.
  - An update's round has **Restore earlier summaries**, which puts the replaced summaries back in the form; saving keeps them.
  - *Accept*, *Mark reviewed*, *Delete* or *Dismiss*.
- **Chat**: a round lists its **Chronicle chips** (the pinned entries) above its Sources; a chip opens the entry. Under the reply, the memory update's summary is shown (*Memory: updated …; proposed …*). The Context inspector has a *Why* column (`checked`, `parent`, `stage 1 · retrieved`, …).
- **The Chronicles control** in ⚙: on/off, *Entries* (top-K), *Context matching* and *Update memory*.

**Not yet**
- The **graph (network) view**, and custom types with their summarizing modes (Cognition).
- **Correcting stage 1 from a chip** (✕ on a chip excluding the entry next round).
- **Saving checks at once**: a checkbox changes the conversation's settings in the browser, and they are stored with the next message. Checks made before a reload with no message since are lost.
- **Showing the memory update's summary after a reload**: the `followed` event is not stored. The entry editor's rounds list still shows every change.
- **Sharing entries** (*built in slice 11*, see [Built in slice 11](#built-in-slice-11)), and the moderation queue for flagged entries (not yet: the queue holds global shares only).
- **Undoing a whole round's changes at once**, rather than restoring summaries entry by entry.
- Editing a past prompt, and deleting a round.

**Verification of slice 6**
- **Backend tests** (`backend/scripts/build.sh --test ./...`, all passing):
  - `chronicleservice/impl`: editing, validation (types, fields, parents, cycles), indexing, reindexing after an embedding failure, links, deleting, owner isolation, accepting a proposal.
  - `chronicle/steps`: candidates (parents, active, linked, retrieved, never proposed), stage 1 (ranking, unknown IDs, the fallback, skipping) and the memory update (only used entries, deduplicated proposals, round records).
  - `chronicle/store/entrystore`: vector and keyword search, owner filtering, unembedded entries, replacing, deleting, a dimension change.
  - `conversationservice/impl`: pinned items first and once, the window passed to providers, followers and the `followed` event, the feedback hook.
- **Against a stub Gemini** (as in slice 5): entries were created and indexed; `retrieve` ranked them with the axis multiplier; a round pinned *checked*, *parent* and *stage 1* entries in that order; the memory update updated the used entries and proposed one; 🚫 flagged them; accepting the proposal made it searchable.
- **Headless Chromium against `yarn dev`**: the Editors and Chronicles side views, a checkbox used in the next round, the Chronicle chips and the memory line, and a chip opening the entry editor with its rounds.
- **Not verified**: a real model's stage 1 choices and memory updates. The stub's are canned. This is the ★ review.

### Decisions to review after seeing it
1. The pipeline runs inside `conversationservice.Send`: stage 1 as the `chronicle` context provider, the memory update as the `chronicle` round follower (a new point, `chat.roundFollower`).
2. Checked entries are conversation settings, and active entries are the last round's pinned segments; there is no `ActiveEntry` table.
3. The memory update **proposes** new entries, which wait for the user, and **updates** used entries directly, keeping the old summaries to restore.
4. Without stage 1 (no key, or switched off), retrieval's order decides, rather than no memory.
5. RAG Search searches Chronicles too: entries stage 1 did not choose can still be injected, unpinned, when the dial favours memory.
6. One parent per entry (a column), built-in types in code, entry titles instead of slugs.
7. The window default drops from 50 to 15 rounds.
8. The side bar became vertical tabs (`workbench.sideView`) with an Editors view built from `workbench.command`s.

## Built in slice 7
- **The pipeline is now a Cognition graph** ([Cognition.md › Built in slice 7](Cognition.md#built-in-slice-7)). `chronicle/nodes` wraps `Candidates`, `Match` and `Update` as nodes and contributes the built-in **Personal** graph.
- **The steps are unchanged.** The `chronicle` context provider runs Personal's *On user message* event, and the round follower runs its *On reply* event; each run is traced and shown in the Cognition Editor.
- `chroniclenodes.Settings` replaces the provider's own settings type, and the memory update's `maxProposals` (3) is now the *Update memory* node's config.
- Custom entry types and their summarizer graphs still wait for editing.

## Built in slice 11
Sharing entries, with the global tenant and the moderation queue, is recorded in [MultiTenant.md › Built in slice 11](MultiTenant.md#built-in-slice-11). What it changed here:
- `Entry` gained `Shared` (`tenant`, or `global` on the global tenant's copy) and `SourceID`, and `ChronicleShare` is the global request. `Entry.Hash` covers whether it is shared, so a change of sharing rewrites its row.
- The entries table has a `shared` column. A search finds the user's entries and the tenant's shared ones; `chronicle.Owner.OthersOnly` leaves out the user's own (for the global table).
- `chronicle.Visible` is what a user may read. Stage 1 chooses from it (`Steps.visible`); the memory update and every write keep to `chronicle.Owned` (`Steps.entries`).
- The entry editor opens someone else's entry read-only, and has *Share…* for one's own. The Chronicles view lists shared entries under *Shared with you*.
- **Not yet**: flagged segments in the moderation queue; updating a global copy when its entry changes; sharing an entry's children with it.
