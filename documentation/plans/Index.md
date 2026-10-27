# Plans

What is still to build. Each plan holds the design for its remaining work and a **TODO** section: open questions (each with a *Decision* line), what to review after seeing it ★, and what is not built yet. What is already built is documented in [../Index.md](../Index.md); how it was built, slice by slice, is in [../BuildHistory.md](../BuildHistory.md).

| Plan | Remaining |
|---|---|
| [Cognition.md](Cognition.md) | The Nexus slice (slice 9, skipped: needs question 1, Nexus storage, answered first); editing as shared Yjs drafts and publishing; the Prompt Editor; debugging, loops and Replay; `Send` as a mode graph; run token and cost limits; the Palette and the toolbar |
| [ChatCompletion.md](ChatCompletion.md) | The Markdown Editor's Chat tab and editing documents with the AI; modes; editing a user bubble; chip corrections; sampling overrides; renaming and deleting conversations; three open questions |
| [Chronicle.md](Chronicle.md) | Custom entry types with summarizer graphs; the graph view; editing and deleting rounds; undoing a round's changes; saving checks at once; more sharing |
| [ContextRetrieval.md](ContextRetrieval.md) | The `rounds` source; the budget and volume-filter controls; the *Flagged segments* widget; citation relocation; questions 1 to 3 to confirm |
| [Tools.md](Tools.md) | MCP (open question), web tools, tenant policies, rate limits, unattended writes, the Nexus tools, tools as Cognition nodes |
| [WorkbenchContext.md](WorkbenchContext.md) | The `document` surface, `workbench.subjectDescriber`, `editor.getSelection`, the Assistant mode; questions 1 to 4 and 6 to confirm |
| [MultiTenant.md](MultiTenant.md) | OIDC; `db.ForTenant`; per-tenant Volumes; purging deleted tenants; sign-in rate limits, invitations and password reset |
| [RBAC.md](RBAC.md) | Groups; `authz.Filter`; the audit viewer; `PolicyVersion` checks; shares on files and graphs; enforcing `usage.viewAll` and the tool permissions |
| [Configuration.md](Configuration.md) | Export and import; browser-only `ui.*` sections; custom section editors; the remaining sections (`ai.embed`, `chat.modes`, `index`, `ui.appearance`) |
| [Usage.md](Usage.md) | Budgets, retention, export, currency (four open questions); more Dashboard widgets and saving its layout |
| [Logging.md](Logging.md) | Saved queries; `Stats`/`StatsRange` charts; the error toast's link; full logging from the Explorer; alerts |
| [Secrets.md](Secrets.md) | `--vault-token-file`; re-reading the bundle without a restart; Vault TLS and namespaces; an audit viewer; questions 2, 3, 4 and 6 to confirm |
| [Contributors.md](Contributors.md) | `chat.composerAction`, `workbench.subjectDescriber`, `cognition.nodeView`; the command palette; runtime plugins (open question) |
| [AIChatEmbed.md](AIChatEmbed.md) | More providers; a released lancedb-go; a vector index; deleting files; per-tenant indexes; index titles |
| [MarkdownEditor.md](MarkdownEditor.md) | Detecting external file changes; remote cursors in WYSIWYG |
| [Backlog.md](Backlog.md) | Candidate surfaces no plan owns yet (conversation history, audit viewer, index administration, notifications, …) and app-wide issues such as the dark-preference contrast |
