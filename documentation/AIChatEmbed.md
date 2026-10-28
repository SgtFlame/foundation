# AI services (embed + chat) and the hybrid search index

## Context
Foundation needs retrieval-augmented generation over the knowledge base, with chat on top of it. This document covers four things:
- **`ai`**: two provider-neutral services, `embedservice` (vector embeddings) and `chatservice` (chat completion), shaped so the OpenAI, Azure AI and Gemini APIs all fit. Gemini is the first provider.
- **`index`**: a feature that keeps every volume markdown file chunked, embedded and searchable. Its `Find` fuses vector similarity with BM25 keyword search.
- **`db`**: the relational database (SQLite through GORM by default) with TSID primary keys, migrated by `serve` at startup.
- **`scripts/build.sh` and `scripts/serve.sh`**: LanceDB needs CGO and a native library, so builds always go through `build.sh`. `serve.sh` rebuilds and restarts the server on every Go change.

Chat in the UI is planned in [ChatCompletion.md](ChatCompletion.md), and its memory system in [Chronicle.md](Chronicle.md).

Status: implemented.

## Storage at a glance

| What | Where | Why |
|---|---|---|
| Markdown files | the kb root (`--kb-root`, `~/.foundation/kb`) | unchanged: the volumes' own files |
| Chunk meta for each file | dotted sidecar `.<name>.md.index.json` next to the file | moves with the content and can be inspected by hand; dotted entries are never listed |
| Chunk text, embeddings, BM25 index | LanceDB at `<data-dir>/lancedb` (`--data-dir`, `~/.foundation`), table `chunks` | vector search and full-text search in one store |
| Relational data | SQLite at `<data-dir>/foundation.db` (`--db`), via GORM | features' tables (the first are Chat's) |
| File-change notifications | in-process `events.Bus[volumeservice.FileEvent]` | no broker is needed in a single process |

**NATS is not used yet.** Nothing needs a KV or object store beyond the above. An embedded NATS server (core pub/sub in place of the in-process bus, plus JetStream KV/Object Store) comes in when services run as separate processes, as in the thin-client deployment. The `FileEvents` type is the one place that would change.

## `ai` feature: `backend/features/ai/`

### Provider-neutral services
Both services follow the CLAUDE.md service layout. The provider is passed in with `impl.WithProvider` and implements a small `Provider` interface declared next to each service. Errors carry an HTTP status through `providers.Error` (`features/ai/providers/Error.go`):
- 400: bad input
- 429: rate limited
- 502: any other upstream failure
- 503: no provider or no API key, or the provider rejected our key
- 504: timeout

**`embedservice`** (`/api/v1/ai/embed`, `/api/v1/ai/embed/describe`)
- `Embed{Texts, Purpose, Title, Model}` returns one **unit-length** vector per text, plus the model, the dimensions and token usage.
- Texts are batched to the provider's limit (`MaxBatch`; 100 for Gemini).
- `Purpose` is `document` (the default) or `query`. Gemini maps it to the `RETRIEVAL_DOCUMENT`/`RETRIEVAL_QUERY` task types, Cohere-style APIs have the same notion, and OpenAI and Azure OpenAI ignore it. `Title` is the source document's name, which Gemini uses with `RETRIEVAL_DOCUMENT`.
- `Describe` reports the default model, its dimensions and the batch size. The index records these in each sidecar, so a model change re-embeds everything.

**`chatservice`** (`/api/v1/ai/chat/complete`, `/api/v1/ai/chat/stream`)
- `Complete{Model, System, Messages, Temperature, MaxTokens, JSONSchema}` returns the assistant `Message`, the finish reason (`stop`, `length`, `safety`, ...) and usage.
  - `System` stays apart from `Messages`, which hold only `user`/`assistant` roles and must end with the user. That maps cleanly onto Gemini's `systemInstruction`, OpenAI's and Azure's system message, and Anthropic's `system`.
  - `JSONSchema` asks for structured JSON output. Chronicle's stage 1 depends on it.
