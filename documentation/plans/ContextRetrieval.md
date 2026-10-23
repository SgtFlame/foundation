# Context retrieval engine: dual-axis ingestion and post-response grounding

## Context
A chat turn needs context from two kinds of source:
- **Chronicle memory**: CCG entries and past conversation rounds ([Chronicle.md](Chronicle.md)).
- **Library documents**: Volume chunks indexed by `indexservice` ([AIChatEmbed.md](AIChatEmbed.md)).

Large context windows, plain RAG and keyword-only search each fail in their own way. This engine combines vector and BM25 search across both kinds of source. The user steers it with two discrete dials, and per-response feedback corrects it over time. Every step is deterministic and inspectable: the UI can show exactly which segments were injected and why.

The design follows Tony Richards' "Deterministic Context Control: Dual-Axis Ingestion & Post-Execution Grounding" (Sep 2026).

Status: planned. It generalizes the RRF that `indexservice.Find` already implements (`index/services/indexservice/impl/fuse.go`).

## User controls
These are per conversation: a `chat.contextSettings` contribution (`rag`, see [ChatCompletion.md](ChatCompletion.md)). The defaults live in configuration.

| Control | Range | Meaning |
|---|---|---|
| **Axis 1: Lexical ↔ Semantic** | 11 steps, −5…+5, 0 neutral | −5 favors exact keyword matches (BM25): function names, endpoints, error codes. +5 favors dense vector similarity: concepts, intent. |
| **Axis 2: Chronicle ↔ Library** | 11 steps, −5…+5, 0 neutral | −5 favors Chronicle memory (entries, session history). +5 favors Library documents (Volumes: manuals, specs, notes). |
| **Top-K** | 1…50, default 8 | the number of segments injected |
| **Context budget** | tokens | the cap on injected text, defaulting to a share of the level's context window |
| **Temperature, Top-P** | continuous | generation parameters. They default from the chosen level (`ai.chat.levels`) and can be overridden per conversation. |

**Presets**: the `chat.context.presets` configuration holds named combinations, so the user does not have to tune both dials. Examples:
- *Code lookup*: Axis 1 −4, Axis 2 +3.
- *Brainstorm*: Axis 1 +4, Axis 2 0.
- *Recall*: Axis 1 0, Axis 2 −4.

The popover shows the presets as chips above the sliders, and moving a slider switches to *Custom*.

**Mapping the axes to numbers**
- **Axis 1 → method weights.** For slider value a₁:
  - `w_vec = (a₁ + 5) / 10` and `w_bm25 = 1 − w_vec`;
  - at ±5 the unused search is skipped, as `Find` does at weight 0 or 1;
  - `Find`'s `Weight` (0…1) is the same quantity, so the API and the UI agree.
- **Axis 2 → source multipliers.** For slider value a₂:
  - `m_library = B^(a₂/5)` and `m_chronicle = B^(−a₂/5)`;
  - the base `B` defaults to 4, so the extremes favor one side 16:1 but never exclude the other completely;
  - turning a source off entirely is a separate toggle.
  - It is exponential, not linear, because RRF scores differ only a little between neighbouring ranks. A multiplier of a few times is already decisive, and an exponential gives even steps per slider notch.

## Pipeline
`backend/features/retrieval`: a `retrievalservice` used by Chronicle's `Send` and by the document Chat tab.

**1. Dual parallel fetch**
- **Sources** are a backend extension point, `retrieval.Source` ([Contributors.md](Contributors.md)):
  ```go
  type Source interface {
      ID() string                 // "library", "chronicle", "rounds"
      Axis() Axis                 // AxisChronicle | AxisLibrary
      VectorSearch(ctx, vec []float32, k int, f Filter) ([]Segment, error)
      KeywordSearch(ctx, query string, k int, f Filter) ([]Segment, error)
  }
  ```
- **Built-in sources**
  - `library` wraps the index's LanceDB `chunks` table.
  - `chronicle` is a LanceDB `entries` table with entry short and full summaries, a vector, and an FTS index.
  - `rounds` is a LanceDB `rounds` table of past rounds' text, giving the session history the Chronicle axis covers.
- **Fetching**
  - The query is embedded once (`PurposeQuery`).
  - Every enabled source runs both searches concurrently (`errgroup`), each fetching `K = max(5·TopK, 50)` candidates.
  - A failed source is logged and skipped, so the turn still answers.

**2. RRF scoring** (k = 60)

  `RRF(d) = Σ_{m ∈ {vec, bm25}} w_m / (k + r_m(d))`

  `r_m(d)` is d's 1-based rank in method m's list for its source. A method that missed d contributes 0, so segments that both methods find rise.

**3. Source-axis re-weighting and grounding**

  `final(d) = RRF(d) · m_axis(d) · g(d)`

- `m_axis` is the Axis 2 multiplier for d's source.
- `g(d)` is the **grounding multiplier** from feedback (below). It starts at 1.

