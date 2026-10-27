# Documentation

What is built. Each document describes a feature as it works now, with what was decided while building it. What remains is in [plans/Index.md](plans/Index.md), and how it was built, slice by slice, is in [BuildHistory.md](BuildHistory.md).

| Feature | What it does |
|---|---|
| [DevTools.md](DevTools.md) | The DevTools border tab: the axios capture with its Network viewer, the browser's structured `log` and its Log viewer |
| [MarkdownEditor.md](MarkdownEditor.md) | Collaborative editing of a Volume's markdown file in three views (Live Preview, Source, WYSIWYG) over one Yjs document, with `collabservice` |
| [AIChatEmbed.md](AIChatEmbed.md) | Provider-neutral `embedservice` and `chatservice` with the Gemini provider, and the hybrid (vector and BM25) search index over Volumes in LanceDB |
| [Contributors.md](Contributors.md) | The extension registry, frontend and backend, and every extension point features contribute to |
| [Logging.md](Logging.md) | One builder for errors and logs, the request trace, storage in VictoriaLogs, browser log forwarding, the Log Explorer and full logging for one user |
| [Configuration.md](Configuration.md) | Settings sections declared by features, stored per app, tenant and user scope with secrets, and the generic Settings tab |
| [Usage.md](Usage.md) | Token and cost recording for every AI call, the opt-in debug capture, and the Dashboard's *Usage & cost* widget |
| [ChatCompletion.md](ChatCompletion.md) | AgentOS › Chat: conversations and rounds, levels, context providers, citations, feedback, Chronicle memory and the tool loop |
| [ContextRetrieval.md](ContextRetrieval.md) | The retrieval engine: vector and keyword search over Volumes and Chronicles, fused by RRF and weighted by two dials and by feedback, behind RAG Search |
| [Chronicle.md](Chronicle.md) | Chronicle memory: entries, stage 1 context matching and the memory update, the Chronicles side view, the entry editor, and sharing |
| [Cognition.md](Cognition.md) | The Cognition Engine's skeleton (graphs, nodes, traced runs, the built-in *Personal* graph, the `Agent` node) and the read-only Cognition Editor |
| [Tools.md](Tools.md) | Function calling: the tool loop, the first server tools, confirmation cards, and client tools that run in the browser |
| [WorkbenchContext.md](WorkbenchContext.md) | The browser's record of open editors and their subjects, sent to chat as a snapshot, and the client tools that read and act on it |
| [MultiTenant.md](MultiTenant.md) | Tenants, local accounts and sessions, the application and tenant databases, the global tenant and Chronicle sharing |
| [RBAC.md](RBAC.md) | Resource-based access control: `authz.Check` over role grants, built-in and custom roles, the Role Editor and the Share dialog |
| [Secrets.md](Secrets.md) | The sealed start from HashiCorp Vault, `SecretStore`, the operator's scripts, and the write-only Secrets editor |
| [BuildHistory.md](BuildHistory.md) | The plans' dependencies, the seams that broke their cycles, and each tracer-bullet slice with what it decided |
