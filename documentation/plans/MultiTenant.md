# Multi-tenancy, identity and scoping

## Context
Several plans assume tenants and users that do not exist yet:
- configuration scopes ([Configuration.md](Configuration.md));
- Chronicle ownership ([Chronicle.md](Chronicle.md));
- feedback weights ([ContextRetrieval.md](ContextRetrieval.md));
- graph and Nexus sharing ([Cognition.md](Cognition.md));
- tool policies ([Tools.md](Tools.md));
- budgets ([Usage.md](Usage.md)).

The code has hooks already:
- `features.ServiceArgs.Tenant`;
- `kbservice.VolumePath`'s optional `[tenant/][user/]` path segments;
- a `/login` page with no backend behind it.

This document collects the decisions in one place.

Status: partly built. The seams were built in slice 1 ([Built in slice 1](#built-in-slice-1)). Local accounts, sessions, tenants, the application database, per-tenant LanceDB directories and the Account, Tenant and User editors were built in slice 10 ([Built in slice 10](#built-in-slice-10)). The global tenant and Chronicle sharing were built in slice 11 ([Built in slice 11](#built-in-slice-11)). Still to come: OIDC and per-tenant Volumes.

## Model
- **`Tenant`**: an organisation or workspace (`db.Model` TSID, name, plan, settings scope).
  - The **global tenant** is a built-in tenant for content shared across all tenants, such as Chronicle entries promoted for everyone (see [Sharing Chronicles](#sharing-chronicles)).
- **`User`**: an identity (TSID, display name, email, auth subject).
- **`Membership`**: links a user to a tenant. What the user may do there comes from **role grants**, defined in [RBAC.md](RBAC.md): for example *Owner*, *Administrator*, *Moderator*, *Editor* and *Viewer* on the tenant. A *Moderator* of the global tenant is a **global moderator**.
- **Several tenants per user**: a user can belong to several tenants and switch between them in the UI. The active tenant is part of the session token, and switching tenants issues a new token.
- **Request context**: every request carries `Tenant` and `User` in `ServiceArgs`, filled by auth middleware from the token. Services never trust IDs from request bodies.

## What gets scoped

| Resource | Scope | Notes |
|---|---|---|
| Configuration | app → tenant → user layers | Who writes each scope is an [RBAC.md](RBAC.md) question. |
| Volumes (KB) | tenant, optionally user | `VolumePath` already supports `root/tenant/user/...`. |
| Index (LanceDB) | tenant | One LanceDB directory per tenant, `<data-dir>/lancedb/<tenantId>/`. |
| Chronicles | user, shareable to the tenant or the global tenant | See [Sharing Chronicles](#sharing-chronicles). |
| Conversations | user | |
| Feedback (`SegmentFeedback`) | tenant (proposal) | See the open questions. |
| Cognition graphs, prompts | tenant, with personal drafts | Built-ins are global and read-only. |
| Nexuses | tenant (project) | |
| Usage, budgets | tenant and user | Dashboard totals per tenant; per-user breakdown for admins |
| Secrets | app or tenant | A tenant may bring its own provider keys. |

### Sharing Chronicles
Chronicle entries belong to the user who made them. The owner can share an entry:
- **with their tenant**: other members of the same tenant can use it right away;
- **with the global tenant**: the share needs two approvals before other tenants can use it:
  1. a moderator of the owner's tenant;
  2. a global moderator.

A `ChronicleShare` record tracks each request: the entry, the target, a status (`pending` | `tenantApproved` | `approved` | `rejected`), the approvers and timestamps. Retrieval sees a globally shared entry only once its status is `approved`.

## Storage
- **Databases**: two, both through `features/db`.
  - The **application database** holds what spans tenants: tenants, users, memberships, app-scope configuration and secrets.
  - The **tenant database** is one database shared by all tenants. Every tenant-owned table has a `TenantID` column, and services always filter on it.
  - Services reach the tenant database through a resolver (`db.ForTenant(tenantID)`), never a global handle. A tenant can later move to its own database by changing only the resolver.
- **Index**: one LanceDB directory per tenant, so tenants are fully isolated and removing a tenant drops its directory. The global tenant has its own directory too.

## Identity
- **Login methods**: local accounts (username and password, hashed with argon2id) and OIDC (Google, Microsoft, …).
  - OIDC is configured at the application level. When it is configured, `/login` redirects to the OIDC provider; otherwise `/login` shows the local account form.
- **Tokens**: the session is a **JWT**, carried in an HTTP-only cookie.
  - Its claims hold the user, the active tenant and a policy version. Grants are not in the token; see [RBAC.md](RBAC.md#evaluation).
  - Auth middleware verifies it and puts the claims in `ServiceArgs`. The token itself is also kept in `ServiceArgs`, so it is forwarded with service-to-service calls and each service makes its own access checks.
  - The WebSocket (collab) and SSE endpoints authenticate the same way.
- **Single-user mode**: with no auth configured, the server runs as one implicit tenant and user, which is today's behaviour. Development stays simple.

## Built in slice 1
- **`features.Principal`**: `{Kind, UserID, TenantID}`, where `Kind` is `user` or `service`. `ServiceArgs.Principal` replaces `ServiceArgs.Tenant`, and request DTOs no longer have a `tenant` field: the principal comes only from the request middleware. `VolumePath`'s optional tenant and user segments are untouched.
- **Single-user mode**: `rest.Middleware` gives every request `features.LocalPrincipal()`. Its IDs are reserved, `LocalTenantID = 1` and `LocalUserID = 1`, so the default tenant and the first user keep them when identity arrives, and rows written until then already belong to them.
- **Background work** (the index worker and the startup reindex) runs as `features.ServicePrincipal(LocalTenantID)`, through `features.BackgroundArgs(ctx)`.
- **The table convention** (`features/db/Tenancy.go`): a tenant-owned table embeds `db.Tenanted` (`TenantID`, indexed) after `db.Model`, and a user-owned one `db.Owned` (`OwnerID`) too. Services fill them from the principal (`db.TenantOf(p)`, `db.OwnedBy(p)`) and filter with the scope `db.InTenant(id)`. The scope is named so that `db.ForTenant`, the resolver of the tenant database, stays free. No table uses them yet.

## Built in slice 10
What was decided while building, beyond the answers to questions 2 to 5.

**Backend: the `identity` feature** (`backend/features/identity`)
- **Tables** in the application database (`identity.Models()`): `Tenant`, `User` (lower-case `Username`, argon2id `PasswordHash` in PHC form, `Admin`, `Disabled`, `LastTenantID`, `PolicyVersion`), `Membership` (unique per tenant and user), `RefreshToken` (stored as SHA-256 only, with a `Family`, `RevokedAt` and `ReplacedAt`) and `SigningKey` (one row, sealed by `config.Cipher`).
- **Two databases.** `serve --app-db` (default `<data-dir>/app.db`) is the application database. The existing `foundation.db` (`--db`) is the tenant database, shared by every tenant and filtered by `TenantID` as before. The `db.ForTenant` resolver was not built: services still get one handle. App-scope configuration and secrets stay in the tenant database until Vault (slice 12).
- **Sessions** (`identity.Sessions`, `identity.Signer`): a 15-minute HS256 access JWT (`sub`, `tid`, `adm`, `pv`) in the `foundation_access` cookie (path `/api`), and a 30-day refresh token in `foundation_refresh` (path `/api/v1/identity`). Both are HTTP-only and `SameSite=Strict`, and `Secure` over HTTPS. Each refresh rotates the token.
  - A replaced token used again within 30 seconds (two browser tabs refreshing at once) gets an access token only.
  - Used again later, it is taken as stolen, and its whole family is revoked.
  - Refreshing also fails when the user is disabled or no longer a member of the token's tenant.
  - A user with no tenant cannot sign in (`403 no_tenant`).
- **Authentication** (`identity.Authenticator`, passed to `rest.Middleware` with `rest.WithAuthenticate`): while no user exists, every request is `features.LocalPrincipal()`, as before. Afterwards a request without a valid token is `features.AnonymousPrincipal()`. `rest.RequireUser()` refuses it with 401: `session_expired` when the token has expired, else `unauthenticated`. Only `/api/v1/identity/*` is outside `RequireUser`.
- **`Principal.Admin`** carries the user's flag, and `LocalPrincipal` is an administrator.
- **Services**:
  - `identityservice` (`/identity`): `GET status`; `POST setup|login|refresh|logout|switch-tenant|account`.
  - `tenantservice` (`/identity/tenants`): `POST list|create|rename|delete|members|add-member|remove-member`.
  - `userservice` (`/identity/users`): `POST list|create|update`.
  - Handlers set and clear the cookies; responses carry only `expiresAt` and the `account`.
- **Setup** makes tenant 1 and user 1 (the reserved IDs) in one transaction, and works only while no user exists (`409 setup_done`).
- **Sign-in failures**: a wrong username and a wrong password fail alike (`401 bad_credentials`), and an unknown username still spends a password check.
- **Changing what a user may do** ends their sessions (`Sessions.RevokeUser`) and raises their `PolicyVersion`. That covers removing a membership, deleting a tenant, changing `Admin` or `Disabled`, and a new password. Their access token lives on for at most 15 minutes.
- **Users are disabled, never deleted.** An administrator cannot take away their own flag or disable themselves, and one enabled administrator always remains (`409 last_admin`).
- **Deleting a tenant** removes its memberships and sessions. Tenant 1 cannot be deleted.

**Per-tenant LanceDB directories** (seam 7)
- `db.TenantDir(root, tenant)` is `<data-dir>/lancedb/<tenantId>`. At start, `db.MoveToTenantDir` moves the single-tenant layout's tables into tenant 1's directory, once.
- Chronicle entries use `entrystore.Tenants`, one entries table per tenant, opened when first used.
- **Volumes stay the default tenant's.** The knowledge base root is not per tenant yet, so the Volumes, their index (one `lancestore` in `lancedb/1`) and collaboration belong to tenant 1:
  - `kbhandlers.InTenant()` refuses the `/kb` and `/index` routes elsewhere (`403 volumes_unavailable`);
  - the `library` source finds nothing there, and the kb tools fail;
  - the Volumes command is hidden.

**Frontend**
- `src/session/session.ts` keeps the session. It refreshes a minute before expiry and when a tab wakes, and the http client's `setUnauthorizedHandler` refreshes once on a 401 and retries.
- `RequireSession` guards `/workbench`, sending the browser to `/login?next=…`, and starts log forwarding once there is a session.
- `/login` signs in, and `/setup` (or `/login` while there are no accounts) sets up. Both load the next page afresh.
- The **account menu** at the foot of the side bar shows who is signed in. It holds the tenant switcher (which reloads the page), *Account…* and *Sign out*. In single-user mode it offers *Set up accounts…*.
- **Editors**: Account (profile, password, tenants), and Administration › Tenants and Users, shown only to administrators (`when` sees `{ admin, tenantId }`). The Users editor also turns on full logging for one user.

**Verified**: Go tests for passwords, tokens, rotation and reuse, ending a session, leaving a tenant, `TenantFor`, the interim `authz` rules, the LanceDB move and per-user log levels; a curl walkthrough of every route; and headless Chromium for setup, Tenants, Users, a second user signing in (wrong password first) and seeing no admin commands or Volumes, the Account page, and signing out.

### Decisions to review after seeing it
- The access token lasts 15 minutes and the refresh token 30 days, with a 30-second grace for concurrent refreshes.
- Auth is opt-in: single-user mode lasts until `/setup`.
- The tenant database is today's `foundation.db`, and app-scope settings stay there.
- Volumes, their index and collaboration are the default tenant's only.
- Switching tenants reloads the page.
- The Account page is a workbench editor, not a route.
- Users are disabled, never deleted. A deleted tenant's content is kept, unreachable.
- Passwords need at least 8 characters, and usernames are 3 to 64 of `a-z 0-9 . _ -`.
- `logs.readAll` and app-scope `config.write` are admin-only too (see [RBAC.md › Built in slice 10](RBAC.md#built-in-slice-10)).

### Not yet
- OIDC (question 4).
- Per-tenant knowledge base roots, so other tenants get Volumes.
- The `db.ForTenant` resolver, and moving app-scope configuration into the application database.
- Purging a deleted tenant's rows and LanceDB directory.
- The global tenant and Chronicle sharing. *Built in slice 11.*
- Rate limiting sign-in.
- Invitations and password reset by email.
- Refreshing from the `fetch`-based streaming clients on a 401 (the timer covers them).
- `PolicyVersion` checks per request.

## Built in slice 11
Roles replaced the administrator flag (see [RBAC.md › Built in slice 11](RBAC.md#built-in-slice-11)). What slice 11 decided about the global tenant and sharing Chronicles:

**The global tenant**
- `features.GlobalTenantID` is the reserved ID 2. It has no `Tenant` row and no members: nobody signs in to it. It holds the copies of approved global shares, in the tenant database (`TenantID = 2`) and in its own LanceDB directory (`lancedb/2`).

**Sharing an entry** (`chronicleservice.Share`, `POST /chronicle/share {id, target, note}`)
- **With the tenant** (`target: "tenant"`) takes effect at once: `Entry.Shared = "tenant"`.
  - The entries table has a `shared` column, so the tenant's other members find the entry by search and stage 1 can choose it.
  - The table's schema changed, so its old rows are dropped and every entry is embedded again on the first start.
- **Globally** (`target: "global"`) makes a `ChronicleShare` request in the tenant database. It is a snapshot of the entry's type, title and summaries (not its full context), the owner's name and an optional note.
  - A moderator of the owner's tenant (`chronicle.moderate`) moves it from `pending` to `tenantApproved`.
  - A global moderator (`chronicle.moderateGlobal`, on `app`) then approves it, which creates the global copy: an `Entry` in tenant 2 with `Shared = "global"` and `SourceID`, owned by the original owner.
  - Either stage may reject it, with a reason.
  - Nobody moderates their own request, and the global approval must come from someone other than the tenant's approver. A tenant that wants global sharing therefore needs two moderators.
- **Stopping** (`target: ""`) unshares from the tenant, withdraws an open request, and deletes the global copy. Deleting the entry does the same.
- **Reading**: `chronicle.Visible` is the principal's entries, those shared with their tenant, and the global tenant's except copies of their own.
  - `List` appends the active shared entries after the principal's own; `Get` reads any visible entry, but returns links and rounds only for one's own.
  - Stage 1's candidates come from `Visible`. The memory update and every write still load only the principal's own entries (`Owned`), so nobody changes another's entry.
  - The `chronicle` retrieval source searches the principal's tenant table (theirs and the tenant's shared rows), then the global table without their own copies, and interleaves the two rankings.
- `POST /chronicle/shares {queue}` lists the principal's own requests, or the queue they may moderate: their tenant's `pending` requests, and every tenant's `tenantApproved` ones for a global moderator. It never lists their own. `POST /chronicle/moderate {shareId, approve, note}` decides.

**Frontend**
- The entry editor has *Share…* for one's own active entries (`ShareChronicleDialog`): a switch for the tenant, and the global request with its status.
- Someone else's entry opens read-only, marked *Shared with you* or *Shared with every tenant*.
- The Chronicles view lists shared entries apart, under *Shared with you*. They can be checked for a conversation.
- **Administration › Moderation** (`src/editors/moderation/`, shown with either moderate permission) is the queue: each request with its owner, note and summaries, and *Approve* or *Reject* with a reason.

**Verified**: a Go test of the whole flow (tenant sharing, read-only for colleagues, both approvals in order, no self-moderation, one moderator not approving twice, another tenant seeing only the copy, stopping), a curl walkthrough across two tenants (including the copy found by retrieval in tenant 2), and headless Chromium for the share dialog and the queue.

### Decisions to review after seeing it
- The global tenant is a reserved ID with no row and no members.
- The global copy is a snapshot of title and summaries, without the full context. Changing the entry later does not change the copy: share again to update it.
- The two approvals must come from two different people, so a tenant needs two moderators to share globally.
- Sharing with the tenant needs no approval.
- Stopping the tenant share also stops the global one; the dialog shares with the tenant again when only the global share is stopped.
- Shared entries are listed apart, not in your tree, and their parents are not shared with them.

## Open questions
These were moved from [TODO.md](TODO.md).
Who may edit settings has moved to [RBAC.md](RBAC.md).
1. **Feedback scope**: is a segment's 👍/👎 weight shared by everyone, per tenant, or per user? The proposal is per tenant. *Decision*: (after seeing) built per tenant in slice 5: `SegmentFeedback` embeds `db.Tenanted`, and retrieval reads only the principal's tenant ([ContextRetrieval.md › Built in slice 5](ContextRetrieval.md#built-in-slice-5)).
2. **JWT lifetime and revocation**: the proposal is a short-lived access token (15 minutes) plus a refresh token stored server-side, so logging out or removing a membership takes effect quickly. Tokens are signed with a key held in the app-scope secrets. *Decision*: as proposed. A 15-minute access JWT in an HTTP-only cookie, plus a refresh token stored server-side (hashed, rotated on each use). Logging out, removing a membership or disabling a user revokes the refresh tokens. The signing key is an app-scope secret, sealed by `config.Cipher` until Vault (slice 12).
3. **Bootstrap and single-user mode** (asked before slice 10): *Decision*: auth is opt-in. While the database has no users, the server stays in single-user mode, as today. A one-time `/setup` page creates the first user as user 1 in tenant 1 (the reserved IDs, so existing rows stay theirs). After that, login is required.
4. **OIDC** (asked before slice 10): *Decision*: deferred to a follow-up after the local flow has been seen. Slice 10 builds local accounts only.
5. **Administration before RBAC** (asked before slice 10): *Decision*: users get an app-level `Admin` flag, and the first user has it. Until slice 11 replaces it with role grants (the flag becomes *Super Administrator* on `app`; see [RBAC.md](RBAC.md#open-questions) question 7), `authz.Check` denies `tenant.*`, `user.*` and `logs.setUserLevel` to non-admins.
