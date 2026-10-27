# Multi-tenancy and identity: what remains

Local accounts, sessions, tenants, the application database, per-tenant LanceDB directories, the Account, Tenant and User editors, the global tenant and Chronicle sharing are built; see [MultiTenant.md](../MultiTenant.md).

## OIDC
- Login methods are local accounts and OIDC (Google, Microsoft, …), configured at the application level. When it is configured, `/login` redirects to the OIDC provider; otherwise `/login` shows the local account form.
- The Account page lists linked OIDC identities.
- OIDC group claims could feed RBAC groups ([RBAC.md](RBAC.md) question 3).

## Storage
- **The tenant database resolver**: services reach the tenant database through `db.ForTenant(tenantID)`, never a global handle, so a tenant can later move to its own database by changing only the resolver. Today services get one handle to `foundation.db`.
- **App-scope configuration** moves into the application database with it ([Configuration.md](Configuration.md)).
- **Per-tenant knowledge base roots**, so every tenant gets Volumes, their index and collaboration (today they are the default tenant's only: `kbhandlers.InTenant()`).
- **Purging a deleted tenant**: its rows, its LanceDB directory and its grants' audit. Today a deleted tenant's content is kept, unreachable.

## Accounts and sessions
- Rate limiting sign-in.
- Invitations and password reset by email; inviting by username, so a tenant's *Administrator* can add members without `user.list` ([RBAC.md](RBAC.md)).
- Refreshing from the `fetch`-based streaming clients on a 401 (the refresh timer covers them today).
- `PolicyVersion` checks per request.
- Tenant settings in the Tenants editor.

## TODO
### Open questions
1. **Feedback scope**: is a segment's 👍/👎 weight shared by everyone, per tenant, or per user? The proposal is per tenant. *Decision*: (after seeing) built per tenant in slice 5: `SegmentFeedback` embeds `db.Tenanted`, and retrieval reads only the principal's tenant.

(Questions 2 to 5 are decided; see [MultiTenant.md › Decisions](../MultiTenant.md#decisions).)

### Review after seeing it ★
- **Sign-in, setup, the account menu and the admin editors** (slice 10), by a person. Seen in headless Chromium, light theme only. Not seen in the browser: switching tenants (tried through the API only), refreshing after the 15 minutes, a disabled user's session ending, and records actually stored at debug. To review: run `serve` on a copy of your data directory (`--data-dir`), choose *Set up accounts…* in the account menu, make a second tenant and user, and check that what you made before setup is still yours.
- **The decisions of slice 10**, in [MultiTenant.md › Built in slice 10](../MultiTenant.md#decisions-to-review-after-seeing-it), and **of slice 11** (the global tenant, Chronicle sharing), in [MultiTenant.md › Built in slice 11](../MultiTenant.md#decisions-to-review-after-seeing-it-1).

### Not built yet
- OIDC.
- The `db.ForTenant` resolver, and app-scope settings in the application database.
- Per-tenant knowledge base roots.
- Purging a deleted tenant's data.
- Rate limiting sign-in; invitations and password reset by email; refreshing from the streaming clients on a 401; `PolicyVersion` checks.
- **Sharing**: flagged segments in the moderation queue; updating a global copy when its entry changes; sharing an entry's children ([Chronicle.md](Chronicle.md)).
- **Seen while verifying**: the tenant rename dialog's button says *Create* (it reuses `NameDialog`).
