# RBAC: resource-based access control, roles and the Role Editor

## Context
[MultiTenant.md](MultiTenant.md) defines tenants, users and memberships, and gives every request a principal, carried as a JWT in `ServiceArgs`. This plan defines **what that principal may do**. Access is fine-grained and **resource based**:
- a permission names a resource type and an action;
- a role is a named set of permissions;
- a role is granted on a **resource**, not globally, and applies to that resource and everything under it.

Some examples:
- A user is granted *Editor* on tenant A. They can write in tenant A and cannot see tenant B at all.
- A user is granted *Tenant Administrator* on the application. They can create and delete tenants.
- A user is granted *Viewer* on one Volume in tenant A, without being a member of anything else there.

Status: draft. Not fully defined. The `authz.Check` seam is built; see [Built in slice 1](#built-in-slice-1).

## Resources
Resources form a tree, and each resource has a path:

```
app
├── tenant:<id>
│   ├── volume:<id>
│   │   └── file:<id>
│   ├── chronicle:<id>        (owned by a user; see "Ownership")
│   ├── conversation:<id>
│   ├── graph:<id>            (Cognition graphs)
│   ├── prompt:<id>
│   ├── nexus:<id>
│   ├── tool:<name>
│   └── config:<section>
└── config:<section>          (app scope)
```

- **Inheritance**: a grant on a resource applies to everything under it. *Editor* on `tenant:A` covers every Volume in A.
- **Resource types are an extension point** ([Contributors.md](Contributors.md)). Each feature declares its resource types and their actions on `authz.ResourceType`:
  ```go
  type ResourceType interface {
      Name() string       // "volume"
      Parent() string     // "tenant"
      Actions() []Action  // read, write, create, delete, share, …
  }
  ```
  The Role Editor builds its permission matrix from these contributions, so a new feature's permissions show up with no editor changes.

## Permissions and actions
A permission is `<resourceType>.<action>`, for example `volume.write` or `tenant.create`.

**Common actions** (a type declares the ones that apply to it):

| Action | Meaning |
|---|---|
| `read` | see and open it |
| `write` | change its content |
| `create` | create children of this type under the parent |
| `delete` | delete it |
| `share` | grant others access to it |
| `moderate` | approve or reject shares and flagged content |
| `execute` | run it (Cognition graphs, tools) |
| `admin` | change its settings and the grants on it |

**Type-specific actions**, for example:
- `tenant.create`, `tenant.delete` (held on `app`);
- `tenant.manageMembers`;
- `chronicle.shareGlobal`;
- `graph.publish`;
- `tool.approveWrite` (approving `write`/`external` tool calls; see [Tools.md](Tools.md));
- `usage.viewAll` (other users' usage);
- `config.writeSecrets`;
- `logs.read` (your tenant's logs), `logs.readAll` (every tenant's) and `logs.setUserLevel` (full logging for one user, held on `app`); see [Logging.md](Logging.md).

## Roles
- **A role** is a name, a description and a set of permissions. Roles are defined at a **scope**:
  - **system roles** are built in and read-only;
  - **tenant roles** are custom roles a tenant administrator defines. They can be used only within that tenant and can hold only tenant-level permissions.
- **Built-in roles** (a first proposal):

| Role | Granted on | Gives |
|---|---|---|
| *Super Administrator* | `app` | everything |
| *Tenant Administrator* | `app` | create, delete and administer any tenant; no access to tenant content unless also granted |
| *Application Administrator* | `app` | maintain the server's secrets, write-only (`secret.list`, `secret.write`); see [Secrets.md](Secrets.md) |
| *Global Moderator* | `app` (the global tenant) | moderate content shared to the global tenant |
| *Owner* | `tenant:<id>` | everything in the tenant, including deleting it |
| *Administrator* | `tenant:<id>` | tenant settings, members, roles, policies and budgets |
| *Moderator* | `tenant:<id>` | moderate shares out of the tenant and flagged feedback |
| *Editor* | `tenant:<id>` or below | read, write, create and execute |
| *Viewer* | `tenant:<id>` or below | read |

These replace the fixed `role` on `Membership` in [MultiTenant.md](MultiTenant.md): a membership says the user belongs to the tenant, and grants say what they may do there.

## Grants
- **`RoleGrant`**: `db.Model` (TSID), principal, role, resource path, granted by, granted at, and an optional expiry.
- **Principals**:
  - a **user**;
  - a **group** (later; see the open questions);
  - a **service principal** for runs with no user present, such as scheduled Cognition graphs.
- **Ownership**: the creator of a user-owned resource (a Chronicle entry, a conversation, a personal draft graph) gets an implied *Owner* on it. No grant row is needed.

## Evaluation
- **One check function**: `authz.Check(ctx, args.Principal, action, resource)` returns allow or deny, plus a reason.
  - It allows when any grant on the resource or on an ancestor holds a role containing the permission, or when the principal owns the resource.
  - v1 is **allow-only**: there are no explicit deny rules (see the open questions).
- **Where it runs**: every service operation checks at its start, using the principal from `ServiceArgs`. Handlers never check, so the rule lives with the logic, and calls between services are checked too.
- **Listing**: list operations filter by what the principal may `read`, through `authz.Filter`. They never return everything and drop items afterwards.
- **What the JWT carries**: the user, the active tenant and a **policy version**. Grants can be fine-grained, so they are not put in the token. Each service resolves them through `authzservice`, which caches each principal's effective grants and drops the cache entry when the policy version changes.
- **Agents and tools**: a Cognition run acts **on behalf of** the user who started it, never with more access than that user has. A graph can narrow this further by listing the resources it may touch.
- **Single-user mode**: the implicit user holds *Super Administrator*.
- **Audit**: every grant change, and every denied check on a write, goes to an `AccessAudit` table, shown in the audit viewer.

## Services: `features/authz`
- **`authzservice`**:
  - resource types: `ListResourceTypes`, for the editor;
  - roles: `ListRoles`, `GetRole`, `SaveRole` (409 on a stale revision), `DeleteRole`. Built-in roles cannot be changed or deleted, but can be duplicated.
  - grants: `ListGrants` (by principal or by resource), `Grant`, `Revoke`;
  - checks: `Check`, `Explain` (why a principal may or may not do something), `EffectivePermissions`.
- **REST and client**: under `/api/v1/authz`, with `src/services/Authz.ts`.
- **Frontend checks**: the client caches the principal's effective permissions, and `useCan(action, resource)` hides or disables UI. This is a convenience only; the backend check is authoritative.

## The Role Editor
An editor opened from an **Administration** sidebar section, with these tabs:
- **Roles**: the roles visible in the current scope, built-in roles marked as such. Selecting a role opens it.
- **Role**: its name and description, and a **permission matrix**:
  - rows are resource types, grouped by feature;
  - columns are actions, and a checkbox appears only where the type declares the action;
  - row and column toggles, and a filter.
- **Grants**: who holds this role, and on which resource. You can add a grant with a principal picker and a resource picker (a tree), set an expiry, and revoke grants.
- **Access checker**: pick a principal, an action and a resource to see allow or deny, with the reason (for example "*Editor* granted on `tenant:A` by Pat on 2026-11-02").

**Reusable pieces**: a **Share dialog** (on Volumes, files, graphs and Nexuses), which lists and edits the grants on one resource, and the **resource picker**.

## Relation to other plans
- [MultiTenant.md](MultiTenant.md): tenants, users, the JWT and the global tenant. Memberships no longer carry roles.
- [Configuration.md](Configuration.md): writing a scope requires `config.write` on that scope's resource, and secret fields require `config.writeSecrets`.
- [Tools.md](Tools.md): tool allow-lists and approvals are `tool.execute` and `tool.approveWrite`.
- [Cognition.md](Cognition.md): `graph.execute` and `graph.publish`; runs act on behalf of a user.
- [Chronicle.md](Chronicle.md): ownership, `chronicle.share` and `chronicle.shareGlobal`, and moderation.
- [Usage.md](Usage.md): `usage.viewAll` and budget administration.

## Built in slice 1
- **`features/authz`**: `authz.Check(ctx, principal, action, resource) Decision` allows everything, with the reason "access control is not enabled". `Decision.Err(action, resource)` is nil when allowed, and otherwise an `access_denied` error to fail with `authz.Status` (403):
  ```go
  if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
      return Response{Common: *args.Fail(authz.Status, err)}
  }
  ```
- Resource paths are strings built with `authz.Resource("volume", id)` and `authz.Tenant(principal)`, for example `tenant:1/volume:42`.
- `extensionsservice.ListPoints` is the first operation to check (`extensions.read` on `app`). Operations written before slice 1 do not check yet; each gains its check when its resource type is declared on `authz.ResourceType`.

## Open questions
1. **Who may edit settings** (moved from [MultiTenant.md](MultiTenant.md)): the proposal is `config.write` for *Administrator* on tenant scopes and *Super Administrator* on the app scope, while everyone may write their own user scope. *Decision*:
2. **Deny rules**: allow-only, or explicit deny grants (for example "Editor on the tenant, but not this Volume")? Deny is more expressive but harder to explain. *Decision*:
3. **Groups**: are user groups needed for grants, and could they come from OIDC group claims? *Decision*:
4. **Custom system roles**: may a Super Administrator define new app-level roles, or are app-level roles only the built-in ones? *Decision*:
5. **Tenant Administrator and content**: the proposal is that *Tenant Administrator* can manage tenants but cannot read their content. Is that right, or should it see everything? *Decision*:
6. **Time-limited grants**: is expiry needed in v1? *Decision*:
