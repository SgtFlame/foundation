# RBAC: resource-based access control, roles and the Role Editor

## Context
[MultiTenant.md](MultiTenant.md) defines tenants, users and memberships, and gives every request a principal, carried as a JWT in `ServiceArgs`. This document defines **what that principal may do**. Access is fine-grained and **resource based**:
- a permission names a resource type and an action;
- a role is a named set of permissions;
- a role is granted on a **resource**, not globally, and applies to that resource and everything under it.

Some examples:
- A user is granted *Editor* on tenant A. They can write in tenant A and cannot see tenant B at all.
- A user is granted *Tenant Administrator* on the application. They can create and delete tenants.
- A user is granted *Viewer* on one Volume in tenant A, without being a member of anything else there.

Built: the `authz.Check` seam (slice 1), interim admin-only rules (slice 10), and in slice 11 the real check over role grants, the built-in and custom tenant roles, `authzservice`, the Role Editor, the Share dialog and tenant and user scopes in Settings; slice 12 checks `secret.*`. Groups, `authz.Filter`, the audit viewer, shares on files and graphs, and the rest are in [plans/RBAC.md](plans/RBAC.md).

## Decisions
1. **Who may edit settings** (asked before slice 11): the app scope needs `config.write` on `app` (*Super Administrator*); a tenant scope needs `config.write` on `tenant:<id>` (*Owner* and *Administrator*); every user writes their own user scope. Secret fields also need `config.writeSecrets` on the scope's resource (and, at the app scope since slice 12, `secret.write` on `app` instead).
2. **Deny rules** (asked before slice 11): allow-only. Narrow access by granting on a smaller resource instead.
5. **Tenant Administrator and content** (asked before slice 11): no content. *Tenant Administrator* manages tenants and their memberships; reading a tenant's content needs a grant in that tenant. *Super Administrator* still sees everything.
7. **The `Admin` flag** (asked before slice 11): a one-time migration grants *Super Administrator* on `app` to every user with the flag, and the flag goes. *Application Administrator* is a separate, secrets-only built-in role, which *Super Administrator* includes (Secrets 5). New members get *Editor* on their tenant by default, and a tenant's creator gets *Owner*.

