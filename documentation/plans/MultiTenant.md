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

Status: draft. Not fully defined. The seams are built; see [Built in slice 1](#built-in-slice-1).

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

## Open questions
These were moved from [TODO.md](TODO.md).
Who may edit settings has moved to [RBAC.md](RBAC.md).
1. **Feedback scope**: is a segment's 👍/👎 weight shared by everyone, per tenant, or per user? The proposal is per tenant. *Decision*:
2. **JWT lifetime and revocation**: the proposal is a short-lived access token (15 minutes) plus a refresh token stored server-side, so logging out or removing a membership takes effect quickly. Tokens are signed with a key held in the app-scope secrets. *Decision*:
