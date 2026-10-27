# Context retrieval engine: dual-axis ingestion and post-response grounding

## Context
A chat turn needs context from two kinds of source:
- **Chronicle memory**: CCG entries and past conversation rounds ([Chronicle.md](Chronicle.md)).
- **Library documents**: Volume chunks indexed by `indexservice` ([AIChatEmbed.md](AIChatEmbed.md)).

Large context windows, plain RAG and keyword-only search each fail in their own way. This engine combines vector and BM25 search across both kinds of source. The user steers it with two discrete dials, and per-response feedback corrects it over time. Every step is deterministic and inspectable: the UI can show exactly which segments were injected and why.

The design follows Tony Richards' "Deterministic Context Control: Dual-Axis Ingestion & Post-Execution Grounding" (Sep 2026).

Status: partly built. Slice 5 built the engine with the library source, the RAG Search provider and control, citations, the Context inspector and 👍/👎/🚫 feedback; see [Built in slice 5](#built-in-slice-5). Slice 6 added the `chronicle` source and pinned items; see [Built in slice 6](#built-in-slice-6). The rounds source and the *Flagged segments* widget wait.

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
   - *Decision*: (after seeing) built on the proposal in slice 5. With only the library source, the dial moves every score by the same multiplier, so its effect shows once Chronicle memory arrives (slice 6).
2. **Thumbs down**:
   - *Proposal*: 👎 *Incorrect* lowers the weight of the round's segments but keeps the round in the window, so the user can correct the model next turn. Only 🚫 *Ignore* removes it.
   - This differs from the first Chronicle article.
   - *Decision*: (after seeing) built on the proposal in slice 5.
3. **Past rounds as a memory source**:
   - *Proposal*: index the text of every non-ignored round in a LanceDB `rounds` table, searched across all conversations, not only the current one.
   - *Decision*: (after seeing) not built in slice 6. Chronicle entries carry what outlives the window, so the `rounds` source waits until memory has been seen with a real model.

Who feedback affects (global, per tenant or per user) has moved to [MultiTenant.md](MultiTenant.md).

## Built in slice 5
**The feature** (`backend/features/retrieval`)
- **Shared types** (package `retrieval`)
  - `retrieval.Sources`, the backend point `retrieval.source`.
  - A source is a struct contribution `{Descriptor, Title, Axis, VectorSearch, KeywordSearch}`, rather than the interface sketched above, matching `chat.contextProvider`. The searches get the call context, from which `features.CallFrom` reads the principal.
  - `Segment{SourceID, ID, Key, Title, Text, Location}`. `ID` is unique within the source and is what RRF fuses on (a chunk's path and ordinal). `Key` is what feedback counts against (the chunk's content hash).
  - `Scored` adds `Number`, `Keys` (every feedback key it covers), `Also` (other paths with the same text), `Score`, `RRF`, `AxisMult`, `Grounding`, both ranks, `Pinned` and `Tokens`.
- **One RRF**: `features/retrieval/fuse` holds the generic `fuse.Fuse`, and `indexservice.Find` now uses it. `fuse.go` in the index only groups chunks into documents.
- **`retrievalservice.Retrieve`** (`POST /api/v1/retrieval/retrieve`, action `retrieval.read`)
  - Args: `Query`, `LexicalSemantic` and `ChronicleLibrary` (−5…5), `TopK` (1…50, 0 for the default), `ContextWindow`, `Sources` and `VolumeIDs`.
  - The pipeline is as planned: one query embedding (`embed-query`), both searches per source concurrently, per-source RRF, the axis multiplier, the grounding multiplier, then window assembly.
  - **The budget** is `min(budgetShare × contextWindow, maxBudgetTokens)`. Gemini's windows are about a million tokens, so a share alone would allow far more than the prompt's 48 KB context cap. `maxBudgetTokens` (10 000) keeps the two consistent.
  - **No API key**: when the query cannot be embedded, retrieval falls back to the keyword ranking alone and says so (`VectorSkipped`), rather than failing the round. A failed source is logged and skipped.
  - **Merging**: chunks of one file merge when their byte ranges overlap or touch and both texts still match their ranges. The merged segment keeps the higher score and carries every merged chunk's feedback key, so a vote reaches all of them.
- **The library source**: `indexsource.Contribute` (`features/index/source`), searching the `lancestore` store. Each chunk is located from its sidecar, and only when the sidecar's hash matches the stored chunk.
  - `indexservice.Match` gained `Hash`, which `lancestore` now returns.
  - `serve.go` opens LanceDB before the registry is built, so the source can be contributed with it.
- **Tables**
  - **`RoundSegment`**: as planned, plus `Number`, `OtherKeys`, `Title`, `Snippet` (400 bytes), `Location` (JSON), `Also` and every score. A round's sources and Context inspector therefore read from it alone, with no re-retrieval.
  - **`SegmentFeedback`** (table `segment_feedback`): `db.Tenanted`, so feedback is **per tenant** (MultiTenant question 1, on its proposal).
  - **`retrieval.Vote`** moves counts from a round's old verdict to its new one inside the caller's transaction.
- **Settings** (`retrievalsettings`)
  - **`retrieval`** (AI › Retrieval, app scope): `rrfK`, `candidateFactor`, `minCandidates`, `axisBase`, `defaultTopK`, `budgetShare` and `maxBudgetTokens`.
  - **`retrieval.feedback`**: `gamma`, `min` and `max`.
  - **`chat.context.presets`** (AI › Chat › Context, all scopes): *Code lookup* (−4, +3, 8), *Brainstorm* (+4, 0, 12) and *Recall* (0, −4, 8).

**RAG Search** (`retrievalcontext`, the `rag` context provider)
- **Settings**: `enabled`, `preset`, `lexicalSemantic`, `chronicleLibrary`, `topK` and `sources` (`{library: true}`). Their defaults section is `chat.context.defaults.rag`, declared like every provider's.
- **Gather** runs `Retrieve` with the conversation's dials and the round's model's context window (`GatherArgs.ContextWindow`, new). It returns one `ContextItem` per segment, carrying the segment (`ContextItem.Segment`, new).
- **Late binding**: the provider is contributed before the retrieval service exists, because configuration needs the finished registry first. `serve.go` therefore hands it a function returning the service.
- **In `Send`**
  - The prompt numbers citable items `[1]…[n]` in the order they fit. It adds one instruction to cite them inline and never invent a number.
  - It stores the round's `RoundSegment`s and sends them as a `sources` event before the first delta.
  - `rounds` returns each round's segments, and `POST /conversation/feedback` sets the verdict.

**Feedback**
- **The verdict** is `Round.Feedback`: `""`, `up`, `down` or `ignore`.
  - 👍 and 👎 move the round's segment counts, and changing the verdict moves the vote.
  - 🚫 drops the round from later rounds' history (`InHistory` and `Send`'s history query). It also takes back a 👍 or 👎.