**4. Deduplication and window assembly**
1. **Pinned items go first** and count against the budget: user-checked Chronicle entries, the open document in the document Chat tab, and entries the stage-1 selection kept.
2. **Merge overlaps**: chunks of the same file whose byte ranges overlap or touch merge into one segment, keeping the highest score. The index's 512-byte chunk overlap would otherwise inject the same text twice. Identical text from different files (the same chunk hash) is kept once, citing both.
3. **Select**: sort by `final`, then take segments until `TopK` or the token budget runs out. A segment that does not fit is skipped, and the next one is tried.
4. **Number** the result `[1]…[n]` for citations, and render it into stage 2's system prompt by source: memory first, then documents.

**Output**: `RetrieveResponse{ Segments []Scored{Segment, Rank, Score, RRF, AxisMult, Grounding, VectorRank, KeywordRank, Pinned} }`.
- Each round stores it, so the UI can show the reason behind every injected segment: the per-round "Context" inspector.
- DevTools can show it for debugging.

## How this fits Chronicle's stages
Retrieval is deterministic and feeds Chronicle's **stage 1** ([Chronicle.md](Chronicle.md)).
- **Stage 1's candidates**: retrieval's Chronicle-axis segments, the active entries and their graph neighbours.
- **Stage 1's output**: it decides *shift vs continue* and which entries stay active.
- **Stage 2's input**: stage 1's chosen entries are then pinned, and the library segments are assembled around them.

Stage 1 is a node in the mode's Cognition graph ([Cognition.md](Cognition.md)). A mode graph without it uses retrieval's selection alone, which is cheaper, faster and fully deterministic.

## Post-response feedback
Each assistant reply has three buttons; neutral is the default.

| State | Meaning | Effect |
|---|---|---|
| **Neutral** | recorded normally | the round stays in the window and the memory queue |
| 👍 **Correct** | the answer was grounded | every segment the round injected gets **+1 up** |
| 👎 **Incorrect** | a retrieval or generation failure | every segment the round injected gets **+1 down**, and the round is listed for review. *The round itself stays in the window*, so the user can correct the model in the next turn. |
| 🚫 **Ignore** | exclude this exchange | the user prompt and the reply leave the sliding window and the Chronicle memory-update queue. Memory updates already made from it are flagged for review. Segment weights are not touched. |

**Storage** (GORM, `db.Model` TSIDs)
- `RoundSegment{RoundID, SourceID, SegmentKey, Rank, Score, Pinned}` records what each round injected. It replaces the per-entry `RoundEntry` table of the first Chronicle draft.
- `SegmentFeedback{SourceID, SegmentKey, Up, Down, LastAt}` holds the counters. `SegmentKey` is a chunk's **content hash** for library chunks, or an entry or round id.
  - Feedback on a library chunk therefore survives reindexing for as long as the chunk's text is unchanged, and is dropped when the text is edited. That is the right behaviour, because the text it judged is gone.
- **Changing feedback is undoable**: the counters are recomputed from the round's previous and new state, so 👍 → 👎 moves the vote rather than adding a second one.

**Grounding multiplier**

  `g = clamp( ((1 + Up) / (1 + Down))^γ , g_min, g_max )`

- The defaults are γ = 0.5, g_min = 0.25 and g_max = 4, in the `retrieval.feedback` configuration section.
- One vote moves a segment about 1.4×, and many consistent votes saturate at 4× (or ¼).

**Surfacing**: the Dashboard gets a *Flagged segments* widget. It lists segments with a high Down count and those from 👎 rounds, links to the source file or entry, and points at bad documentation or weak embeddings ([Usage.md](Usage.md)).

## Configuration sections ([Configuration.md](Configuration.md))
- `retrieval`: `rrfK` (60), `candidateFactor` (5), `minCandidates` (50), `axisBase` (4), `defaultTopK` (8), `budgetShare` (0.5 of the level's context window).
- `retrieval.feedback`: `gamma`, `min`, `max`.
- `chat.context.presets`: named presets for the two axes plus Top-K.
- `chat.context.defaults.rag`: the default axes, Top-K, the sources enabled, and the volume filter.

## Relation to `indexservice.Find`
`Find` stays as the search API over the Library alone.
- Its RRF (`fuse.go`) moves to `features/retrieval/fuse` and is shared, so there is one implementation.
- `Find`'s `Weight` is Axis 1 expressed as 0…1.
- The library `Source` reuses `lancestore`.

## Open questions
Moved here from [TODO.md](TODO.md):
1. **Axis 2 mapping**:
   - *Proposal*: exponential multipliers `B^(±a/5)` with B = 4, so ±5 favors one side 16:1 but never excludes the other.
   - *Alternative*: linear, where ±5 removes the other side entirely.
   - *Decision*:
2. **Thumbs down**:
   - *Proposal*: 👎 *Incorrect* lowers the weight of the round's segments but keeps the round in the window, so the user can correct the model next turn. Only 🚫 *Ignore* removes it.
   - This differs from the first Chronicle article.
   - *Decision*:
3. **Past rounds as a memory source**:
   - *Proposal*: index the text of every non-ignored round in a LanceDB `rounds` table, searched across all conversations, not only the current one.
   - *Decision*:

Who feedback affects (global, per tenant or per user) has moved to [MultiTenant.md](MultiTenant.md).
