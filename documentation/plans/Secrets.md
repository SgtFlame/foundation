# Secrets: bootstrapping from HashiCorp Vault

## Context
Slice 2 stores secret settings (provider API keys today; OIDC client secrets, the JWT signing key and log-store credentials later) in the `Configuration` table, encrypted with AES-GCM. The key comes from `$FOUNDATION_SECRET_KEY` or `<data-dir>/secret.key` ([Configuration.md](Configuration.md#built-in-slice-2)). That is fine on a developer machine, but on a server the key sits beside the data it protects. Anyone who can read the data directory can read every secret.

This plan moves secrets into **HashiCorp Vault**, so that nothing secret is stored on the server host:
- The backend **starts sealed**. It opens its configured port, serves only the bootstrap endpoints, and waits for a token.
- With the token it logs in to Vault and reads the rest of its secrets. Only then does it finish starting: it migrates the database, creates the services and serves the API.
- **Bash scripts** set up Vault and maintain the secrets.
- A **Secrets editor** and a `secretsservice` maintain secrets from the UI. They are **write-only** (a secret's value is never returned) and only an **Application Administrator** can use them.

Status: draft. This is the last slice ([Index.md](Index.md), slice 12). It depends on Identity (slice 10) for users and on RBAC (slice 11) for the Application Administrator role.

Decision (Configuration 1): slice 2 stores secrets encrypted in SQLite for now. This plan replaces that store later.

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
  3. On unseal it unwraps the token (see question 1) and logs in to Vault with AppRole. It reads the **bootstrap bundle** at `<mount>/data/server`: the database secret key, provider keys, and later the JWT signing key and VictoriaLogs credentials.
  4. Startup then continues exactly as today, and the state becomes `ready`.
- **No secrets are written to disk.** The Vault token is held in memory and renewed before its TTL runs out (it is a periodic token).
- If the token cannot be renewed, or Vault becomes unreachable, the server keeps running on its cached secrets. Its state becomes `degraded`, an error is logged, and an Application Administrator can unseal again with a new token. See question 6.
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
- **Settings**: once `vaultstore` is active, the `secret` fields in Settings write through `SecretStore` and need the same permission. Without it they show "managed by an Application Administrator" and are read-only. No hint is shown in Vault mode (question 4).
- **Sealed page**: when `bootstrap/status` is `sealed`, the frontend shows a page explaining the state. An operator unseals with `unseal.sh`; question 3 asks whether the page also takes a token.

## Verification (when built)
1. `vault-dev.sh`, `init.sh` and `set.sh server/gemini-api-key`, then `serve --vault-addr …`. The server answers 503 `server_sealed` until `unseal.sh` runs; after that, chat works with the key from Vault.
2. Nothing secret is on disk: there is no `secret.key`, and no `$secret` values in `foundation.db`.
3. A user without Application Administrator gets 403 from every `/secrets` operation, and the Administration › Secrets entry is hidden.
4. `List` and the editor never return or show a value, including in DevTools' Network tab.
5. Stopping Vault: the server stays up and becomes `degraded`, and unsealing again restores `ready`.

## Open questions
1. **Token form**: a response-wrapped, single-use AppRole SecretID (the proposal), or a plain Vault token? *Decision*:
2. **Unattended restarts**: should `--vault-token-file` (for example a Vault Agent sink) be allowed, so a production restart needs no operator? The proposal is yes, opt-in, with the default still waiting for a token. *Decision*:
3. **Where to unseal**: only `unseal.sh`, or also a token field on the browser's sealed page? The proposal is the script only, so that no token passes through a browser. *Decision*:
4. **Hints**: show nothing about a stored secret but whether it is set and its version (the proposal), or keep slice 2's last-four-characters hint? *Decision*:
5. **Application Administrator**: is it a new built-in app-level role holding only `secret.*` (the proposal; *Super Administrator* includes it), or simply *Super Administrator*? See [RBAC.md](RBAC.md). *Decision*:
6. **Losing Vault**: keep running on cached secrets as `degraded` (the proposal), or seal again? *Decision*:
