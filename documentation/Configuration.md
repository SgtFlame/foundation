# Configuration: ConfigurationContributor, the Configuration table and the Settings tab

## Context
Settings are scattered today: `serve` flags (`--embed-model`, `--chat-model`, `--gemini-api-key`, …), env vars, and constants in factories. The chat UI needs user-editable settings, starting with the **chat completion levels** (Fast / Standard / Deep) and their **pricing** ([Usage.md](Usage.md)).

Configuration provides:
- **ConfigurationContributor**: any feature declares a settings section through the `configuration` extension point ([Contributors.md](Contributors.md)).
- **Storage**: values live in SQLite tables `Configuration` and `ConfigurationRevision`, served by a `config` feature.
- **Settings tab**: a right-border tab beside DevTools that edits every section, rendered generically from its schema.

Built: `configservice` with all three scopes, the generic Settings tab, `SecretStore` (the key-file cipher or Vault), and the sections listed under [First sections](#first-sections) and in each *Built in slice N* below. What remains (export and import, browser-only sections, custom editors, the other sections) is in [plans/Configuration.md](plans/Configuration.md).

## Decisions
- **Scopes** are layered **app → tenant → user**; who writes each is in [RBAC.md](RBAC.md) (question 1).
- **Secrets** (Configuration 1): AES-GCM in SQLite with a key file (`<data-dir>/secret.key` or `$FOUNDATION_SECRET_KEY`), built in slice 2. Slice 12 moved them into HashiCorp Vault when the server runs with `--vault-addr` ([Secrets.md](Secrets.md)).
- **Env vs stored** (Configuration 2): flags and env vars **win for secrets** and are **only defaults** for everything else (built in slice 2).

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

**Typed reads in Go**
- `config.Read[T](svc, sectionID, scope) (T, error)` decodes the resolved value into a struct, so services never handle raw JSON.
- Services that cache settings, such as `chatservice` with its levels, subscribe to `events.Bus[ConfigChanged{Section, Scope}]` and reload.

**REST**
- Endpoints live under `/api/v1/config`, with handlers and DTOs per CLAUDE.md, and `GET /config/events`, a Server-Sent Events stream of `ConfigChanged` so other open clients refresh.
- `vite.config.ts` gets a proxy entry for `/api/v1/config`.

## Frontend
- **Client**: `src/services/Config.ts` with `sections`, `get`, `set`, `reset` and `events(onChange)`.
- **Store**: `src/settings/configStore.ts` caches resolved sections and refetches on SSE events.
  - `useConfig(sectionId)` returns `{ value, sources, revision }`.
  - `useSetting(sectionId, path)` returns one value. Components use it to read settings, for example the composer reads `ai.chat.levels`.
- **Settings tab** (`src/tabs/settings/`)
  - It is a right-border tab next to DevTools, contributed to `workbench.borderTab` (built in slice 1) from `src/settings/contributions.ts`: `{ id: 'settings', source: 'settings', name: 'Settings', location: 'right', component: lazy(...) }`.
  - **Header**: a search box that filters by title, description and key, plus a **scope selector** with all three scopes (since slice 11; see [Built in slice 11](#built-in-slice-11)).
  - **Left**: a category tree built from the `category` paths of every contributed section.
  - **Right**: the forms of the selected category's sections. Each section is either the generic form or the contributor's `Editor`.
  - **Per leaf**: a source badge (*default*, *env*, *app*, …) and "Reset to inherited". A `restart` key shows a "needs restart" note.
  - **Per section**: dirty tracking, **Save** / **Discard**, server validation errors shown inline at their paths, and 409 handled as "changed elsewhere — reload or overwrite".

## First sections
The sections the server declares (the rest of the plan's list is in [plans/Configuration.md](plans/Configuration.md)):
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
- **`cognition`**: default run limits (steps, wall time, tokens, cost, loop iterations) and trace retention.
- **`retrieval`**, **`retrieval.feedback`** and **`chat.context.presets`**: the retrieval engine's constants, grounding multiplier and dial presets ([ContextRetrieval.md](ContextRetrieval.md)).
- **`chat.context.defaults.<id>`**: generated from each ChatContextSettingsContributor's defaults.
- **`logging`**: the VictoriaLogs host and port, optional credentials, the minimum level stored, and frontend log forwarding ([Logging.md](Logging.md)).

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

## Built in slice 4
- **New sections**
  - **`chat`** (AI › Chat): the system prompt, the rounds of history sent, and the title level, at the app, tenant and user scopes.
  - **`chat.context.defaults.workbench`** (AI › Chat › Context).
  - **`usage.capture`** (System).
- **Sections derived from another point**: a section can be declared from another point's contributions. `conversationsettings.Contribute` adds a `chat.context.defaults.<id>` section for every `chat.contextProvider`, from the provider's `Schema` and `Defaults`, so it runs after them in `serve.go`. That is how the browser's per-conversation controls get defaults Settings can edit, while browser-only sections cannot be stored.
- **Reading at user scope**: the Chat editor reads `ai.chat.levels`, `chat` and the context defaults at the `user` scope, the values that apply to the person chatting. It reads `ai.models` at `app`, its only scope.
- **The levels are live**: `chatservice` resolves levels through `config.Read` on every call, so a change in Settings applies to the next message. `--chat-model` is now only the model for calls that name no level.

## Built in slice 5
- **New sections**
  - **`retrieval`** and **`retrieval.feedback`** (AI › Retrieval, app scope): the pipeline's constants and the grounding multiplier.
  - **`chat.context.presets`** (AI › Chat › Context, all scopes): the RAG Search preset chips.
  - **`chat.context.defaults.rag`**: derived from the `rag` provider, like the workbench's.
- **A `ref` may be empty**: `chat.context.defaults.rag`'s `preset` refers to `chat.context.presets#presets`, and an empty value (*Custom*) passes, as `Set` only checks non-empty refs.
- See [ContextRetrieval.md › Built in slice 5](ContextRetrieval.md#built-in-slice-5).

## Built in slice 6
- **`ai.chat.stages`** (AI › Chat › *Internal stages*, all scopes): `match` and `memory`, levels by reference to `ai.chat.levels`, both Fast. A stage naming no level, or one that does not exist, uses the default level.
- **`chat.context.defaults.chronicle`** (AI › Chat › Context): `enabled`, `topK` (5), `match` and `update`, declared from the `chronicle` provider like every provider's defaults. A conversation's `chronicle` settings also hold `checked` (the entries checked in the Chronicles view), which is not in the schema and so never a default.
- **`chat.context.defaults.rag`** gains `sources.chronicle` (on).
- **`chat`**: `historyRounds` is now titled *Window*, with a default of 15 (was 50).

## Built in slice 7
- **`cognition`** (AI › Cognition, app scope): `maxSteps` (1000) and `maxWallSeconds` (300) per run, `maxValueKB` (256: a larger traced value keeps only a preview), and retention, `keepRuns` (50) and `keepDays` (7). A trace is kept while it is among its graph's last `keepRuns` runs *or* younger than `keepDays` (Cognition question 5).

## Built in slice 8
- **`tools`** (AI › Tools, app and tenant scope): `maxSteps` (8), `maxResultKB` (32), `timeoutSeconds` (30), `clientTimeoutSeconds` (30) and `disabled`, a list of tool names never offered. See [Tools.md › Built in slice 8](Tools.md#built-in-slice-8).
- **`chat.context.defaults.tools`** (AI › Chat › Context › Tools): `{"enabled": true}`, the default of each conversation's Tools switch.

## Built in slice 11
- **Who writes each scope** (RBAC question 1): `config.write` on `app/config:<id>` (*Super Administrator*), on `tenant:<id>/config:<id>` (*Owner* and *Administrator*), and on `user:<id>/config:<id>` (everyone, their own: `config` is a user-owned resource type). A patch that touches a `secret` field also needs `config.writeSecrets`. Reading is the `self` class: everyone.
- **The scope selector** in Settings is live. It opens at *App* for those who may write it, and at *User* for everyone else. Its tooltip says when a scope is read-only for you.
- A section you may not write at the chosen scope shows a *Read-only* chip in place of Save and Discard, and every control is disabled. List controls and secret inputs now also respect a field's allowed scopes.
- The browser reads settings at the **user scope** by default (`useConfig`, `useSetting`), so tenant and user overrides take effect in the browser as they do on the server.

## Built in slice 12
See [Secrets.md › Built in slice 12](Secrets.md#built-in-slice-12).
- **`SecretStore`**: `configservice` no longer calls `config.Cipher` itself. It keeps secrets through a `config.SecretStore` (`configimpl.WithSecrets`): `config.CipherStore` without Vault (`WithCipher`, as before), and `vaultstore` with it, which leaves `{"$vault": {"version": n}}` in the row. A save writes only the secrets it changed, and deletes those it cleared.
- **A new source, `server`**: `configimpl.WithServerSecret` sets the server's own secret from Vault's bootstrap bundle, under every stored scope (`server/gemini-api-key` for `ai.providers` › `gemini.apiKey`). Layering is now defaults → server → env (plain keys) → app → tenant → user, with a flag or env var still winning for secrets.
- **`ai.providers` at every scope** (Secrets question 7): a tenant or a user may set their own key, used for their calls.
- **Who writes secrets**: plain settings need `config.write`; secrets need `secret.write` on `app` at the app scope (an *Application Administrator*), and `config.writeSecrets` at a tenant or user scope. Settings lets a user edit only the fields they may write, and a secret they may not write at the app scope says "Managed by an Application Administrator".
- **No hint with Vault** (Secrets question 4): the view of a secret is `{set}` only. Without Vault the hint stays.
- A value that could not be read from the store (Vault unreachable) is dropped from that read, and the resolved section is not cached, so it is read again next time.
- `Config.set` in the browser is not captured in DevTools' Network tab when the patch carries a secret.

