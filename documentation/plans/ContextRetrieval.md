# Context retrieval: what remains

The engine with the `library` and `chronicle` sources, RAG Search, citations, the Context inspector, pinned items and 👍/👎/🚫 feedback are built; see [ContextRetrieval.md](../ContextRetrieval.md).

## The `rounds` source
A LanceDB `rounds` table of past rounds' text, giving the session history the Chronicle axis covers: every non-ignored round, searched across all conversations, not only the current one (question 3). It joins `library` and `chronicle` as a `retrieval.source` on the Chronicle axis.

## The remaining controls
| Control | Range | Meaning |
|---|---|---|
| **Context budget** | tokens | the cap on injected text, defaulting to a share of the level's context window; a control in the popover (the engine already applies `min(budgetShare × window, maxBudgetTokens)`) |
| **Temperature, Top-P** | continuous | generation parameters behind "Advanced". They default from the chosen level (`ai.chat.levels`) and can be overridden per conversation ([ChatCompletion.md](ChatCompletion.md) question 4). |
| **Volume filter** | Volumes | which Volumes the library source searches; `VolumeIDs` exists in the API, and `chat.context.defaults.rag` gains it |

## Surfacing feedback
- **The *Flagged segments* widget** on the Dashboard ([Usage.md](Usage.md)): segments with a high Down count and those from 👎 rounds, linking to the source file or entry, pointing at bad documentation or weak embeddings.
- **Listing 👎 rounds for review**, and flagged segments in the moderation queue ([MultiTenant.md](MultiTenant.md)).

## TODO
### Open questions
1. **Axis 2 mapping**:
   - *Proposal*: exponential multipliers `B^(±a/5)` with B = 4, so ±5 favors one side 16:1 but never excludes the other.
   - *Alternative*: linear, where ±5 removes the other side entirely.
   - *Decision*: (after seeing) built on the proposal in slice 5. Since slice 6 the dial moves results between Chronicle memory and Volumes.
2. **Thumbs down**:
   - *Proposal*: 👎 *Incorrect* lowers the weight of the round's segments but keeps the round in the window, so the user can correct the model next turn. Only 🚫 *Ignore* removes it. This differs from the first Chronicle article.
   - *Decision*: (after seeing) built on the proposal in slice 5.
3. **Past rounds as a memory source**:
   - *Proposal*: index the text of every non-ignored round in a LanceDB `rounds` table, searched across all conversations.
   - *Decision*: (after seeing) not built in slice 6. Chronicle entries carry what outlives the window, so the `rounds` source waits until memory has been seen with a real model.

Who feedback affects is [MultiTenant.md](MultiTenant.md) question 1.

### Review after seeing it ★
- **The axis mapping, Top-K and the inspector with a real model** (slice 5). Everything was seen against a stub of the Gemini API whose reply always cites `[1][2]`, so how a real model cites (or fails to) is unseen. Set a key in Settings › AI › Providers, then review.
- **The decisions made while building**, in [ContextRetrieval.md › Decisions to review after seeing it](../ContextRetrieval.md#decisions-to-review-after-seeing-it).

### Not built yet
- The `rounds` source (above).
- The context budget control, the volume filter in the popover, and the Advanced temperature, Top-P and sampling Top-K overrides.
- The *Flagged segments* widget, and listing 👎 rounds for review.
- Caching the query embedding, which stage 1's candidate retrieval and RAG Search each make.
- DevTools showing a round's retrieval.
- Merging that bridges two kept segments (a chunk joining both neighbours merges into one of them only).
- **Citation relocation**: opening a citation uses the line numbers stored when the round ran, which drift once the file is edited; relocate by the chunk's `Head`/`Tail` anchors.
