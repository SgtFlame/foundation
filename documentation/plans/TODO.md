# Open questions

Open questions now live in the plan they belong to, each with a **proposal** (what the plan currently assumes) and a ***Decision*** line to fill in. This page is the index of questions; [Index.md](Index.md) indexes the plans and their build order.

| Plan | Questions |
|---|---|
| [ChatCompletion.md](ChatCompletion.md) › Gaps | document chat persistence, Manuscript, auto level routing, what "Top-K" means (built as segments in slice 5, to decide after seeing); tool calling was built in slice 8 |
| [ContextRetrieval.md](ContextRetrieval.md) › Open questions | Axis 2 mapping, what thumbs down does (both built on their proposals in slice 5, to decide after seeing), past rounds as a memory source (not built in slice 6, to decide after seeing) |
| [Chronicle.md](Chronicle.md) › Open questions | Volumes as memory (not built in slice 6, on its proposal); slice 6's decisions to review after seeing are in *Built in slice 6* |
| [Cognition.md](Cognition.md) › Open questions | Nexus storage (before slice 9, still open; slice 9 is skipped for now), background and durable runs; collaborative graph editing (shared Yjs drafts), template helpers (global) and trace retention (last N runs or N days) were decided before slice 7; slice 7's decisions to review after seeing are in *Built in slice 7* |
| [Tools.md](Tools.md) › Open questions | MCP; citing tool results (built on its proposal in slice 8, to decide after seeing); first tools and confirmation (per call) were decided before slice 8; slice 8's decisions to review after seeing are in *Built in slice 8* |
| [MultiTenant.md](MultiTenant.md) › Open questions | feedback scope (built per tenant in slice 5, to decide after seeing); JWT lifetime, bootstrap, OIDC and administration before RBAC were decided before slice 10; slice 10's decisions to review after seeing are in *Built in slice 10*, and slice 11's (the global tenant, Chronicle sharing) in *Built in slice 11* |
| [RBAC.md](RBAC.md) › Open questions | groups (not built in slice 11), custom system roles and time-limited grants (built on their proposals in slice 11, to decide after seeing); who edits settings (as proposed), deny rules (allow-only), Tenant Administrator and content (no content) and the `Admin` flag (becomes *Super Administrator*) were decided before slice 11; slice 11's decisions to review after seeing are in *Built in slice 11* |
| [Configuration.md](Configuration.md) › Open questions | none open (secret storage and env vs stored were decided in slice 2) |
| [Logging.md](Logging.md) | none open (slice 3's decisions are in *Built in slice 3*) |
| [Secrets.md](Secrets.md) › Open questions | none open: unattended restarts (not built in slice 12), where to unseal, hints and losing Vault (built on their proposals in slice 12) are to decide after seeing; token form (a wrapped SecretID) and keys set by users (every scope) were decided before slice 12, and Application Administrator before slice 11; slice 12's decisions to review after seeing are in *Built in slice 12* |
| [Usage.md](Usage.md) › Open questions | currency, retention, budgets, VictoriaLogs (prompt text was decided before slice 4: an opt-in debug capture) |
| [Contributors.md](Contributors.md) › Open questions | runtime plugins |
| [WorkbenchContext.md](WorkbenchContext.md) › Open questions | snapshot size, recent history, editor content, asking, several browser tabs (1, 2, 3 and 6 are built on their proposals in slice 4, and 4 in slice 8, to decide after seeing); the example domain was decided before slice 8 (only an illustration) |