- **Nothing reads 👎 rounds yet**: they are kept for the *Flagged segments* widget and the moderation queue.

**Frontend**
- **Clients**: `src/services/Retrieval.ts` (new, with a `/api/v1/retrieval` proxy). `Conversation.ts` gained `RoundSegment`, `Round.feedback`, `Round.segments`, `onSources` and `feedback()`.
- **`src/retrieval/`**: the RAG Search `chat.contextSettings` contribution (`RagControl`).
  - It has the on/off switch, preset chips (choosing one sets the dials; moving a dial makes it *Custom*), the two 11-step dials, Top-K and the Volumes source switch.
- **`src/components/chat/`**
  - `RoundSources`: the Sources row and the Context inspector, a table of #, source, segment, vector/keyword ranks, RRF, axis ×, grounding ×, score and tokens.
  - `FeedbackButtons`: shown on hover, and stays visible once a verdict is set. An ignored round is greyed out.
- **Citations**: `MarkdownText` takes `citations`.
  - Lezer parses `[1]` and `[1][3]` as links, and Live Preview would hide their brackets. Valid numbers are therefore swapped for `⟦n⟧` before rendering and shown as chips (`components/markdown/citations.ts`).
  - Clicking a chip, or a source's title, opens the file at its lines through `openMarkdownAt`. The lines are asked for through `editors/markdown/reveal.ts`, which Live Preview reads once the document has loaded, even in a new tab.

