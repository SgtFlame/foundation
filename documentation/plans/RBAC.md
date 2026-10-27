# RBAC: what remains

The real `authz.Check` over role grants, the built-in and custom tenant roles, `authzservice`, the Role Editor, the Share dialog and the scopes in Settings are built; see [RBAC.md](../RBAC.md).

## Groups
- A **group** principal for grants, beside users and service principals. Groups could come from OIDC group claims ([MultiTenant.md](MultiTenant.md)).
- The Role Editor's principal picker and the Share dialog list groups too.

## Evaluation
- **Listing**: list operations filter by what the principal may `read`, through `authz.Filter`. They never return everything and drop items afterwards. Today lists filter by owner or tenant in their queries.
- **The policy version**: the JWT carries a policy version. Each service drops a principal's cached grants when it changes, so a change takes effect on the next request on every server (today the cache is one server's and is dropped on any change).
- **Agents and tools**: a Cognition run acts **on behalf of** the user who started it, never with more access than that user has. A graph can narrow this further by listing the resources it may touch ([Cognition.md](Cognition.md)).
- **Audit viewer**: every grant change and every denied check on a changing action is already in `AccessAudit`; a viewer shows it, with configuration revisions, secret changes (`SecretAudit`) and tool-call approvals ([Backlog.md](Backlog.md)).

## Permissions not enforced yet
- `usage.viewAll` (other users' usage) is declared, but usage is the tenant's for every member with `usage.read` ([Usage.md](Usage.md)).
- `tool.execute` and `tool.approveWrite`: tool allow-lists and approvals ([Tools.md](Tools.md)).
- `graph.execute` and `graph.publish`, and the `graph`, `prompt`, `nexus` and `tool` resource types.

## Shares
The Share dialog on files, graphs and Nexuses (today on Volumes and tenants only).

## TODO
### Open questions
3. **Groups**: are user groups needed for grants, and could they come from OIDC group claims? *Decision*: (after seeing) not built in slice 11; grants name users only.
4. **Custom system roles**: may a Super Administrator define new app-level roles, or are app-level roles only the built-in ones? *Decision*: (after seeing) built on the proposal in slice 11: app-level roles are only the built-in ones; custom roles are per tenant.
6. **Time-limited grants**: is expiry needed in v1? *Decision*: (after seeing) built on the proposal in slice 11: a grant may have an optional expiry date, after which it gives nothing. A policy beyond the optional date is not built.

(Questions 1, 2, 5 and 7 are decided; see [RBAC.md › Decisions](../RBAC.md#decisions).)

### Review after seeing it ★
- **The built-in roles and the permission matrix** (slice 11), by a person and with more than one tenant in use. Seen in headless Chromium, light theme only, and with curl across two tenants. Not seen in the browser: dark theme, the Tenants editor as a tenant *Administrator* (not a *Tenant Administrator*), a grant expiring, and a read-only viewer handing the writer role to an editor who joins later. To review: run `serve` on a copy of your data directory (the first start migrates the `Admin` flag and re-embeds every Chronicle entry), open Administration › Roles, share a Volume with a second user as *Viewer*, and share a Chronicle entry globally with two moderators.
- **The decisions made while building**, in [RBAC.md › Decisions to review after seeing it](../RBAC.md#decisions-to-review-after-seeing-it).

### Not built yet
- Groups (question 3) and custom app-level roles (question 4).
- The audit viewer, and `PolicyVersion` checks per request.
- Shares on files, graphs and Nexuses.
- `authz.Filter`.
- Enforcing `usage.viewAll`, `tool.execute` and `tool.approveWrite`; narrowing a Cognition run to the resources a graph lists.
- Inviting by username, so a tenant's *Administrator* can add members without `user.list`.
- **Seen while verifying**: the permission matrix scrolls sideways on narrow editors.