Questions 3 (groups), 4 (custom system roles) and 6 (time-limited grants) wait for a decision after seeing slice 11; see [plans/RBAC.md](plans/RBAC.md).

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
- The tree shows the resource types the design names; `graph`, `prompt`, `nexus` and `tool` are not declared yet.
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
- `logs.read` (your tenant's logs), `logs.readAll` (every tenant's), `logs.ingest` (forwarding your browser's records; every user needs it) and `logs.setUserLevel` (full logging for one user, held on `app`); see [Logging.md](Logging.md).

## Built in slice 1
- **`features/authz`**: `authz.Check(ctx, principal, action, resource) Decision` allows everything, with the reason "access control is not enabled". `Decision.Err(action, resource)` is nil when allowed, and otherwise an `access_denied` error to fail with `authz.Status` (403):
  ```go
  if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
      return Response{Common: *args.Fail(authz.Status, err)}
  }
  ```
- Resource paths are strings built with `authz.Resource("volume", id)` and `authz.Tenant(principal)`, for example `tenant:1/volume:42`.
- `extensionsservice.ListPoints` is the first operation to check (`extensions.read` on `app`). Operations written before slice 1 do not check yet; each gains its check when its resource type is declared on `authz.ResourceType`.

## Built in slice 10
Identity made multi-user use possible before RBAC, so `authz.Check` gained interim rules (MultiTenant.md question 5). Slice 11 replaced them with real grants (see [Built in slice 11](#built-in-slice-11)).
- An **anonymous** principal (no session once accounts exist) may only do `session.*`: status, setup, sign-in, refresh and sign-out.
- A **service** principal (background work) may do anything.
- **Admin-only** (`authz.AdminOnly`), for a principal whose `Admin` flag is set (the user's app-level flag; `LocalPrincipal` has it):
  - `tenant.*` and `user.*`;
  - `logs.setUserLevel` and `logs.readAll`;
  - `config.write` at the app scope.

  Everything else stays allowed. The last three were added beyond the question's answer, because without them every member could read every tenant's logs and change the app's settings, including its secrets. They are decisions to review after seeing it, and the app-scope `config.write` rule anticipates question 1.
- The user's own actions are `account.update` and `account.switchTenant`.

## Built in slice 11
What was decided while building, beyond the answers to questions 1, 2, 5 and 7 (and Secrets 5).

**The check** (`features/authz`)
- `authz.Check` keeps its signature. `serve.go` installs a `Policy` (`authz.Install`) once the registry is complete, and `Check` evaluates against it. Before a policy is installed (tests of other features), every principal but an anonymous one is allowed.
- **Evaluation**, in order:
  - an anonymous principal may only use `session.*`;
  - a service principal may do anything;
  - an action of the `self` class needs no grant;
  - an action on a user-owned type (`ResourceType.Owned`) is allowed when the resource path names the principal (`…/user:<id>/…`): the implied *Owner*;
  - otherwise a grant must cover the path (the grant's path is the resource or an ancestor; `app` covers everything) with a role holding the action, and must not have expired.
  - An undeclared action is denied, with one warning logged.
- **Resource types are a point**: `authz.ResourceTypes` (`authz.resourceType`). Each feature declares its types in a `ContributeAccess(reg)` (`identity`, `config`, `logs`, `usage`, `conversation`, `chronicle`, `retrieval`, `cognition`, `kbservice`; `authz` declares `role`, `grant`, `secret` and `extensions`).
  - An `Action` has a `Class` (`read`, `write`, `create`, `delete`, `share`, `moderate`, `execute`, `admin`, or `self`) and `On` (`app` or `tenant`, the default): where its grant must be held.
- **Built-in roles are defined by classes**, so a new feature's actions join them with no role changes. They have reserved IDs 1 to 9, which grants name like custom roles:

  | Role | On | Holds |
  |---|---|---|
  | *Super Administrator* | `app` | `*`: every permission |
  | *Tenant Administrator* | `app` | every `tenant.*` and `user.*` action |
  | *Application Administrator* | `app` | `secret.list`, `secret.write` |
  | *Global Moderator* | `app` | app-held `moderate` actions (`chronicle.moderateGlobal`) |
  | *Owner* | tenant | every tenant-held action |
  | *Administrator* | tenant | every tenant-held action but the `delete` class |
  | *Moderator* | tenant | `read`, `moderate` |
  | *Editor* | tenant | `read`, `write`, `create`, `execute` |
  | *Viewer* | tenant | `read` |

  *Administrator* holds `write` and the rest because of the no-escalation rule below: without them it could not grant *Editor*.
- **Grants** (`RoleGrant`: user, role, resource path, granted by, optional expiry), **custom roles** (`CustomRole`, per tenant) and **`AccessAudit`** live in the application database. Every grant, revoke and denied check of a changing action (any class but `read` and `self`) is audited. There is no audit viewer yet.
- **No escalation**: granting or revoking a role on a resource needs `grant.write` there *and* every permission of the role there (`Policy.MayGrant`). A custom role may hold only tenant permissions, and only ones its author holds.
- **Scope rules**: an app role is granted only on `app`, a tenant role only on a tenant or below, and a custom role only in its own tenant. Only members of a tenant are given roles in it. Grants are managed from within the principal's tenant (or on `app`), so a tenant's roles are changed while working in it.
- **Caching**: the policy caches each user's grants and every role, and drops the cache on any change (one server). The token's `PolicyVersion` is not checked yet.

**The administrator flag becomes grants** (question 7)
- `identity.MigrateRoles` runs at start. While the `users.admin` column exists, it grants *Super Administrator* to each flagged user, *Editor* to every member (*Owner* to user 1 on the default tenant), and drops the column.
- While no user exists, the reserved user 1 holds *Super Administrator* and *Owner* of tenant 1 (`identity.Bootstrap`). That is single-user mode's implicit principal, and setup's first user inherits it.
- `Principal.Admin` and the token's `adm` claim are gone. `User.SuperAdmin` is read from the grants, and the DTOs' `admin` became `superAdmin`.
- A new member gets *Editor* (`identity.DefaultMemberRole`). A tenant's creator becomes its first member and *Owner* (`identity.CreatorRole`). Leaving a tenant, or its deletion, revokes the grants in it.
- One *Super Administrator* always remains, and an enabled one at that (`409 last_admin`).

**Where checks moved**
- **Settings**: `config.read` is `self`. `config.write` is `admin`-class on the scope's path (`app/config:<id>`, `tenant:<id>/config:<id>`, `user:<id>/config:<id>`); `config` is user-owned, so everyone writes their own user scope. A patch touching a `secret` field also needs `config.writeSecrets`.
- **Volumes**: kb operations check `volume.read`/`volume.write` on the Volume holding the folder (`volumeservice.Resource`: `tenant:<id>/volume:<marker id>`), else `kb.read`/`kb.write` on `tenant:<id>/kb`.
  - Collaboration opens a file with `volume.read`. Without `volume.write` the file opens read-only: the room drops the connection's updates (but a read-only writer's seed), ignores its saves, never sends it tool edits, and hands the writer role to a writable connection that joins.
  - The `library` source drops chunks of Volumes the caller may not read.
  - The kb tools check the Volume of the file.
- **Personal resources**: `conversation.*`, `chronicle.read|write|share`, `account.*` and `logs.ingest` are `self`; their services already scope to the owner.
- **`logs.read`** is now `admin`-class, so the Log Explorer is for a tenant's *Owner* and *Administrator*. Before, every member could read it.
- `tenantservice.List` without `tenant.list` returns the principal's current tenant when they may add members there, so a tenant's *Owner* or *Administrator* sees the Tenants editor. Adding members still needs `user.list` (*Tenant Administrator* or *Super Administrator*).

**`authzservice`** (`/authz`: `GET me|resource-types|roles`, `POST roles/save|roles/delete|grants|grant|revoke|explain`) and `src/services/Authz.ts`. `me` returns the principal's grants with their roles' permissions (`*` expanded), the `self` actions and the owned types. `src/session/permissions.ts` mirrors the check in the browser: `useCan()` and `canIn(ctx, …)` for a command's `when`. The Editors view now passes `{ can, tenantId }` in place of `{ admin, tenantId }`.

**The Role Editor** (Administration › Roles, `src/editors/roles/`, shown with `role.read`)
- **Roles** lists the application, tenant and custom roles. A role shows its **permission matrix**: a row per resource type grouped by feature, a column per action class, a checkbox per declared action, with row and column toggles and a filter. Tenant roles leave out app-held actions.
- *Duplicate* makes a custom role from a tenant role (keeping only what the author holds). Custom roles are saved with a revision (409 `role_conflict`) and deleted with their grants.
- **Grants** lists who holds the role and where, and grants it with the resource picker (`ResourcePicker`: this tenant, its folders outside Volumes, or a typed path) and an optional expiry.
- **Access checker**: a person, an action and a resource, answered with the decision's reason (for example "Editor granted on tenant:1/volume:42 by user 1 on 2026-10-23").

**The Share dialog** (`src/components/share/ShareDialog.tsx`) lists and edits the grants on one resource, and lists the grants that apply from above, read-only. It is used from the Volumes editor (*Share volume*) and the Tenants editor (*Roles…*, current tenant only). The Tenants editor's members show their roles.

**Settings scopes**: the App, Tenant and User toggles are live. Settings opens at App for those who may write it, else at User. A section the user may not write at a scope shows *Read-only*, with every control disabled. The browser now reads settings at the user scope by default (`useConfig`, `useSetting`), so tenant and user overrides apply.

**Chronicle sharing and moderation**: see [MultiTenant.md › Built in slice 11](MultiTenant.md#built-in-slice-11).

**Verified**:
- Go tests: the policy (inheritance, expiry, tenant isolation, *Tenant Administrator* without content, owned settings, no escalation, the last *Super Administrator*, leaving a tenant), the migration and bootstrap, the entries table's shared rows, and the whole sharing flow.
- A curl walkthrough on a fresh data directory: setup, users, an *Editor* refused app settings but allowed their own, a *Viewer* refused a folder, granted *Editor* on one Volume and then allowed there but not outside it, a non-member refused a role, and the Access checker's reason.
- Headless Chromium, light theme only: the Role Editor (matrix, grants, a custom role saved, the Access checker), the Tenants editor's *Roles…* dialog, a Volume's Share dialog, Settings at App (read-only for an *Editor*) and User, a Chronicle shared globally from the entry editor, the moderation queue, and a *Viewer*'s Markdown file opened read-only (typing changes nothing).

### Decisions to review after seeing it
- *Administrator* holds every tenant permission but the `delete` class (not only `read`, `admin` and `share`), so that it can grant *Editor* under the no-escalation rule.
- Built-in roles are defined by action classes; a resource type's actions choose their class.
- Personal resources (conversations, your own Chronicles, your account, settings reads) need no grant, only membership.
- `logs.read` needs *Administrator* or *Owner*.
- Grants are managed only from within the tenant (or on `app`), and only members are given roles.
- A Volume grant only adds: browsing the folders still needs `kb.read` on the tenant.
- A read-only file is enforced by the room dropping updates; the views open read-only.
- Granting and revoking need every permission of the role (no escalation).
- Denied checks of changing actions are audited on every call, with no rate limit.
- The browser reads settings at the user scope.
- The DTO field `admin` became `superAdmin`.

## Built in slice 12
- **`secret.list` and `secret.write` are checked** on `app` by `secretsservice` ([Secrets.md › Built in slice 12](Secrets.md#built-in-slice-12)). The *Application Administrator* role holds only them, and *Super Administrator* includes them.
- **Secrets in Settings**: a patch touching a `secret` field at the app scope needs `secret.write` on `app`, *in place of* `config.write` and `config.writeSecrets`, so an *Application Administrator* sets the app's keys without being able to change any other app setting. At a tenant or user scope it needs `config.writeSecrets` there, as in slice 11; plain fields always need `config.write`.
- The bootstrap routes (`/bootstrap/status`, `/bootstrap/unseal`) need no session and no check: only an operator who may make SecretIDs for the server's AppRole in Vault can make a token the server accepts, and each token works once.
