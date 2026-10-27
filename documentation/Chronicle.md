# Chronicle memory system

## Context
**AgentOS › Chat** ([ChatCompletion.md](ChatCompletion.md)) needs memory that outlives one context window. Feeding the model the whole chat history drifts and does not scale. Searching every past message by similarity alone is unpredictable, and the user cannot steer it.

Chronicle, the **Categorized Context Graph Memory (CCGM)** described in [Architecting the Chronicle Memory System](../../code-and-castles/www/content/code/20260908-chronicle-memory/index.md), replaces both with two things:
- structured memory entries, connected in a graph;
- a deterministic, multi-stage pipeline that decides which entries a turn needs. The user can see and override that choice.

This document records the design and what was built: entries, the Chronicles side view and entry editor, the `chronicle` retrieval source, stage 1 and the memory update (slice 6), run as the built-in *Personal* Cognition graph (slice 7), and sharing with the tenant and the global tenant (slice 11). Custom types, the graph view and the rest are in [plans/Chronicle.md](plans/Chronicle.md).

## The design (from the article)

### 1. Active conversation window
- Each turn sees a **window** of the last N rounds (user message + assistant reply). It defaults to **15** and the user can configure it. The window is used to read immediate intent without dragging in the whole history.
- Editing a past prompt and deleting a whole round are planned ([plans/Chronicle.md](plans/Chronicle.md)).
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
Entries form a graph the user browses as a **tree** (hierarchy); a **network** (graph) view is planned.
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

## Built in slice 6
**The feature** (`backend/features/chronicle`)
- **Tables** (all `db.Tenanted`; entries and links also `db.Owned`, private to their owner until RBAC):
  - `Entry`: `Type`, `Title`, `ShortSummary`, `FullSummary`, `FullContext`, `Fields` (JSON), `ParentID`, `Status` (`active` or `proposed`), `Review` and `IndexedHash`.
  - `EntryLink`: `related` links only, with no direction.
  - `EntryRound`: `created` or `updated`, with `Previous` (the summaries an update replaced, so it can be undone).
- **Differences from the first plan** (its data model and services are in [plans/Chronicle.md](plans/Chronicle.md#the-first-mapping-onto-foundation))
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

**Verification of slice 6**
- **Backend tests** (`backend/scripts/build.sh --test ./...`, all passing):
  - `chronicleservice/impl`: editing, validation (types, fields, parents, cycles), indexing, reindexing after an embedding failure, links, deleting, owner isolation, accepting a proposal.
  - `chronicle/steps`: candidates (parents, active, linked, retrieved, never proposed), stage 1 (ranking, unknown IDs, the fallback, skipping) and the memory update (only used entries, deduplicated proposals, round records).
  - `chronicle/store/entrystore`: vector and keyword search, owner filtering, unembedded entries, replacing, deleting, a dimension change.
  - `conversationservice/impl`: pinned items first and once, the window passed to providers, followers and the `followed` event, the feedback hook.
- **Against a stub Gemini** (as in slice 5): entries were created and indexed; `retrieve` ranked them with the axis multiplier; a round pinned *checked*, *parent* and *stage 1* entries in that order; the memory update updated the used entries and proposed one; 🚫 flagged them; accepting the proposal made it searchable.
- **Headless Chromium against `yarn dev`**: the Editors and Chronicles side views, a checkbox used in the next round, the Chronicle chips and the memory line, and a chip opening the entry editor with its rounds.

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

## Built in slice 11
Sharing entries, with the global tenant and the moderation queue, is recorded in [MultiTenant.md › Built in slice 11](MultiTenant.md#built-in-slice-11). What it changed here:
- `Entry` gained `Shared` (`tenant`, or `global` on the global tenant's copy) and `SourceID`, and `ChronicleShare` is the global request. `Entry.Hash` covers whether it is shared, so a change of sharing rewrites its row.
- The entries table has a `shared` column. A search finds the user's entries and the tenant's shared ones; `chronicle.Owner.OthersOnly` leaves out the user's own (for the global table).
- `chronicle.Visible` is what a user may read. Stage 1 chooses from it (`Steps.visible`); the memory update and every write keep to `chronicle.Owned` (`Steps.entries`).
- The entry editor opens someone else's entry read-only, and has *Share…* for one's own. The Chronicles view lists shared entries under *Shared with you*.
