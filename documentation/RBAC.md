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

## Since slice 33
Forge 05c ([Apps.md › Built in slice 33](Apps.md#built-in-slice-33)): an installed app's roles are custom roles of the tenant marked with their app (`CustomRole.AppID`, `AppRole`, `Rules`), made and removed by the system with the install, and read-only in `roles/save|delete` (409 `app_role`) and the Role Editor (*App roles*). Their permissions are **scoped**, `<scope>/<type>.<action>` (`app:tasks/entity:task/record.read`), held only on that path below the tenant (`Role.HoldsOn`), and `MayGrant` checks each on its own path. A resource type may have a **fence** (`ResourceType.Fence`; `record` has `app`): a grant above a segment of that kind reaches past it only through the Super Administrator, *Owner*, *Administrator* and that app's own roles. `authz.RowRules` gives the row rules the roles allowing an action put on a resource. `GET /authz/me` carries `fences` and each grant's `app` and `crossesFences`, which `can` mirrors.

## Since slice 49

Features may contribute built-in tenant roles (`authz.ContributedRoles`, point `authz.role`; [Connectors.md › Built in slice 49](Connectors.md#contributed-built-in-roles)): fixed permissions, usually scoped (`<scope>/<type>.<action>`, held only on that path by `Role.HoldsOn`, as app roles' are), ids reserved from 100 to 999, listed with the built-in roles and neither edited nor deleted. Forge's *Mail administrator* (id 100) is the first: `config:mail/config.write` and `connection.read|use|manage` under `connectors:mail`. Such a role is granted beside a member's role, which holds the tenant-wide checks every command passes first.

## Built in slice 51
Forge 07j, agent principals and roles granted on a record ([forge 07j-AgentPrincipals.md](../../forge/documentation/plans/07j-AgentPrincipals.md); questions 1 and 2 answered with their proposals before building, 2026-11-20).

### Agent principals
- **A principal kind `agent`** (`features.PrincipalAgent`, `features.AgentPrincipal(tenant, agent, onBehalfOf)`; `Principal.AgentID`, `OnBehalfOf`, carried by `PrincipalRef` too, `String()` `agent:<id>`). It cannot sign in (`session.*` is refused) and has no password or session.
- **An agent is its record.** The records feature answers for agents (`authz.Agents`, set by `serve.go` with `authz.SetAgents(recordsdata.Agents{…})`): an agent is a record of an entity type with a **`principal` field** (computed, `kind: agent`, at most one per type; its value the record's own id, written by the create's decision). Its id is the record's, its name the record's title (renamed with it), its creator the record's `_createdBy`, and it is **retired when the record is deleted**: every check of a retired or unknown agent is denied. Nothing is minted or copied, so every process answers alike from the projections (`authz.FindAgent`, `authz.ListAgents`, `authz.AgentInfo{ID, TenantID, Name, Entity, App, CreatedBy, Resource}`).
- **Agents hold roles as users do**: `RoleGrant.AgentID` (with `UserID` 0; `AccessAudit.AgentID` too), `GrantArgs.AgentID`, `Policy.GrantsOfAgent`; `Check`, `RowRules`, `EffectiveFor` and `HasRole` read an agent's own grants. Grants are re-read after every change (`Changed()`), so **a revoked grant fails the agent's next check**.
- **Using an agent** (`agent.use`, the resource type `agent` fenced by `app`, checked on the agent's record path): its creator may, the service principal may, and anyone holding `agent.use` on the record (`authz.MayUseAgent`). `authz.AsAgent(ctx, p, id)` is the principal p runs work as: the agent, `OnBehalfOf` p's user (an agent's own `OnBehalfOf`, or the creator for the service and app principals).
- **Runs as the agent**: *Enqueue job*'s `As agent` pin and `jobs.Request.As` of kind `agent` (`job.enqueue` accepts it when the enqueuer may use the agent and `onBehalfOf` is theirs), and *Call graph*'s `As agent` pin (`cognition.ExecHooks.CallAs`, `cognitionnodes.AsAgentPin`). Every command, query, connector call (`connections.CheckAccess`) and tool call in such a run is checked against the agent's grants, never its creator's.
- **Audit and presence**: an agent's events carry `agentId` and `onBehalfOf` (`live.Event`, `UserID` 0) and the actor `agent:<id>`; the operation log records the op as its user's (`ops.Entry.Principal` = `onBehalfOf`) with that actor. `presence.SetAs` with an agent principal shows it for its user as an actor with its own name and colour (`Entry.AgentName`, `AgentColor`; `PresenceAvatars` draws the robot in the agent's colour, named "<agent>, for <user>").
- **An agent never holds more than its granter could give**: granting goes through `Policy.MayGrant` as the granter, whoever made the agent.

### Roles granted on a record
- A grant's resource may be one record: `tenant:<t>/app:<appId>/entity:<type>/record:<id>` (`authz.RecordResource`, `authz.RecordOf`). An app role granted there (`GrantableOn` already allowed it) applies, for `record.*` actions only, to its scope's entity types: **its own type, restricted to that record** (`record._id == "<id>"`, or-ed with the role's rule for the type), and **each type the role has a row rule for, restricted by that rule**; a type with neither gets nothing from it. Such a grant is always restricted in `RowRules`.
- **`principal.hasRole(role, record)`** in row rules (`authz.HasRole`): whether the principal holds the app role (its id in the manifest) on that record, or on the tenant or the role's app; `record` is an id as text or number, a relation's value `{id}`, or a list of them: `principal.hasRole('mailSender', record.account)`. Row rules also see `principal.agent` and `principal.onBehalfOf` (ids as text), and `principal.kind` `agent`.

### Granting
- **`authzservice`**: `POST /authz/grant {agentId}` grants to an agent of the tenant in place of a member (400 `not_member` for an unknown or retired agent, `invalid_grant` with both), `POST /authz/agents {resource?}` lists the tenant's agents by name (`grant.read` on the resource), `POST /authz/explain {agentId}` explains for an agent (`grant.read`); grants carry `agentId`.
- **Nodes** (`features/authz/nodes`, `authznodes.Contribute`, category *Access*, in actions, triggers, handlers, function and agent graphs): *Grant role* (`authz.grantRole`) and *Revoke role* (`authz.revokeRole`) take an agent, a role (an app role's manifest id, the running graph's app's first, or a role id) and a record (`entity` + `record`) or `resource` (the tenant by default), as the run's principal through `MayGrant`; they leave by *Failed* with the reason. They write to the application database outside the event stream, so they are External (a sandboxed run stubs them).
- **The Share dialog and the Role Editor** offer agents beside people (`agentsFor`, ids `agent:<id>` in `grants.ts`, `granteeOf`, `granteeRequest`), with a robot icon; the Access checker explains for an agent too. A page's *Share* button opens the dialog on one record: the `share` UI action (`ui.Handle.Share(resource, title)`, the *Share* node `ui.share` with `entity` + `record`), handled by `AppHost`.

### Verification of slice 51
- `authz` `TestAgentGrants`: an agent's grant on one account gives it that account (`record._id`) and its messages (through `hasRole`'s rule), nothing on other types or apps; a grant on one connection gives `connection.use` there and not on another; a revoke fails the next check; the creator may use the agent and an Editor behind the fence may not; a retired agent may nothing.
- `apps/installs` `TestAgentPrincipals` (the Tasks app installed with an agent entity type, a *Sender* role and two tools): the agent record makes the agent (a command cannot set the principal field; renaming the record renames it); before a grant the agent reads nothing; with *Sender* granted on one task through `authzservice`, the agent sees only that task (`records.find`), changes it and is refused another (`record.update`), and the app's tools do the same as the agent while running as the owner for the owner; Eve cannot use or grant to the agent; the Share dialog's list has it; *Run in sandbox* as the agent decides the change and commits nothing; a revoke fails the next command; deleting the record retires the agent.
- `tools` `TestLoopLetsAnAgentWrite`; `records` `TestRuleHasRole`.

### Decisions to review after seeing it
- **Agents are read from their records, not minted** into a table: one source of truth and nothing to keep in step across processes. A deleted agent's grants stay, inert (its id is never reused); they are not revoked.
- **A record grant reaches other types only through the role's row rules**, never by the record type alone, and gives only `record.*` actions.
- **An agent's writes run without asking** (`tools.ApprovalAgent`; the Agent node offers an agent its write tools): its roles are the control (iteration question 5).
- **The grant nodes grant to agents only**; people are granted roles in the Share dialog and the Role Editor.
- **`agent.use` is checked when work is started** (a job enqueued, a graph called), not again when a delayed job runs; the agent's own grants are checked at every step.

## Changed in slice 54
Forge 7m ([Apps.md › Changed in slice 54](Apps.md#changed-in-slice-54)): an app principal may use the agents its own entity types define (`MayUseAgent`, by `AgentInfo.App`), and holds every action marked `Action.Apps` in its own tenant with no grant (`job.create`, so its triggers enqueue jobs).

## Changed in slice 56
Forge 7o ([forge MailApp.md](../../forge/documentation/MailApp.md#built-in-part-7o)): what the mail client's privacy and members needed.
- **`principal.owner`** in row rules: whether the principal holds the tenant's *Owner* role on the tenant, or is the Super Administrator (`authz.IsOwner`, from the cached grants). An app's type-level access rule can let Owners read what Administrators may not: an Administrator's grants are not restricted by app roles' row rules, so only the type's own rule (which everyone is held to) can hide records from them.
- **An app grants its own roles on its own records**: `Policy.MayGrant` allows an app's principal a role of that app (`Role.App`, its tenant) on a resource under `tenant:<t>/app:<appId>/…`, and nothing else (another app's role, a tenant role, the tenant itself, another tenant). The app's blueprints decide whom, after a command its roles allowed (Mail's trigger keeps an account's members' grants).
- ***Grant role* and *Revoke role* take a `user`** (a user id, or `user:<id>`) in place of an `agent`; revoking matches the grant's user and agent both.
- **The *Has role* node** (`authz.hasRole`, config or input `role`, input `record`, output `has`), in command and query graphs as well as actions, triggers, handlers, function and agent graphs: whether the run's principal holds an app role on a record or above it, as `principal.hasRole` tells in a rule. A command refuses what only a role allows with it (Mail's `draft.send` and *Mail sender*).
- Tests: `authz` `TestAppGrantsItsOwnRolesAndOwner`; Forge's `starter/mail` `TestPrivacyAndMembers` and `TestMailSender` over the nodes and rules.

## Changed in forge 7p-3
**An agent run's reach**: what an agent's run is about, so a host's tools can keep its writes there even where the agent's roles reach further (an agent holding *Member* on a shared mailbox may change any of its threads; a run on one thread should change only that one). Forge's Mail uses it ([forge MailApp.md › Built in part 7p-3](../../forge/documentation/MailApp.md#built-in-part-7p-3)).
- **`features.Principal.Reach`**: the run's subjects as record keys `<entity>:<id>`, separated by spaces (comparable, so the principal stays a value type); `ReachKeys()` splits them. Empty means not confined: a person, an app, or an agent run started without one. It **grants nothing**: access is still the agent's grants alone. `PrincipalRef` carries it as `reach`, so work an agent enqueues as itself keeps it.
- **Set by *Call graph***: an optional input `reach` (json: a subject `{entity, id}`, a list of them, or keys), read only with `As agent` (`cognitionnodes.ReachOf`). A malformed reach fails the call (through *Failed* with `catch`).
- **Never widened**: `authz.AsAgent` copies an agent caller's reach onto the agent it runs as, and *Call graph* refuses a different reach for a run that has one (*an agent's run cannot change its reach*).
- ***Current user*** (`live.currentUser`) also gives `kind` (`user`, `app`, `agent`, `service`, `anonymous`), `agent` (the agent's id as text) and `reach` (`[{entity, id}]`, an empty list when not confined). A host's tool graph reads `reach` and refuses a write outside it; what counts as inside (the subject itself, several selected threads, threads from the same senders) is the host's choice, made in its graph.
- Row rules do not see the reach yet: a host that wants the store itself to refuse writes outside the run could add it to `principal` in the rule environment.
- Tests: `features` `TestPrincipalReach`, `cognition/nodes` `TestReachOf`, `cognition/drafts` (Call graph's pins); end to end, Forge's `starter/mail` `TestTriageOnArrival` (an injected message's writes to another thread refused).

## Changed in forge 7p-4
Found while building Forge's Mail drafting and sending ([forge MailApp.md › Built in part 7p-4](../../forge/documentation/MailApp.md#built-in-part-7p-4)); question 1 of 7p-4 decided by the user (2026-11-22).
- **An agent may always know its own definition.** *Current user* (`live.currentUser`) also gives **`agentRecord`**: when an agent runs the graph, the record that defines it (its fields by id, with `_id` and `_entity`), read whatever the agent's own grants allow; `null` otherwise. A host's tool reads a setting of the agent from it (Mail's `mail.draftReply` reads `autonomy`). It comes from `authz.AgentInfo.Record`, which the records feature's agent directory (`recordsdata.Agents`) fills with the record's values; the field is never sent in `/authz/agents` listings (`json:"-"`).
- **A role's caution for agents.** A manifest role may carry **`agentWarning`** (`apps.Role.AgentWarning`), kept on the app role (`AppRole`, `CustomRole.AgentWarning` column, `Role.AgentWarning`, the roles API's `agentWarning`). The Share dialog shows it as a warning when that role is about to be granted to an agent (Mail: *Member* and *Mail sender*).
- **The Share dialog loads people and agents apart**: a failed people lookup (a single-user server has no tenant members) still lists the agents, and its error still shows.
- Tests: `authz` `TestAppRoles` (the warning kept with its role), `apps/installs` `TestAgentPrincipals` (the agent's record values with it); end to end, Forge's `starter/mail` `TestDraftsAndSending` (an agent whose autonomy is `suggest` refused, one whose autonomy is `draft` drafting).

## Changed in forge 15a
Agents search Volumes and memory ([ContextRetrieval.md › Changed in forge 15a](ContextRetrieval.md#changed-in-forge-15a)).
- **`authz.Action.Agents`**: an action every agent principal holds in its own tenant with no grant, as `Apps` is for apps. `retrieval.read` has it, so an agent can search at all; what it finds stays held to its other checks.
- **`authz.Action.AsMaker`**: an action decided for an agent by the grants of the person its run acts for (`Principal.OnBehalfOf`), never its own; a run acting for no one is refused. `volume.read` has it, so an agent never reads a Volume its maker could not, even when granted one itself. The plan named `authz.ContributeAgents` as the place; the flags on the actions were simpler and keep each feature's actions its own.
- **`features.Principal.MemoryOf`**: the person whose Chronicle memory an agent's run may search. It grants nothing else. `PrincipalRef` carries it as `memoryOf`, so work an agent enqueues keeps it. **Set only by *Call graph*'s `memoryOf`** input with `As agent`, checked by **`authz.MayRemember(caller, user)`**: a person may name only themselves, an agent's run only its own (never widened), and an app or the service any person (a host naming its subject's owner). `authz.AsAgent` copies an agent caller's `MemoryOf` with its reach.
- ***Current user*** also gives **`memoryOf`** (an agent run's, as text; empty otherwise) and **`owner`** (whether the user is the tenant's Owner, as row rules' `principal.owner`), so a command graph can keep a setting to Owners (Forge's Mail: a shared account's primary owner).
- Tests: `authz` `TestAgentRetrievalAccess`, `TestMemoryOfNeverWidens`; `cognition/drafts` (Call graph's pins).

## Changed in forge 15b
- ***May*** (`authz.can`, `authznodes`): whether the run's principal holds an action on a resource (the tenant by default), as `authz.Check` decides. For a page to show a control only to those who may use it (Forge's agent editor's *Customize*, which needs `cognition.write` and `cognition.publish`); every command is still checked.
- **`retrieval.vote`** (class write, `Apps`): voting on segments, which a host app's trigger does for a reviewer's outcome ([Tools.md › Changed in forge 15b](Tools.md#changed-in-forge-15b)).