**Not yet**
- **Sources**: the Chronicle and rounds sources (slice 6).
- **Window assembly**: pinned items.
- **Controls**: the context budget control, and the volume filter in the popover (`VolumeIDs` exists in the API).
- **Sampling**: the Advanced temperature, Top-P and sampling Top-K overrides (ChatCompletion gap 6).
- **Surfacing feedback**: the *Flagged segments* widget, and listing 👎 rounds for review.
- **Plumbing**: DevTools showing a round's retrieval, and merging that bridges two kept segments (a chunk joining both neighbours merges into one of them only).

**Verification of slice 5**
- **Backend tests**
  - `retrievalservice/impl/Retrieve_test.go`: fusion, the axis multipliers, grounding after votes, a moved vote, tenant isolation, source selection, the keyword fallback, merging, deduplication, the budget and Top-K.
  - `conversationservice/impl/Send_test.go`: numbered citations in the prompt, the `sources` event, stored segments, feedback moves and 🚫 history.
  - `index/source/Source_test.go`: titles.
- **Against a stub Gemini**: a real server was run against a local stand-in for the Gemini API (`GOOGLE_GEMINI_BASE_URL`), with hashed bag-of-words embeddings and a reply citing `[1][2]`.
  - Files were indexed, and `retrieve` ranked them.
  - A round streamed `sources` before its deltas, and 👍 moved the grounding to √2.
- **Headless Chromium against `yarn dev`**
  - The RAG Search popover and presets.
  - Citation chips, the Sources row and the inspector (Axis × 2.30 under *Code lookup*, Grounding × 1.41 after 👍).
  - A citation opening `indexing.md` with the cited lines selected.
- **Not verified**: replies from a real model, which may cite differently or not at all. The stub's reply always cites. This is part of the ★ review.

### Decisions to review after seeing it
1. Sources are struct contributions on a backend point, like context providers.
2. Feedback is per tenant (MultiTenant question 1's proposal).
3. 👎 keeps the round in the window, and 🚫 removes it and takes back its vote (question 2's proposal).
4. The Axis 2 mapping is exponential with B = 4 (question 1's proposal).
5. A round stores everything its inspector shows, so old rounds explain themselves even after reindexing.
6. Without an embedding, retrieval falls back to keywords instead of failing the round.
7. The context budget is capped at `maxBudgetTokens` (10 000), not only a share of the window.

## Built in slice 6
- **The `chronicle` source** (`features/chronicle/source`, see [Chronicle.md › Built in slice 6](Chronicle.md#built-in-slice-6)): the principal's active entries from LanceDB's `entries` table, on the Chronicle axis. A segment's `ID` and `Key` are the entry's ID, and `Location.EntryID` (new) makes its citation open the entry.
- **RAG Search** searches it by default (`sources: {library, chronicle}`, with a *Chronicles* switch), so the Chronicles ↔ Library dial now moves results between memory and Volumes.
- **Pinned items** come from the `chronicle` context provider, not from retrieval's window assembly: `Send` puts pinned items first, in their provider's order, and a segment two providers found goes in once, pinned. They count against the prompt's 48 KB cap, not retrieval's token budget.
- **`Scored.Reason`** (and `RoundSegment.Reason`) says why a pinned segment is in: `checked`, `parent`, or `stage 1 · retrieved|active|linked`. The Context inspector shows it as *Why*.
- **Not yet**: the `rounds` source (question 3), the *Flagged segments* widget, and caching the query embedding, which stage 1's candidate retrieval and RAG Search now each make.
