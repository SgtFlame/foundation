# Secrets: bootstrapping from HashiCorp Vault

## Context
Slice 2 stores secret settings (provider API keys today; OIDC client secrets, the JWT signing key and log-store credentials later) in the `Configuration` table, encrypted with AES-GCM. The key comes from `$FOUNDATION_SECRET_KEY` or `<data-dir>/secret.key` ([Configuration.md](Configuration.md#built-in-slice-2)). That is fine on a developer machine, but on a server the key sits beside the data it protects. Anyone who can read the data directory can read every secret.

With `--vault-addr`, secrets live in **HashiCorp Vault**, so that nothing secret is stored on the server host:
- The backend **starts sealed**. It opens its configured port, serves only the bootstrap endpoints, and waits for a token.
- With the token it logs in to Vault and reads the rest of its secrets. Only then does it finish starting: it migrates the database, creates the services and serves the API.
- **Bash scripts** set up Vault and maintain the secrets.
- A **Secrets editor** and a `secretsservice` maintain secrets from the UI. They are **write-only** (a secret's value is never returned) and only an **Application Administrator** can use them.

Built in slice 12. The sections below are the design as planned; [Built in slice 12](#built-in-slice-12) records where the build differs, and wins where they disagree. It depends on Identity (slice 10) for users and on RBAC (slice 11) for the Application Administrator role and the `secret` resource type (`secret.list`, `secret.write`). What remains (unattended restarts, re-reading the bundle, Vault TLS, an audit viewer) is in [plans/Secrets.md](plans/Secrets.md).

## Decisions
1. **Token form** (before slice 12): a response-wrapped, single-use AppRole SecretID, made by `unseal.sh`.
5. **Application Administrator** (asked with RBAC 7, before slice 11): a new built-in app-level role holding only `secret.*`; *Super Administrator* includes it.
7. **Keys set by users** (before slice 12): at the app, tenant and user scopes, the narrowest winning. You write your own user-scope secrets, a tenant's *Owner* or *Administrator* the tenant's, and an *Application Administrator* the app's. Every scope's secrets go through `SecretStore` (Vault when it is on). The Secrets editor lists the server's and the app scope's secrets only.

Questions 2 (unattended restarts), 3 (where to unseal), 4 (hints) and 6 (losing Vault) wait for a decision after seeing slice 12; see [plans/Secrets.md](plans/Secrets.md).

## Seams already built
- **`secret` fields** are a schema type. `configservice` is the only code that seals and opens them, and no secret ever reaches the client: a read returns `{set, hint}`.
- **`config.Cipher`** is the one place secrets are encrypted. It becomes one implementation of a `SecretStore` interface (below).
- **Flags and env vars win for secrets** (Configuration 2). An operator can always override a stored secret without the UI.

## Sealed start
- `serve` gains `--vault-addr` (default `$VAULT_ADDR`) and `--vault-mount` (default `foundation`). With `--vault-addr` set, it starts **sealed**:
  1. It sets up logging and opens `--addr`.
  2. Only these routes are served:
     - `GET /api/v1/bootstrap/status`: `{ state: "sealed" | "starting" | "ready" | "degraded" }`;
     - `POST /api/v1/bootstrap/unseal`: `{ token }`;
     - a static "server is sealed" page for the browser.
     
     Every other route answers 503 with `errorCode: "server_sealed"`.
  3. On unseal it unwraps the token (question 1) and logs in to Vault with AppRole. It reads the **bootstrap bundle** at `<mount>/data/server`: the database secret key, provider keys, and later the JWT signing key and VictoriaLogs credentials.
  4. Startup then continues exactly as today, and the state becomes `ready`.
- **No secrets are written to disk.** The Vault token is held in memory and renewed before its TTL runs out (it is a periodic token).
- If the token cannot be renewed, or Vault becomes unreachable, the server keeps running on its cached secrets. Its state becomes `degraded`, an error is logged, and an operator can unseal again with a new token.
- Without `--vault-addr`, `serve` behaves as in slice 2: unsealed, with the key-file cipher.

## `SecretStore`: where `secret` fields live
`configservice` stops calling `config.Cipher` directly. It calls:

```go
type SecretStore interface {
    Put(ctx context.Context, ref SecretRef, value string) (version int, err error)
    Get(ctx context.Context, ref SecretRef) (string, error)
    Delete(ctx context.Context, ref SecretRef) error
    Describe(ctx context.Context, ref SecretRef) (SecretInfo, error) // set, version, updatedAt, updatedBy; never the value
}
```

- **`sqlitestore`** is slice 2's behaviour: AES-GCM in the `Configuration` value, `{"$secret": "<ciphertext>"}`.
- **`vaultstore`** (KV v2) stores each secret at `<mount>/data/config/<scope>/<section>/<path>`. The `Configuration` value then holds only `{"$vault": {"version": n}}`. Values are cached in memory, and the cache is dropped when the value changes.
- **Moving across**: `cli secrets migrate` reads each `$secret` with the key-file cipher, writes it to Vault, and rewrites the row. It is idempotent.

## Scripts: `backend/scripts/secrets/`
Every script reads secret values from **stdin**, never from arguments, so they never land in shell history or `ps`. Each script uses `VAULT_ADDR` and the operator's own `VAULT_TOKEN`.

| Script | Does |
|---|---|
| `vault-dev.sh` | Starts a local dev Vault (a container, or the `vault` binary in `-dev` mode) and prints the `VAULT_ADDR` to export. For development only. |
| `init.sh` | Enables the KV v2 mount and writes the `foundation-server` policy (read the bootstrap bundle; read and write `config/*`). Creates the `foundation` AppRole, with a short SecretID TTL and single-use SecretIDs. Idempotent. |
| `set.sh <name>` | Writes one secret, read from stdin, e.g. `set.sh server/db-key`. |
| `list.sh [prefix]` | Lists names, versions and update times only. |
| `rotate-db-key.sh` | Generates a new database secret key and re-seals every stored secret under it (through `cli secrets rekey`). |
| `delete.sh <name>` | Deletes a secret (soft delete with its versions kept), after asking for confirmation. |
| `unseal.sh [server-url]` | Makes a response-wrapped, single-use SecretID token and posts it to the server's `POST /bootstrap/unseal`. It waits until the state is `ready`. |

## `secretsservice` and the Secrets editor
**Backend**: `features/secrets`, with `secretsservice` at `/api/v1/secrets`. It follows the CLAUDE.md layout.

| Method | Purpose |
|---|---|
| `List{Prefix}` | Names with `{set, version, updatedAt, updatedBy}`. **Never values.** |
| `Put{Name, Value}` | Writes a new version. |
| `Delete{Name}` | Soft-deletes a secret. |
| `Status` | The bootstrap state and the token's remaining TTL. |

- Every operation checks `secret.write` (or `secret.list`) on `app`. Only the **Application Administrator** role holds them.
- Every write is audited: who, what name and version, and when. Values are never audited.

**Frontend**
- **Secrets editor** (`src/editors/secrets/`), opened from the **Administration** sidebar section. It shows a table of names with their status, version, and who changed them last.
  - **Set…** opens a password field for the new value; the dialog clears itself on close.
  - **Delete** asks for confirmation.
  - Nothing ever displays a value.
- **Settings**: once `vaultstore` is active, the `secret` fields in Settings write through `SecretStore` and need the same permission. Without it they show "managed by an Application Administrator" and are read-only. No hint is shown in Vault mode.
- **Sealed page**: when `bootstrap/status` is `sealed`, the frontend shows a page explaining the state. An operator unseals with `unseal.sh`; the page takes no token.

## Built in slice 12
What was decided while building, beyond the answers to questions 1 and 7 (and 5, before slice 11).

**The sealed start** (`features/secrets`: `Bootstrap`, `Gate`)
- `serve` always listens at once, behind a `secrets.Gate`. Until startup is done, the gate serves only `GET /api/v1/bootstrap/status` and `POST /api/v1/bootstrap/unseal`; every other API route answers 503 with `server_sealed` (or `server_starting`), and any other path a small HTML page that reloads itself. Without Vault the state is `starting` and then `ready`, so a browser sees 503 `server_starting` for those seconds where it used to see a refused connection.
- **Flags**: `--vault-addr` defaults to `$FOUNDATION_VAULT_ADDR`, not `$VAULT_ADDR` as planned. The scripts use `VAULT_ADDR` for the operator's own Vault, and a shell that has it exported should not make `serve` start sealed. `--vault-mount` (`foundation`) as planned, and a new `--vault-role-id` (`$FOUNDATION_VAULT_ROLE_ID`).
- **Unseal** takes `{token, roleId}`: AppRole login needs the role ID too. `unseal.sh` reads it from Vault and sends it; `--vault-role-id` pins it, and then a different one is refused.
- Before spending a token, the server looks it up (`sys/wrapping/lookup`) and refuses one that does not wrap a SecretID of its own role (400 `bad_token`), so a wrapped token of anything else is not consumed.
- **The bootstrap bundle** is every secret under `<mount>/server/`, each its own KV secret with one `value` field (`server/db-key`, `server/gemini-api-key`), rather than one secret with a field per key. `set.sh server/<name>` writes one.
- **The database key** (`server/db-key`, made by `init.sh`) replaces `secret.key`. The JWT signing key stays a row in the application database, sealed with it, rather than moving into Vault.
- **The server's Gemini key** (`server/gemini-api-key`) sits under every stored scope (source `server`), not over them like a flag: a key set in Settings wins over it. A flag or env var still wins over everything.
- **Bundle changes need a restart.** The bundle is read once, when the server unseals, so a server secret set in the editor applies at the next start; the editor says so. Secret settings apply at once.
- **Losing Vault** (question 6): any failed Vault request but a missing secret, and a failed renewal, make the state `degraded`, with the reason. The renewer then retries every 30 seconds, and a successful renewal makes it `ready` again. When the token has expired, `unseal.sh` works on a degraded server and replaces it. Values already read are cached, and a setting whose secret could not be read is not cached, so it is read again next time.

**`SecretStore`** (`config.SecretStore`)
- `Put` returns the placeholder kept in the `Configuration` value, and `Get` takes it back, so a store decides what the row holds. There is no `Describe`: the Secrets editor reads versions from Vault itself.
- `config.CipherStore` is slice 2's behaviour (the plan's `sqlitestore`). `vaultstore` keeps each secret at `config/<scope>/<section>/<path>` with `updated_by` in its custom metadata, and still opens `{"$secret": …}` values with the database key until they are migrated.
- `configservice` writes only the secrets a save changed: an unchanged secret keeps its placeholder, so saving a section makes no new Vault version. A secret cleared or reset is deleted from the store once the row is saved.
- **`cli secrets migrate`** moves every `$secret` into Vault, re-seals the signing key with `server/db-key`, and replaces sealed values in the settings history with `{"$vault": {}}`, so `foundation.db` keeps none. It is idempotent. **`cli secrets rekey`** (behind `rotate-db-key.sh`) makes a new key, re-seals under it, and writes a new `server/db-key` version; without `VAULT_ADDR` it rotates the key file instead. Both run with `serve` stopped.

**Who writes secrets** (question 7)
- `ai.providers` takes values at the app, tenant and user scopes. Each AI call's key is the setting as the call's principal sees it (`gemini.WithKeyFor` reads `features.CallFrom`), so a user's or tenant's own key is used for their calls. Calls with no principal, such as indexing, use the default tenant's.
- In `configservice.Set` and `Reset`, plain settings need `config.write` and secrets their own check: `secret.write` on `app` at the app scope (*Application Administrator*, or *Super Administrator*), and `config.writeSecrets` elsewhere (a tenant's *Owner* or *Administrator*; everyone at their own user scope). An *Application Administrator* can therefore set the app's keys and nothing else there.
- In Settings, a section the user may not write but whose secrets they may stays editable for those fields only; a secret they may not write at the app scope says "Managed by an Application Administrator".

**`secretsservice` and the Secrets editor**
- `/secrets`: `GET status`, `POST list|put|delete`, as planned. Names are `server/<name>` (lower case, up to four levels; Vault only) and `config/app/<section>/<path>` for the app scope's secret settings, whose writes go through `configservice.Set`, so they are stored and applied like any setting. `server/db-key` is listed but protected (409 `protected_secret`).
- `GET /bootstrap/status` needs no session and returns only the state; `/secrets/status` (with `secret.list`) adds the Vault address, mount, token expiry and the reason for `degraded`.
- **Audit**: each editor write is a `SecretAudit` row in the application database (name, action, version, user) and a log record. Settings' own secret writes are logged, not audited in that table.
- The editor (Administration › Secrets, `src/editors/secrets/`, shown with `secret.list`) has two groups, server secrets and the app scope's secret settings, each row with its status, version, when and by whom it was changed, *Set…* (a password field in a dialog that unmounts on close) and *Delete*. *New server secret* makes `server/<name>`. Without Vault only the settings group shows.
- **Nothing captures a value**: `Secrets.put` is not captured in DevTools' Network tab, and neither is `Config.set` when its patch carries a secret. The latter fixes a leak from slice 2.
- **The sealed page** is `src/session/SealedGate.tsx`, around the router: it polls `/bootstrap/status` every 3 seconds while sealed or starting, and continues by itself.

**Scripts** (`backend/scripts/secrets/`) use `curl` and `jq` against Vault's HTTP API rather than the `vault` CLI, sharing `lib.sh`. Values come from stdin (typed without echo on a terminal) and the operator's token reaches `curl` through a file descriptor, so neither appears in `ps`. `vault-dev.sh` runs `hashicorp/vault:1.17.6` in Docker (`vault-dev.sh stop` removes it). `init.sh` is idempotent and prints the role ID. `list.sh` shows `(deleted)` versions.

**Seen while building**: in Settings, the "not set at the … scope" caption showed on every field of a read-only section; it now shows only where the scope does not take the field.

### Verification of slice 12
Against a dev Vault (`vault-dev.sh`), with a data directory first used without Vault:
1. `init.sh` (twice), `set.sh server/gemini-api-key`, `cli secrets migrate` (twice: the second moves nothing), then `serve --vault-addr`. It answered 503 `server_sealed`, and the HTML page, until `unseal.sh`; a made-up token was refused with 400 `bad_token` and left it sealed.
2. Nothing secret on disk: no `secret.key` (moved aside), and no `$secret` in `foundation.db` or its revisions.
3. As an *Editor*: 403 from `/secrets/list` and from setting the app's key; their own key saved at the user scope, into Vault. The Secrets command is hidden, and Settings shows "Managed by an Application Administrator". As an *Application Administrator*: the app's key set, a plain app setting refused with 403.
4. In headless Chromium: a value set in the editor appeared in neither the page nor DevTools' Network tab, where `/secrets/put` is not listed.
5. Pausing Vault made the server `degraded`, still serving settings, and it was `ready` again within 30 seconds of Vault returning. Revoking its token kept it `degraded` until `unseal.sh` gave it a new one. `rotate-db-key.sh` with `serve` stopped, then a restart and unseal, worked.

Also: unit tests against an in-memory Vault (`secrets/vault/vaulttest`) for unsealing, `vaultstore`, migration and re-sealing, and in `configservice` for unchanged secrets, unreadable secrets and the server source.


### Decisions to review after seeing it
- `--vault-addr` defaults to `$FOUNDATION_VAULT_ADDR`, and unseal takes the role ID.
- The bundle is a folder of one-value secrets, read once at unseal; server secrets apply at the next start.
- `server/gemini-api-key` sits under stored keys rather than over them.
- The signing key stays in the application database, sealed with `server/db-key`.
- An *Application Administrator* writes the app scope's secrets through Settings and the editor, but no other app setting.
- A user's own key is used for their calls, and indexing uses the default tenant's.
- The server listens before startup is done, answering 503 `server_starting`.
- A degraded server recovers by itself when its token renews.
- Settings' secret writes are logged but not in `SecretAudit`.
