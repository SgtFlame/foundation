# AI services and the search index: what remains

The provider-neutral `embedservice` and `chatservice`, the Gemini provider, and the hybrid search index are built; see [AIChatEmbed.md](../AIChatEmbed.md). Tool calling in `chatservice` (slice 8) and cached and thinking token counts in `Usage` (slice 2) are built too.

## TODO
### Not built yet
- **More providers**: `providers/openai` and `providers/azure` behind the same interfaces, including their tool-calling mappings (`tools` / `tool_calls`; see [Tools.md](Tools.md)).
- **lancedb-go**: move to a released version with full-text search and prebuilt binaries, then remove the source build.
- **A vector index** (IVF-PQ/HNSW) on `chunks.vector` once there are enough rows. Flat search is exact and fast enough until then.
- **Embedded NATS** (pub/sub, JetStream KV/Object Store) when services run as separate processes.
- **Removing a file through the app**: `volumeservice` has no delete yet. Today `Reindex` notices missing files.
- **Tenant- and user-scoped indexes**, when `VolumePath` tenants and users are used (with per-tenant knowledge base roots, [MultiTenant.md](MultiTenant.md)).
- **Index titles**: a chunk made of several small sections takes the *last* section's heading, so its citation can be titled by a heading near its end (`mergeSmall` in the chunker).
