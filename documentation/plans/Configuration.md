# Configuration: ConfigurationContributor, the Configuration table and the Settings tab

## Context
Settings are scattered today: `serve` flags (`--embed-model`, `--chat-model`, `--gemini-api-key`, …), env vars, and constants in factories. The chat UI needs user-editable settings, starting with the **chat completion levels** (Fast / Standard / Deep) and their **pricing** ([Usage.md](Usage.md)).

This plan adds:
- **ConfigurationContributor**: any feature declares a settings section through the `configuration` extension point ([Contributors.md](Contributors.md)).
- **Storage**: values live in SQLite tables `Configuration` and `ConfigurationRevision`, served by a `config` feature.
- **Settings tab**: a right-border tab beside DevTools that edits every section, rendered generically from its schema.

Status: partly built. Slice 2 built `configservice` (app scope in the UI), the generic Settings tab and the sections `ai.providers`, `ai.models` and `ai.chat.levels`; see [Built in slice 2](#built-in-slice-2).

Decision: scopes are layered **app → tenant → user**. The v1 UI edits the app scope; the tables and resolution support all three.

Decision (secrets): encrypted in SQLite with AES-GCM for now. A later slice bootstraps secrets from HashiCorp Vault instead; see [Secrets.md](Secrets.md).

Decision (env vs stored): flags and env vars **win for secrets** and are **only defaults** for everything else.

## Sections: the contributed unit
A **section** is a named group of settings, declared in the same shape on both sides:

```ts
{
  id: 'ai.chat.levels',          // dotted, owner-prefixed
  title: 'Chat levels',
  description: '…',
  category: ['AI', 'Chat'],      // the Settings tree path
  scopes: ['app', 'tenant', 'user'],
  schema: { … },                 // see below
  defaults: { … },
  version: 1,
  migrate?(old, fromVersion) => new,
}
```

**Schema**: a small JSON-Schema subset that is enough to render forms and validate on the server.
- **Types**: `string`, `number`, `integer`, `boolean`, `enum` (with labels), `secret`, `object` (named properties), `array` (of any type; an array of objects renders as a table).
- **Annotations**: `title`, `description`, `min`, `max`, `step`, `pattern`, `format` (`url`, `multiline`, `currency`, `duration`), `unit`, `scopes` (per-key override of the section's scopes), `restart: true` (takes effect after restart) and `ref: '<sectionId>#<path>'`. A `ref` renders as a select over another section's array items, for example a level's `modelRef` picks from `ai.models`.

**Ownership**
- **Backend sections**: a section the backend reads is declared in Go and registered on the `config.Section` point. `GET /config/sections` publishes those declarations, and the frontend adds them to its registry. The backend validates every write to its own sections against the schema.
- **Client-only sections** (`ui.*`, such as appearance or editor preferences) are declared in a frontend `contributions.ts` and validated in the browser.
- **Custom editors**: a frontend contributor may add `Editor`, a React component taking `{ value, onChange, schema, errors }`, for a section it does not own. The pricing table and the levels editor use this. Without one, the generic form renders.
- **Schema versions**: a stored value carries `SchemaVersion`. On read, `migrate` brings it up to date, and the next save writes the new version.

## Storage: `backend/features/config`
**Tables** (GORM; each embeds `db.Model`, so IDs are TSIDs):

| Table | Fields | Notes |
|---|---|---|
| `Configuration` | `Scope`, `Section`, `Value` (JSON), `SchemaVersion`, `Revision`, `UpdatedBy` | unique (`Scope`, `Section`). `Value` holds **only the keys this scope overrides**. `Scope` is `app`, `tenant:<id>` or `user:<id>`. |
| `ConfigurationRevision` | `ConfigurationID`, `Revision`, `Value`, `UpdatedBy` | append-only history, for audit and "revert to this version" |

**Resolution**, from lowest to highest precedence:
1. contributor defaults
2. CLI flags and env vars
3. `app`
4. `tenant`
5. `user`

A key only takes values from the scopes it allows. The resolved view returns each leaf's **source** (`default`, `env`, `app`, …), which the UI shows.

Today's `serve` flags remain as bootstrap fallbacks at step 2, so a fresh install with `GEMINI_API_KEY` set still works.

**Secrets** (`secret` fields, such as provider API keys)
- They are never returned to the client. A read returns `{ set: true, hint: '…f3a9' }`, and a write replaces the value or clears it.
- They are encrypted at rest with AES-GCM. The key comes from `$FOUNDATION_SECRET_KEY`, or else from `<data-dir>/secret.key`, created with mode 0600 on first use.
- An env or flag value still wins (step 2 sits above the default). The Settings UI shows "set by environment" and disables the field.

## `configservice` (`features/config/services/configservice`)

| Method | Purpose |
|---|---|
| `Sections()` | All registered backend sections with their schemas. |
| `Get{Section, Scope}` | The resolved value as seen from `Scope`, with each leaf's source and the stored `Revision`. |
| `Set{Section, Scope, Patch, Revision}` | Validate, merge and store, then write a revision. Returns 409 when `Revision` is stale, so concurrent edits are not lost. |
| `Reset{Section, Scope, Keys?}` | Drop overrides at this scope, so values are inherited again. |
| `Export{Scopes}` / `Import{Document, Mode}` | A JSON document of sections, for backups and moving between machines. Secrets are excluded from exports. |

**Typed reads in Go**
- `config.Read[T](svc, sectionID, scope) (T, error)` decodes the resolved value into a struct, so services never handle raw JSON.
- Services that cache settings, such as `chatservice` with its levels, subscribe to `events.Bus[ConfigChanged{Section, Scope}]` and reload.

**REST**
- Endpoints live under `/api/v1/config`, with handlers and DTOs per CLAUDE.md, and `GET /config/events`, a Server-Sent Events stream of `ConfigChanged` so other open clients refresh.
- `vite.config.ts` gets a proxy entry for `/api/v1/config`.

## Frontend
- **Client**: `src/services/Config.ts` with `sections`, `get`, `set`, `reset`, `export`, `import` and `events(onChange)`.
- **Store**: `src/settings/configStore.ts` caches resolved sections and refetches on SSE events.
  - `useConfig(sectionId)` returns `{ value, sources, revision }`.
  - `useSetting(sectionId, path)` returns one value. Components use it to read settings, for example the composer reads `ai.chat.levels`.
- **Settings tab** (`src/tabs/settings/`)
  - It is a right-border tab next to DevTools, contributed to `workbench.borderTab` (built in slice 1) from `src/settings/contributions.ts`: `{ id: 'settings', source: 'settings', name: 'Settings', location: 'right', component: lazy(...) }`.
  - **Header**: a search box that filters by title, description and key, plus a **scope selector**. Only *App* is enabled until there is auth.
  - **Left**: a category tree built from the `category` paths of every contributed section.
  - **Right**: the forms of the selected category's sections. Each section is either the generic form or the contributor's `Editor`.
  - **Per leaf**: a source badge (*default*, *env*, *app*, …) and "Reset to inherited". A `restart` key shows a "needs restart" note.
  - **Per section**: dirty tracking, **Save** / **Discard**, server validation errors shown inline at their paths, and 409 handled as "changed elsewhere — reload or overwrite".
  - **Footer**: Export and Import.

## First sections
- **`ai.providers`**: `gemini.apiKey` (secret), with `openai` and `azure` blocks later. The provider factories read from here, and `--gemini-api-key` is the fallback.
- **`ai.models`**: the **model catalog**, an array of model entries with these fields.
  - `id` (a local name), `provider`, `model` (the provider's id), `kind` (`chat` | `embed`) and `contextWindow`.
  - `pricing`:
    - `currency`;
    - `inputPerMTok`, `cachedInputPerMTok`, `outputPerMTok` (thinking tokens bill as output), `embedPerMTok`;
    - optional `tiers` (`{ aboveInputTokens, inputPerMTok, outputPerMTok }`, for Gemini Pro's >200k prompt price);
    - `effectiveFrom`.
  - **Pricing lives on the model, not on the level**, so two levels that share a model share one price. Cost is computed and stored when a call is recorded, so editing a price never rewrites history ([Usage.md](Usage.md)).
- **`ai.chat.levels`**: an array of levels, by default **Fast** ⚡ (`gemini-2.5-flash-lite`), **Standard** 💡 (`gemini-2.5-flash`) and **Deep** 🧪 (`gemini-2.5-pro`). Each level has `id`, `label`, `icon`, `modelRef` (→ `ai.models`), `temperature`, `maxOutputTokens`, `thinkingBudget` and `default: bool`.
- **`ai.chat.stages`**: the level each internal call uses, for example Chronicle stage 1 → Fast and the memory update → Fast, so the user's choice only affects the reply.
- **`ai.embed`**: `modelRef` and `dimensions`. Saving a change warns that it triggers a full reindex.
- **`chat.modes`**: the modes offered in the composer (`id`, `label`, `icon`, `graphRef` to a published Cognition graph, `default`). See [ChatCompletion.md](ChatCompletion.md) and [Cognition.md](Cognition.md).
- **`cognition`**: default run limits (steps, wall time, tokens, cost, loop iterations) and trace retention.
- **`retrieval`**, **`retrieval.feedback`** and **`chat.context.presets`**: the retrieval engine's constants, grounding multiplier and dial presets ([ContextRetrieval.md](ContextRetrieval.md)).
- **`chat.context.defaults.<id>`**: generated from each ChatContextSettingsContributor's defaults.
- **`index`**: `debounce`, `chunkSize`, `overlap` (`restart`).
- **`logging`**: the VictoriaLogs host and port, optional credentials, the minimum level stored, and frontend log forwarding ([Logging.md](Logging.md)).
- **`ui.appearance`** (client-only): theme and density.

## Open questions
Moved here from [TODO.md](TODO.md):
1. **Secrets**: AES-GCM with a key file (`<data-dir>/secret.key` or `$FOUNDATION_SECRET_KEY`), or the OS keychain where available? *Decision*: AES-GCM in SQLite for now (built in slice 2). A new last slice starts the server sealed and takes its secrets from HashiCorp Vault, with scripts and a write-only, administrator-only Secrets editor: [Secrets.md](Secrets.md).
2. **Env vs stored values**: the proposal is that flags and env vars *always* win for secrets, and are only defaults for everything else. *Decision*: as proposed (built in slice 2).

Who may edit which scope has moved to [RBAC.md](RBAC.md).

## Built in slice 2
**Backend: `features/config`**
- **`Section`**: as above, plus two optional hooks.
  - `Migrate` brings an older stored value up to date when it is read.
  - `Check` validates what the schema cannot express, such as "exactly one default level". It runs after the schema has passed.
- **Values**: a section's value is always a JSON object.
- **Schema** (`Schema.go`): the subset above, with these concrete forms.
  - An enum lists `options: [{value, label}]`.
  - An array of objects names the property unique per item in `key` (default `id`), and repeats are rejected.
  - `ref` is `"<sectionId>#<arrayPath>"` and names an item by its key, e.g. a level's `modelRef` is `"ai.models#models"`.
  - Unknown keys fail validation.
  - Schemas are written as raw JSON in Go, so their key order is the form's order.
- **Paths**: errors and sources address leaves by dotted paths, array items by index (`levels.0.modelRef`). Arrays are leaves: they are inherited and overridden as a whole.
- **Tables**: `Configuration` and `ConfigurationRevision`, as above. `Scope` is `app`, `tenant:<id>` or `user:<id>`, and every `Set` or `Reset` appends a revision.
- **Secrets**
  - **At rest**: a stored secret is `{"$secret": "<base64 nonce+ciphertext>"}`, sealed by `config.Cipher` (AES-256-GCM). Its section and path are the associated data, so a ciphertext cannot be moved to another key.
  - **The key**: `$FOUNDATION_SECRET_KEY` (base64 of 32 bytes), or else `<data-dir>/secret.key`, created with mode 0600 on first use.
  - **A key that no longer decrypts** (the key file was replaced) is dropped and logged. The secret then shows as unset and can be entered again.
  - **What clients see**: `{set, hint}`, where the hint is the last four characters of a secret of 12 or more.
- **Env layer**: `configimpl.WithEnv(section, path, value)`. For now only `ai.providers` › `gemini.apiKey` is bound, to `--gemini-api-key` or `$GEMINI_API_KEY`. `--chat-model` and `--embed-model` remain provider defaults until levels drive chat (slice 4).

**`configservice`**
- **Go-only typed reads**: `Resolved` (from `config.Reader`) and `config.Read[T]`. They resolve as the principal sees the section, secrets in plain text, and the result is cached until the section changes.
- **Changes**: every `Set` and `Reset` publishes `config.Changed{Section, Scope}` on `config.Changes`.
- **REST**: `GET /config/sections`, `POST /config/get`, `POST /config/set`, `POST /config/reset`, and `GET /config/events` (SSE `changed` events, with a keep-alive comment every 30s).
- **Scopes in requests**: a request's `scope` is a kind (`app`, `tenant` or `user`). The IDs come from the principal, never from the request.
- **`Set`**
  - It takes a **JSON merge patch** (RFC 7386): `null` inherits a key again, and arrays are replaced whole.
  - It also takes the view's `revision`; a stale revision fails with 409 `config_conflict`.
  - Every patched leaf must exist and allow the scope. The resolved result must then pass the schema, `Check` and every `ref`.
  - A failure is 400 `invalid_settings` with `errors: [{path, message}]`.
- **The Gemini key follows Settings**: `serve` reads it through `config.Read` and calls `gemini.Provider.SetAPIKey` again on every `ai.providers` change, so a key entered in Settings works without a restart.

**Frontend**
- **Client**: `src/services/Config.ts`. `set` resolves a 400 that carries `errors`, so forms can show them inline; every other failure throws `ServiceError`. `events` is an `EventSource`.
- **Store**: `src/settings/configStore.ts`.
  - It holds `configStore`, `useConfig(section, scope)` and `useSetting(section, path)`.
  - One event stream refetches the cached sections that changed.
  - `loadServerSections()` adds the server's sections to the `configuration` point with `server: true`, keeping a frontend `Editor` contributed for the same id.
- **Settings tab**: `src/tabs/settings/`, a right-border tab contributed from `src/settings/contributions.ts`.
  - It has the search box (over titles, descriptions and keys), the scope selector (Tenant and User are disabled until sign-in), the category tree, and one `SectionPanel` per section.
  - **`SectionPanel`** records the view editing started from. Its patch is computed against that view, and its revision goes with the patch. Save and Discard; inline and listed errors; and "changed elsewhere" with **Reload** or **Overwrite**.
  - **`SchemaField`** is the generic form.
    - **Every leaf**: a source badge, "reset to inherited" (sent as `null` on save), and "needs restart".
    - **Secrets**: a write-only password field with *set …hint*, Clear, and "set by environment" (disabled).
    - **`ref`**: a select over the other section's items.
    - **Lists of objects**: expandable items with add, move and remove.
- **No custom editors yet**: the model catalog, pricing tiers included, renders in the generic form. ★ Decide after seeing it whether the catalog needs a table editor.

**Not built yet**
- `Export` and `Import`.
- Tenant and user scopes in the UI.
- Storage for browser-only (`ui.*`) sections.
- The other first sections: `ai.chat.stages`, `ai.embed`, `chat.modes`, `cognition`, `retrieval`, `index` and `logging`.
- Schema `migrate` in the browser.