- `Stream` is the CLAUDE.md async pattern: `StreamRequest{Deltas <-chan Delta, Response chan *CompleteResponse}`. Over REST it is Server-Sent Events: a `delta` event `{text}` for each piece, then `done` (a `CompleteResponse`) or `error` `{code, message}`. A request that fails before generation starts gets a plain JSON error with its status.

### Gemini provider: `providers/gemini/`
- Built on `google.golang.org/genai` (Gemini API backend).
- One `Provider` type implements both provider interfaces.
- The key comes from Settings (AI › Providers, stored encrypted since slice 2; see [Configuration.md](Configuration.md#built-in-slice-2)). `--gemini-api-key` or `$GEMINI_API_KEY` wins over the stored key. Without any key, `serve` still runs and every AI call fails with 503.
- Defaults (each a `serve` flag):

  | Flag | Default | Notes |
  |---|---|---|
  | `--embed-model` | `gemini-embedding-001` | |
  | `--embed-dims` | `768` | sent as `outputDimensionality`. Gemini normalizes only at its native 3072, so the provider L2-normalizes smaller vectors |
  | `--chat-model` | `gemini-3.8-flash` | |

- Chat maps `assistant` to Gemini's `model` role. `JSONSchema` becomes `responseMimeType: application/json` plus `responseJsonSchema`.
- **Configuration**: these flags become bootstrap fallbacks once the `config` feature exists. The API key, the model catalog with its pricing, the chat levels and the embed model then live in configuration sections (`ai.providers`, `ai.models`, `ai.chat.levels`, `ai.embed`; see [Configuration.md](Configuration.md)). Every provider call is recorded with its tokens and cost ([Usage.md](Usage.md)).
- **Adding a provider** (OpenAI, Azure AI) is a new `providers/<name>/` implementing the same two interfaces, selected in `serve.go`. Nothing else changes.

### Thinking on Gemini 3
Gemini 3 models think in levels (`thinkingLevel`) and refuse a `thinkingBudget` of 0 with a bare 400 *Request contains an invalid argument*. The provider (`thinking` in `gemini/Provider.go`) sends a level's budget of 0 to a Gemini 3 model as its least thinking, `MINIMAL` on Flash models and `LOW` on the others; Gemini 1 and 2 models, and every other budget (-1 included), keep the budget.

## `index` feature: `backend/features/index/`

### Change notification
- `volumeservice.FileEvents` is an in-process bus (`features/events/Bus.go`). Delivery is synchronous, so subscribers must not block.
- `collabservice` publishes `FileChanged` after each successful save, and `volumeservice.CreateFile` publishes `FileCreated`. The index subscribes and **debounces** each file (`WithDebounce`, default 5s): collab autosaves every second while someone types, and only the settled text is worth embedding.
- One worker goroutine indexes files one at a time.
- At startup `CreateService` runs `Reindex`, which queues every volume `.md` whose sidecar is missing or stale. That covers edits made while the server was down or outside the app. It also deletes the chunks and sidecars of files that no longer exist.

### Chunking: heading-anchored (`impl/chunker.go`)
1. The file is split into **sections at ATX headings** (`#`–`######`, ignoring fenced code). A section's heading is its heading path, e.g. `Guide > Install`. A section under 256 bytes merges into the next one.
2. Each section is windowed into chunks of **at most 4096 bytes with 512 bytes of overlap** (`WithChunkSize`, `WithOverlap`). A cut falls after the last blank line, else the last newline, else the last space in the second half of the window, and always on a UTF-8 boundary. The overlap's start moves forward to the next line or word.

Why sections: fixed 4 KB windows over the whole file would shift every later chunk after a single insertion, so one typo would re-embed the whole document. Chunks never cross a heading, so an edit only changes the chunks of its own section (`TestChunkEditStaysInSection`).

### Sidecar meta (`indexservice/Meta.go`)
`note.md` gets `.note.md.index.json`, written atomically through a dotted temp file:

```json
{
  "version": 1,
  "model": "gemini-embedding-001",
  "dimensions": 768,
  "fileHash": "…",
  "indexedAt": "2026-10-22T15:04:05Z",
  "chunks": [
    { "hash": "…", "start": 0, "end": 3921, "startLine": 1, "endLine": 58,
      "heading": "Guide > Install", "head": "# Install\n\nRun the installer…", "tail": "…and restart." }
  ]
}
```

- **`hash`** is the first 128 bits of the chunk text's SHA-256 (CRC32 collides too easily at knowledge-base scale). A chunk whose hash is unchanged keeps its embedding: vectors are reused from LanceDB by hash, and only new texts go to the embed service. `fileHash` short-circuits a file that has not changed at all.
- **Version, model or dimensions** that differ invalidate every chunk.
- **Markers** come in both forms:
  - `start`/`end` (UTF-8 byte offsets) and `startLine`/`endLine` jump straight to the chunk while the file is unchanged since it was indexed.
  - `head`/`tail`, the first and last 48 characters of the trimmed chunk (exact substrings), find it again after the file has drifted. The frontend must convert byte offsets to string indices, for example with `TextEncoder`, or simply search for `head`.
- The sidecar is written **last**, after LanceDB, so a failure part way leaves it stale and the file is redone next time.

### LanceDB store (`store/lancestore/`)
- Table `chunks` has the columns:

  | Column | Type | Notes |
  |---|---|---|
  | `volume_id` | string | JSON query results cannot carry a 64-bit TSID as a number |
  | `path` | string | relative to the kb root, `/`-separated, with its extension |
  | `ordinal` | int32 | |
  | `hash` | string | |
  | `text` | string | |
  | `vector` | `fixed_size_list<float32>[dims]` | |

- `Replace(path, chunks)` deletes the file's rows and adds the new ones.
- A table of another dimension is dropped and recreated on open, because its embeddings came from another model.
- **Vector search** uses LanceDB's default L2. The vectors are unit length, so L2 order equals cosine order.
- **BM25 keyword search** uses LanceDB's native FTS index on `text` (English, stemming, no positions).
  - The index is created once the table has rows; LanceDB cannot build one on an empty table.
  - After writes, a debounced (10s) `Optimize` folds new rows into the index. LanceDB also searches rows that are not yet indexed.
- **lancedb-go version:** FTS exists only on lancedb-go's `main` branch; the last release, v0.1.2, rejects FTS queries. `go.mod` therefore pins a `main` commit (`v0.1.3-0.20260509194607-fa14ce29c772`) with no prebuilt native library, and `build.sh` compiles it from source (see Scripts). When lancedb-go releases a version with FTS and binaries, pin it and drop the source build.

### `Find`: weighted Reciprocal Rank Fusion (`impl/Find.go`, `impl/fuse.go`)
Since slice 5 the fusion itself is `features/retrieval/fuse`, shared with the retrieval engine ([ContextRetrieval.md](ContextRetrieval.md#built-in-slice-5)); `impl/fuse.go` only groups chunks into documents. Search results (`indexservice.Match`) now carry each chunk's `Hash`.

Inputs:
- `query`
- `weight` ∈ [0, 1]: the vector search's share, with keyword search getting `1 - weight`
- `count`: at most 100 documents, default 10
- optional `volumeIds`

1. Fetch `K = max(5·count, 50)` candidate chunks from each search: vector search on the query embedded with `purpose=query`, and BM25. With `weight = 1` the keyword search is skipped, and with `weight = 0` the vector search and its embed call are skipped.
2. Score each chunk as `weight/(60 + vectorRank) + (1 − weight)/(60 + keywordRank)`. A search that missed the chunk adds 0.
3. Group by document. A document scores its best chunk and carries its top 3 chunks. Return the best `count` documents.

The response is `hits[]` of `{volumeId, path, type, score, chunks[]}`. Each chunk has the sidecar markers, a ~240-character snippet around the first query term, its fused score and both ranks.

Why not take `weight·count` results from one search and the rest from the other: cosine distances and BM25 scores are on incomparable scales, so neither raw scores nor a split list can be merged meaningfully. RRF works on ranks, keeps `weight` as a dial between the two, and rewards chunks both searches agree on.

### REST
- `POST /api/v1/index/find`
- `POST /api/v1/index/files/index` indexes one file now, forcing it.
- `POST /api/v1/index/reindex` takes `{force}` and returns once the files are queued.

Frontend clients: `src/services/Index.ts`, `Embed.ts` and `Chat.ts` (`Chat.stream` parses the SSE stream with `fetch`).

## Database: `backend/features/db/`
- `db.Model` is the base of every table:
  - `ID uint64` primary key, with `autoIncrement:false`. `BeforeCreate` assigns `features.NewTSID()` when the ID is zero; never use an auto-increment number.
  - `CreatedAt` and `UpdatedAt`.
- TSIDs stay below 2^63 until about 2165, so SQLite and Postgres signed BIGINTs hold them. In JSON they travel as strings, like every uint64.
- `db.Open(db.WithPath(...))` opens SQLite through `github.com/glebarez/sqlite` (pure Go, so no second C library in the CGO link), with WAL, `foreign_keys` and a busy timeout. Postgres for the thin client is a later `WithDialector`.
- `db.Migrate(gdb, models...)` runs `AutoMigrate`. `serve` calls it at startup before creating any service; each feature adds its models there.

## Scripts: `backend/scripts/`
- **`build.sh [-o out/cli] | --test [pkgs] | --vet [pkgs]`** sets the CGO environment (`CGO_CFLAGS` to lancedb-go's headers, `CGO_LDFLAGS` to `liblancedb_go.a` plus `-lm -ldl -lpthread`, or the macOS frameworks) and builds `./cli`, tests or vets.
  - If `third_party/lancedb/<lancedb-go version>/lib/<os>_<arch>/liblancedb_go.a` is missing, it builds the library from the module's Rust sources. It installs rustup into `~/.cargo` and protoc into `third_party/protoc/` if they are missing.
  - That first build takes 20–40 minutes and several GB, so set `CARGO_TARGET_DIR` to a roomy disk. Later builds are plain `go build`.
  - `third_party/` and `out/` are git-ignored.
- **`serve.sh [serve flags…]`**:
  1. Builds `out/cli` and starts `out/cli serve` in the background.
  2. Watches `backend/` with `inotifywait`, ignoring `out/`, `third_party/` and `.git`, for `*.go`, `go.mod` and `go.sum` changes. Without inotify-tools it polls every second.
  3. On a change it builds to `out/cli.next`. If that succeeds, it stops the server (SIGTERM, then a 5s grace period, during which `serve` shuts down cleanly and closes LanceDB), deletes `out/cli`, renames `out/cli.next` to `out/cli` and restarts. A failed build leaves the running server alone.

## Verification
1. `cd backend && scripts/build.sh` produces `out/cli`. `scripts/build.sh --test` runs every test, including LanceDB's (`lancestore`) and the index service's (with in-memory fakes).
2. `GEMINI_API_KEY=… scripts/serve.sh`. On startup it migrates the database and queues existing volume files, and `.…md.index.json` sidecars appear.
3. `curl -XPOST localhost:8080/api/v1/ai/embed -d '{"texts":["hi"],"purpose":"query"}'` returns 768 dimensions. `…/ai/chat/complete` and `…/ai/chat/stream` reply (`curl -N` shows the SSE events).
4. Edit one section of a note in the Markdown Editor. About 5 seconds after the save, only that section's chunks change hash in the sidecar.
5. `POST /api/v1/index/find` with weights 1, 0.5 and 0 returns documents whose chunk markers point at the matching text.
6. Touch a `.go` file while `serve.sh` runs: it rebuilds, swaps the binary and restarts. With a syntax error, the old server keeps serving.
