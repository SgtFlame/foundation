# Configuration: what remains

`configservice` with the app, tenant and user scopes, the generic Settings tab, `SecretStore` and the server's sections are built; see [Configuration.md](../Configuration.md).

## Export and import
- `Export{Scopes}` / `Import{Document, Mode}`: a JSON document of sections, for backups and moving between machines. Secrets are excluded from exports.
- The client gains `export` and `import`; the Settings tab's footer has **Export** and **Import**.
- `ConfigurationRevision` already keeps every value; a "revert to this version" action in Settings uses it.

## Browser-only sections and custom editors
- **Client-only sections** (`ui.*`, such as appearance or editor preferences) are declared in a frontend `contributions.ts` and validated in the browser. They need storage (today only server-declared sections can be stored), and schema `migrate` in the browser.
- **Custom editors**: a frontend contributor may add `Editor`, a React component taking `{ value, onChange, schema, errors }`, for a section it does not own. The pricing table and the levels editor are the first candidates. Without one, the generic form renders.

## The remaining first sections
- **`ai.embed`**: `modelRef` and `dimensions`. Saving a change warns that it triggers a full reindex.
- **`chat.modes`**: the modes offered in the composer (`id`, `label`, `icon`, `graphRef` to a published Cognition graph, `default`). See [ChatCompletion.md](ChatCompletion.md) and [Cognition.md](Cognition.md).
- **`index`**: `debounce`, `chunkSize`, `overlap` (`restart`).
- **`ui.appearance`** (client-only): theme and density. A theme that follows the browser's dark preference also fixes the app-wide contrast issue in [Backlog.md](Backlog.md).
- **`ui.dashboard`** (client-only): the Dashboard's hidden widgets and layout ([Usage.md](Usage.md)).
- **`ai.budget`**: daily and monthly limits per scope ([Usage.md](Usage.md)).
- `ai.providers` gains `openai` and `azure` blocks with those providers ([AIChatEmbed.md](AIChatEmbed.md)).

## TODO
### Review after seeing it ★
- **The model catalog in the generic form** (slice 2): decide whether the catalog, pricing tiers included, needs a table editor.

### Not built yet
- Export and import, and reverting to a revision.
- Storage for browser-only (`ui.*`) sections, and schema `migrate` in the browser.
- Custom section editors.
- The sections `ai.embed`, `chat.modes`, `index`, `ui.appearance`, `ui.dashboard` and `ai.budget`.
- App-scope settings in the application database, with the `db.ForTenant` resolver ([MultiTenant.md](MultiTenant.md)).
- **Seen while verifying**: the scope selector's *App* tooltip says "read-only for you" to an *Application Administrator*, who may write its secrets; and a read-only field still shows its reset button.
