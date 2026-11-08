# Apps

Applications built from blueprints: a **manifest** of pages, navigation, editor kinds, entity types, commands, queries, roles, settings and triggers, edited by several people at once, then published, installed into a tenant and run in the workbench. Built as Forge's iteration 5 ([forge 05-Applications.md](../../forge/documentation/plans/05-Applications.md)), one part per slice. What remains is in the parts' TODOs in Forge's plans; part 5g, the Tasks app, is Forge's ([forge TasksApp.md](../../forge/documentation/TasksApp.md)).

## Built in slice 31
Forge's part 5a ([forge 05a-AppDrafts.md](../../forge/documentation/plans/05a-AppDrafts.md)): what an app is, as a draft edited by commands. Nothing is published, installed or run yet.

### The manifest
`apps.Manifest` (`apps/Manifest.go`):
```json
{ "id": "tasks", "name": "Tasks", "icon": "checklist", "description": "...",
  "home": "/mine", "frame": "",
  "pages":   [ { "route": "/mine", "prefab": "myTasks", "title": "My tasks" },
               { "route": "/task/:id", "prefab": "taskDetail" } ],
  "nav":     [ { "title": "My tasks", "route": "/mine", "icon": "person" } ],
  "editorKinds": [ { "kind": "task", "page": "/task/:id", "title": "{record.title}",
                     "subject": { "type": "task", "label": "{record.title}" } } ],
  "entities": [ "task" ],
  "commands": [ { "type": "task.reassign", "graph": "cmd-task-reassign", "action": "entity:task.update" } ],
  "queries":  [ { "name": "myTasks", "graph": "qry-myTasks" } ],
  "roles":    [ { "id": "manager", "actions": [ "entity:task.*" ] },
                { "id": "member", "actions": [ "entity:task.read" ],
                  "rowRules": { "task": "record.assignee == principal.user" } } ],
  "settings": { "schema": { "type": "object", "properties": { "defaultDueDays": { "type": "integer" } } },
                "defaults": { "defaultDueDays": 7 } },
  "triggers": [ { "id": "taskDone", "title": "A task is done" } ],
  "requires": { "primitives": [ "mui" ], "apps": [] },
  "version": 0 }
```
- **Parts stay where they live.** Prefabs (`ui`), graphs (`cognition`) and entity types (`records`) are edited by their own commands, with the owner `app:<appId>` (`apps.Owner`). The manifest names them by id.
- **Ownership** (forge 05a question 2): a part may be the app's own, the tenant's (a tenant-library prefab, an entity type with owner `tenant` or none, a graph with no app owner), or another app's, which needs that app in `requires.apps`.
- **Frame** is `prefab.<id>`, or empty for the default frame (built in 5d). **Home** is a page's route without params, or empty for the first page.

`apps.CheckManifest(manifest, env)` (`apps/Check.go`) reports every problem at a path such as `pages[/mine].prefab` or `roles[member].rowRules.task`, each with a fix a model can act on. `apps.Env` gives the tenant's prefabs, graphs and entity types (draft, else published; graphs also built-in) and the primitive catalog; `manifests.Env` builds it from the projections. It checks:
- the id and name; the frame's prefab;
- **pages**: routes start with `/`, their segments are tokens or `:param`, unique; the prefab exists; each route param is a prop of the prefab, and every required prop without a default is a route param (`apps.RouteParams`);
- **home and nav** name a page's route without params; nav items have titles;
- **editor kinds**: unique tokens (not `main`, the app's own tab), on a page's route, with a title;
- **entities**: exist, are not deleted, listed once;
- **commands and queries**: unique dotted names (`apps.ValidName`); the graph exists, is a `command` or `data.query` graph, and its spec defines that command type or query name; an action, when given, is `<resource>.<action>`;
- **roles**: unique ids; actions shaped `<resource>.<action>` (`entity:task.*`); row rules only for entity types the app uses, compiled with `records.CompileRule`;
- **settings**: a schema of `type: object`, the defaults fitting it (`tools.CheckArgs`);
- **triggers**: unique ids; **requires**: primitives are component ids or families (`mui` covers `mui.*`), apps are tokens other than the app itself;
- the owner of every part it names.

### Backend
- **Drafts** (`apps/manifests`, following prefab drafts; forge 05a question 1): an aggregate on the `def` stream, target type `app`, folded into the `apps.app` projection (`manifests.Draft {manifest, rev, createdBy, updatedBy, updatedAt}`). **Every command applies its events to a copy of the draft and runs `apps.CheckManifest`**, rejecting with problems at `manifest.<path>`, so no draft holds a problem.
- **Commands** (`app.write`):
  - `app.create {name, icon?, description?}`: the id is the target, or made from the name (*Tasks* is `tasks`, *My tasks* `myTasks`, then `_2`).
  - `app.setInfo {name?, icon?, description?, home?, frame?, requires?}` (`requires` replaced whole); `app.setNav {nav}` (replaced whole, in order); `app.setSettingsSchema {schema, defaults?}`.
  - **Keyed lists** (forge 05a question 4), each item named by its key: pages by `route`, editor kinds by `kind`, commands by `type`, queries by `name`, roles by `id`, triggers by `id`. Each has `app.add<Item>` (the item's fields, `index?`), `app.update<Item>` (the key, the fields to change, `rename?`) and `app.remove<Item>` (the key): `app.addPage`, `app.updatePage`, `app.removePage`, `app.addEditorKind`, … `app.removeTrigger`. Entity types are `app.addEntity {entity}` and `app.removeEntity {entity}`. Events are `<item>Added {item, index}`, `<item>Updated {key, patch}` and `<item>Removed {key}` (`manifests.Lists`).
  - **An update is a merge**: the fields given are a JSON merge patch on the item (`manifests.Merge`, RFC 7386): a field given is set, `null` clears it, an object such as `rowRules` merges per key, a list such as `actions` is replaced. The decision drops fields that would not change, and an update that changes nothing commits nothing. So two authors on different items never clash, and two on different fields of one item both land, the bus deciding again on a conflict.
  - **Renaming a page's route** follows it in `home`, the nav and the editor kinds' pages, in the same event. Other renames are only of the item; what names it must change first.
- **Each command's tool reports** `{rev, problems, keys}`, `keys` being every list's item keys, so a model sees what is there (`manifests.Report`).
- **Queries** (`app.read`):
  - `app.describe {id}`: `{manifest, rev, createdBy, updatedBy, updatedAt, parts}` (`manifests.View`). `parts` are the prefabs, graphs and entity types the manifest names, then those the app owns without naming, each `{type, id, name, kind, owner, rev, published, changed, named, owned, builtin, missing}`. It watches the parts' draft projections as well as the app's, so renaming a page's prefab reaches its subscribers. Only the app's own changes send `reached` when nothing it shows changed.
  - `app.list {search?}`: summaries keyed `app.<id>`: `{id, name, icon, description, rev, pages, updatedAt}`. Who is editing an app is presence on `app.<id>`.
- **The tenant database** (forge 05a question 1): the follower `fol_apps_apps` (`manifests.Follow`, started by `app.create` before its event commits and by `app.list`) keeps `apps_apps` (`apps.AppRow`: tenant, app id, name, icon, description, who made it) in step with `created` and `infoSet` events. The manifest itself stays in KV; versions and installs (5b) will refer to the row. `apps.Models()` are migrated by `serve`.
- **Access** (forge 05a question 3): the resource type `app` (`apps.ContributeAccess`, *App definitions*) with `read`, `write` and `publish` (class write) and `install` (class admin), on `tenant:<id>/app:<appId>` (`apps.Resource`); `app.create` checks `app.write` on the tenant.
- **Tools**: every `app.*` command is a tool (`livetools`); `apptools.Contribute` (`apps/tools`) adds `app.describe` and `app.list`.
- **Wiring**: `serve.go`, in Foundation and in Forge, contributes `apps.ContributeAccess` with the other resource types, and `manifests` and `apptools` after the prefabs and before `livetools`.

### Browser
`src/services/Apps.ts` declares the manifest's types (`AppManifest`, `AppView` for `useQuery<AppView>('app.describe', {id})`, `AppSummary` for `app.list`). There is no REST client yet: drafts are read and changed on the live connection. Forge's Applications editor (part 5f) is the first user.

### Decisions
Made while building:
1. **The manifest and its check are in `apps`, the drafts in `apps/manifests`**, as entity types are in `records` and `records/entities`.
2. **Entity types have their own ops**, `app.addEntity` and `app.removeEntity`; the plan listed none.
3. **Update arguments are flat**: the item's key, the fields to change and `rename`, rather than a nested patch, so a model writes `{"id": "member", "title": "Member"}`. Underneath it is a merge patch on the item's JSON, which keeps one generic implementation for every list.
4. **A command's or query's graph must define the name the manifest gives it.** A `command` graph's id is fixed by its spec (`cmd-<type>`), and a published one is already a command of the tenant (`bus.Resolve`), so the manifest's `type` and the graph's `spec.command` must agree; likewise `qry-<name>`. How an app's command types are kept apart from other apps' is forge 05b question 3.
5. **Route params must be the prefab's props**, and the prefab's required props without a default must come from the route, so every page can render from its URL alone.
6. **Primitives in `requires` are component ids or families, without versions**: components carry none yet.
7. **Actions are checked only for their shape** (`<resource>.<action>`, with `*`). What `entity:task.update` grants is forge 05c's.
8. **`app.describe` follows its parts**, by watching the prefab, graph and entity type drafts of the tenant, so the Applications editor's lists stay current. It recomputes on any part's change and sends only real changes.
9. **The tenant database keeps the app's identity, not its draft.** The draft is in KV like prefab and entity type drafts; the published manifest will be copied with its version (5b).
10. **Problem paths name list items by key in brackets** (`commands[task.reassign].graph`), since routes hold `/` and command types `.`.

### Verification of slice 31
- `go test ./features/apps/...` (also with `-race`, three runs in a row), against embedded NATS and SQLite:
  - the Tasks manifest built by commands alone (its id from its name; an entity type, three pages, nav, home, an editor kind, a command, a query, two roles with one put first by `index`, settings with defaults, a trigger); a second subscriber to `app.describe` sees each change as it lands, and the rename of a page's prefab; `parts` lists the named parts and an owned graph the manifest does not name; merged role updates (a title, then clearing a row rule); an unchanged update commits nothing; renaming `/mine` follows it in home and nav, and `/task/:id` in the editor kind; removes; the app's row reaches `apps_apps` with its later icon;
  - two authors, five rounds at once, one adding a page while the other edits a role: both land each time; two edits of different fields of one role both land;
  - refusals with paths: a missing prefab, a duplicate route, a route not starting with `/`, a param the prefab lacks, a required prop the route does not give, another app's prefab and entity type without `requires`, nav to a missing route and to a route with params, removing the home page, a missing frame, an unknown primitive, a duplicate editor kind and one on a missing page, a graph of the wrong kind, a missing graph, a graph defining another command, a malformed action, a row rule for an entity the app does not use and one that does not compile, removing an entity a rule uses, defaults that do not fit the schema, a rename onto a taken route, an app requiring itself, a command on no app; none changes the draft; requiring the CRM then lets the app use its parts;
  - the tools a chat model gets (`app.create` without a target, `app.addEntity`, `app.addPage` with its report of keys, `app.describe`, `app.list`), run directly in Go, and every `app.*` command's example fitting its own schema.
- `go test` of `ui/prefabs`, `records/entities` and `live/...` still pass; Foundation's `serve` vets, Forge's builds, and the common frontend type-checks.
- Forge's server, run fresh: the extensions list the 24 `app.*` commands, `app.describe` and `app.list`; over the live socket, `app.create {name: "Smoke app"}` is acked as `smokeApp`, a subscription to `app.describe` takes its snapshot, a page on a missing prefab is refused with `manifest.pages[/x].prefab` and its fix, `app.setInfo` reaches the subscriber as a change, and `apps_apps` holds the row with the new description.

### Decisions to review after seeing it
In the Applications editor (forge 5f):
- Decision 3 (flat update arguments) and the merge's rule that lists are replaced whole (a role's `actions`), once two authors edit one role's actions.
- Decision 8 (`app.describe` watching every part draft of the tenant): its cost with many prefabs and graphs.

## Built in slice 32
Forge's part 5b ([forge 05b-PublishInstall.md](../../forge/documentation/plans/05b-PublishInstall.md)): an app's lifecycle after its draft. A published **version** pins every part, an **install** puts the version's commands and queries on the live core, and **upgrade** and **uninstall** change it in place. Roles and settings (5c) and running pages in the workbench (5d) are not built yet.

Answered before building (2026-11-07), each with its proposal: forge iteration question 5, a publish takes the draft at the rev its publisher last saw, the others are told, and their later changes stay for the next version; 5b question 1, an installed app follows no part: a part published again reaches users only by a new app version and an upgrade; 2, one install per app per tenant, upgraded in place; 3, an app's commands and queries are `<appId>.<name>` on the live core and `<name>` inside the app; 4, triggers are recorded at install and none runs until forge iteration 11.

### Publishing
- **`app.publish {rev}`** (`apps/manifests/Publish.go`, action `app.publish`) publishes the manifest **as it was at `rev`**: the draft's own when `rev` is its last, else its events folded again up to `rev` (`Projections.History`, an ordered read of one target's events). Changes made since stay in the draft. `app.describe` gives `published`, `publishedRev` and `changed` (rev ≠ publishedRev), which is how the other authors are told and see what the next version will take.
- **Every part must be published**, and is checked as published: an unpublished prefab, graph or entity type is refused first, plainly (`parts.prefab[myTasks]`, code `app_unpublished`); then `apps.CheckManifest` runs against the published versions only, and `ui.CheckPrefab` runs on every prefab the version renders (left by forge 4a: the instances in its pages), with problems at `parts.prefab[<id>].<path>`.
- **The version** (`apps.Version`) holds the manifest and the pins: `prefabs` (the published version of every prefab its pages and frame render, and of those they render in turn), `entities` (each entity type's version), and `graphs`, **copies** of its command and query graphs and of those its prefabs name: a graph keeps only its last published version, so the app keeps its own.
- **Stored** as its own target, type `appVersion`, id `apps.VersionID` (`tasks-v3`), its `published` event committed in one batch with the draft's `published {version, rev}`; the `apps.version` projection keeps every version (`manifests.GetVersion`), and the follower `apps.versions` copies each into `apps_versions` (`apps.VersionRow`). Publishing starts the projection before it commits, and `GetVersion` waits for it to settle when the draft names a version it has not folded yet (after a restart).
- **`app.versions {id}`** (`app.read`): `{published, publishedRev, rev, versions: [{version, rev, publishedBy, publishedAt}]}`, live.

### Installing
- **`apps/installs`**: an app's install is an aggregate on the `def` stream, target type `appInstall`, id the app's (once per tenant), folded into `apps.install` as `apps.Install {app, status, version, installedBy, installedAt, updatedBy, updatedAt}`, holding the **whole version** it runs, so every reader sees one version or the next. The follower `apps.installs` keeps `apps_installs` (`apps.InstallRow`).
- **Commands** (action `app.install` on `tenant:<id>/app:<appId>`):
  - `app.install {version?}` (the last when left out): refused when installed already, when the app has no such version, and when the tenant lacks what it **requires** (`apps.Missing`: a primitive not in the catalog, an app not installed), each named at `requires.primitives[<id>]` or `requires.apps[<id>]` (code `app_requires`).
  - `app.upgrade {version?}`: only to a newer version, checked the same way.
  - `app.uninstall {}`: the commands and queries leave the live core; records stay, and installing again finds them.
- **`app.installs {}`** (`app.read`): every install keyed `app.<appId>`, `{app, name, icon, description, status, version, commands, queries, editorKinds, triggers, …}`, each command and query by its live name with its args' schema and action: what the Apps section (5d) and the chat read.

### The app's commands and queries
- **On the live core** (forge 05b question 3): `installs.CommandFor` and `installs.QueryFor` wrap `livenodes.CommandFor`/`QueryFor` as the bus's and the subscriptions' `Resolve`. `<appId>.<name>` of an installed app is its command or query, built from the pinned graph's spec.
- **A command decides by its pinned graph** (`livenodes.Decide` with `RunArgs.Graph`), reading the install again when it is decided: one decided after an upgrade runs the new version's graph, and one the new version removed is refused (`app_command_removed`). Once the upgrade is projected, the scoped name no longer resolves (404).
- **A query** runs its pinned graph (`livenodes.RunQuery` with a `QueryRun{Graph, Watch}`) and runs again when the install changes: a fresh **snapshot** when the query's graph is another version, and the subscription ends, with a message saying why, when the query is removed or the app uninstalled.
- **Graph ids** (`cognition.Kind.IDFor(spec, owner)`): a graph owned by `app:<appId>` may have the app's id in its own, `cmd-<appId>-<type>` and `qry-<appId>-<name>` (`cmd-tasks-task-reassign` defines `task.reassign`), so two apps can define one command type; the plain id stays allowed. A prefab of an app may declare a query whose graph has that id.
- **A manifest app's graphs are live only through its install**: a published command or query graph owned by an app that has a draft (`apps.app`) is not a command or query by itself. Apps written in Go (Hello) keep theirs live.
- **Tools**: an installed app's commands are sent by the chat with `live.command` and its queries read with `live.query`; `app.installs` lists them with their schemas.

### Wiring
`serve.go`, in Foundation and in Forge: `installs.Contribute` after `manifests.Contribute`, `installs.Deps{Projections, DB, Nodes: liveNodeDeps}`, and the bus's and subscriptions' `Resolve` set to `installs.CommandFor`/`QueryFor` around the graph resolvers. `apps.Models()` adds `apps_versions` and `apps_installs`. The browser's types (`AppView.published|publishedRev|changed`, `AppVersions`, `InstalledApp`) are in `src/services/Apps.ts`.

### Decisions
Made while building:
1. **No `appservice`.** As in slice 31, the lifecycle is commands and queries: publishing and versions in `apps/manifests`, installs and the runtime in `apps/installs`.
2. **The version copies its graphs** and names its prefab and entity type versions: prefab and entity type versions are kept, graph versions are not.
3. **A version is its own target** committed in the publish's batch, so the draft's projection stays small and every version is one read.
4. **Publishing at an earlier rev folds the draft's events again** (`Projections.History`) rather than keeping old drafts.
5. **Parts must be published before the app.** The publish does not publish them: what a part's author has not published is not frozen into an app.
6. **The install holds its whole version**, read again by every decision and every query run.
7. **Commands read the install as projected.** A command sent in the milliseconds between an upgrade's ack and its projection may be decided by the old version.
8. **Upgrades go only forward**; after an uninstall, any version may be installed.
9. **The manifest command's `action`** (`entity:task.update`) is not checked yet: the bus checks the graph spec's action. What app actions grant is 5c's.
10. **No per-app tools**: tools are registered once for every tenant, so the chat uses `live.command` and `app.installs`.
11. **Install creates no entity buckets**: every record is in the one `records.record` projection, so there is nothing to create.
12. **Editor kinds are registered by being installed**: `app.installs` lists them; opening them is 5d.

### Not built yet (in forge 5c and 5d)
- Roles made at install and the installer granted *manager*; the settings section (5c).
- Pages rendering the pinned prefab versions, and graphs named by prefabs run from the version's copies; removed pages closing their tabs and open sessions reloading at their next event (5d, ★ in forge 05b).
- Triggers running (forge iteration 11).

### Verification of slice 32
- `go test ./features/apps/...` (also with `-race`, three runs in a row), against embedded NATS and SQLite, with the record nodes:
  - a publish before the parts are published names each one; a rev past the draft's is refused; published, the version pins the prefabs, the entity type and copies of the two graphs, and `app.versions` lists it;
  - before installing, neither `tasks.task.reassign` nor `task.reassign` is a command; installed, `app.installs` lists the scoped command and query, the editor kind and the trigger; `tasks.task.reassign` sets a record's assignee through its graph's *Update record*, and a subscription to `tasks.myTasks` takes its snapshot;
  - the graphs published again change nothing installed; after publishing and upgrading, the command is decided by the new graph and the open subscription takes a fresh snapshot under the new query; an upgrade that removes the command refuses it (404) and one that removes the query ends its subscription with that reason;
  - uninstall keeps the record; installing version 2 again runs its graph; `apps_versions` and `apps_installs` follow;
  - publishing at the rev seen while another author added a trigger: the version lacks it, the draft keeps it with `changed`; publishing at the last rev leaves nothing waiting;
  - requires: an app requiring an app not installed is refused at `requires.apps[tasks]`, then installs once it is; a primitive the server lacks is named by `apps.Missing`;
  - a command graph owned by a manifest app is not a command by itself, while one owned by a Go app is; `cmd-crm-task-reassign` owned by `app:tasks` is refused, naming `cmd-tasks-task-reassign`.
- `go test` of `live/...`, `cognition/...`, `ui/...` (Hello, prefabs) and `records/nodes` still pass; Foundation's `serve` vets, Forge's builds, Forge's starter tests pass, and the common frontend type-checks.
- Forge's server, run fresh, over the live socket: `app.install` of an app with no version is refused naming it; `app.create`, `app.publish {rev: 1}` and `app.install` straight after are acked (the first run found the version projection started only by the install's read, so the install missed the version; publishing now starts it).

### Decisions to review after seeing it
- In the Applications editor's Versions tab (forge 5f): the notice to other authors as `changed` and `publishedRev`, and a publish refused until every part is published.
- Decision 7, once two people use an app while it is upgraded.


## Built in slice 33
Forge's part 5c ([forge 05c-AppAccessSettings.md](../../forge/documentation/plans/05c-AppAccessSettings.md)): an installed app's **roles** become custom roles of the tenant, its own entity types' records move under its path, its roles' row rules filter what each user reaches, and its **settings** become a configuration section read by pages and graphs. Running pages in the workbench (5d) is not built yet.

Answered before building (2026-11-07), each with its proposal: forge 05c question 1, install needs `app.install`, and the app's roles are made by the system holding only actions on the app's own resources; 2, an app's roles are read-only in the Role Editor and change with the manifest and an upgrade, their grants managed like any role's; 3, an installed manifest app's own entity types move under its path, the tenant's and Go apps' (Hello) stay; 4, row rules per role, a user's rule being the `or` of their roles' rules, the type's own rule still applying to everyone; and 5, found while preparing the build, the tenant's *Editor*, *Viewer* and *Moderator* do not reach an installed app's records: its roles and the tenant's *Owner* and *Administrator* do.

### Roles
- **Actions** (`apps/Roles.go`): a role's or command's action is `entity:<id>.<read|create|update|delete|*>` on one of the app's **own** entity types (`apps.EntityAction`); `apps.CheckManifest` refuses any other, an entity type the app does not use or does not own, and a command action of `*`. A role may be marked `installer: true` (at most one).
- **Custom roles at install** (`authz/AppRoles.go`): `CustomRole` gains `AppID`, `AppRole` (the manifest's id) and `Rules` (its row rules, JSON). `apps.AppRoles(version)` turns each role's actions into **scoped permissions**, `app:<appId>/entity:<id>/record.<action>`, which `authz.Role.HoldsOn` holds only on that path below the tenant, so an app role reaches nothing beyond its app. `Policy.SyncAppRoles(ctx, tenant, app, roles)` makes them what the version says, as the system (no `MayGrant`): new roles made, changed ones saved with a new revision, those gone removed with their grants; `Policy.RemoveAppRoles` removes them all.
- **The follower `apps.access`** (`installs/Access.go`, started with the installs follower) runs it: `installed` and `upgraded` sync the roles, `installed` grants the installer the installer's role (the marked one, else the first) on the tenant, and `uninstalled` removes the roles and their grants.
- **Granting** an app role works as any custom role's (`Policy.Grant`, the Share dialog): on the tenant, or on the app's path and below (`GrantableOn`); `MayGrant` checks each scoped permission on its own path, so only someone who holds it there may give it.
- **Read-only**: `authzservice` refuses `roles/save` and `roles/delete` of an app's role with 409 `app_role`; the DTO carries `app`, `appRole` and `rules`.

### Records under the app
- **Resource paths** (forge 05c question 3): `records.Resource(p, entity)` is `tenant:<id>/app:<appId>/entity:<id>` for a type the version lists in `Version.Owned` (its own types, recorded at publish) while the app is installed, else `tenant:<id>/records/entity:<id>`. It asks `records.SetPlacement(installs.Placement(...))`, which `serve.go` sets: a per-tenant watch of `apps.install` held in memory (`installs.cacheOf`), so no check reads the projection.
- **The fence** (question 5): `authz.ResourceType.Fence` (`record` has `app`): a grant above a segment of that kind (`app:tasks` below a grant on the tenant) reaches past it only through the Super Administrator, *Owner*, *Administrator* (`Role.CrossesFences`) and that app's own roles. Other types, and the tenant's own entity types, are unchanged.
- **The installed command's action**: an app command whose manifest names an action is checked as `record.<action>` on that entity type's path (`installs.actionOf`), in place of its graph spec's action; `app.installs` shows it.

### Row rules per role
- `authz.RowRules(ctx, p, action, resource, key)` collects the grants giving the action (fence and scope applied): unrestricted when one of them has no rule for the key (a role without one, or any other role, such as *Owner*), else every role's rule.
- `records.EntityType.RuleFor(ctx, p, action)` is the type's own rule `&&` (`or` of those rules), compiled and cached by source. Every place that ran `e.Rule(...)` now runs `RuleFor`: queries (`records.find`, `records.list`, `records.get`, expansions, aggregates, search), so live subscriptions send only allowed rows; record commands and the record nodes (the record before and after the change, as type rules); `records.query` and the other tools through `recordservice`. A *Member* with `rowRules: {task: "record.assignee == principal.user"}` reads and changes only their own tasks, and cannot give one away.

### Settings
- **Sections per tenant** (`config.SectionSources`, point `config.sectionSource`, declared in `serve.go`): a source gives a tenant's sections; `configservice` finds sections per principal (`config.SectionsFor`), lists them in `GET /config/sections`, resolves, validates and stores them like registered ones, and does not cache their resolved values.
- **An installed app's section** (`installs/Settings.go`): `app.<appId>` (`installs.Section`), the version's settings schema and defaults, title the app's name, category *Apps › <name>*, scopes tenant and user, gone at uninstall. `apps.CheckManifest` checks the schema parses as a section's (`config.ParseSchema`) and its defaults fit it.
- **`app.settings {app}`** (live query, `config.read`): the settings as the reader resolves them, secret fields left out, replaced whole when the section changes (`configservice.Watch`) or the install does.
- **In pages**: a prefab's `app.settings.<key>` binding, and `app.settings` in `=` expressions, read the page's app's settings (`ui.Page.App`): the session holds `app.settings` as `__app.settings`, so a changed setting re-renders what reads it.
- **In graphs**: the *App setting* node (`apps.setting`, config `{app, key?}`) gives a setting, or all of them, as the run's user sees them.

### Browser
- `Authz.ts`: `Role.app|appRole|rules`, `ResourceType.fence`, `Held.app|crossesFences`, `MeResponse.fences`; `permissions.ts`' `can` mirrors scoped permissions and the fence; `grantable` lets an app's role be granted on the tenant or within its app.
- The Role Editor lists **App roles** in their own group, read-only (*App tasks* chip, a table of each permission's entity type and row rule in place of the matrix, *Duplicate* disabled), with their **Grants** tab as for any role.
- Settings shows an app's section under *Apps* once loaded (the server's sections are read when Settings loads).
- `Apps.ts`: `AppRole.installer`.

### Wiring
`serve.go`, in Foundation and in Forge: `config.SectionSources` declared with the other points; `installs.Deps{…, Roles: policy, Config: configService}`; `records.SetPlacement(installs.Placement(...))`. `configimpl.New` and `authzimpl.New` make services apart from the singletons, for tests.

### Decisions
Made while building:
1. **Scoped permissions** (`<scope>/<type>.<action>`) rather than one grant per entity type: a role is granted once, on the tenant, and holds different actions on different entity types of its app.
2. **The fence is a resource type's** (`Fence: "app"` on `record`), not a rule of the Role Editor, so `authz` stays unaware of apps but for an app role's `App`.
3. **The installer's role is granted on the tenant**, so it shows with the member's other roles in Tenants and the Share dialog; an app role may also be granted on the app's path.
4. **Roles are kept by a follower** of the install events, as the system, after the install commits; a user may wait milliseconds for a new role's grant.
5. **A row rule applies to every action the role gives** on that type: a *Member* updating a task must leave it theirs.
6. **Placement follows the install as projected**, through an in-memory watch per tenant: for milliseconds after an install a check may use the tenant's path (as 5b's decision 7).
7. **Records go back to the tenant's path at uninstall**, where the tenant's roles reach them again; while installed, authors of the app who are only *Editors* reach its records through its roles.
8. **Sections from a source are not cached** by `configservice`, so an upgrade's schema and defaults show at once.
9. **`app.settings` is a held query**, not a value read at render, so a setting changed in Settings reaches open pages.
10. **Command actions name one action** (`entity:task.update`), since a command is checked for one.

### Verification of slice 33
- `go test ./features/apps/...` (also with `-race`, three runs in a row), against embedded NATS and SQLite, with a policy installed (`installs/access_test.go`): the Tasks app with *Manager* (`entity:task.*`) and *Member* (`entity:task.read`, `entity:task.update`, a rule on `task`) and a `defaultDueDays` setting, published and installed by the tenant's *Owner*:
  - refusals: an action that is not one (`entity:task.purge`) and a command action of `*`;
  - before install the records are at the tenant's path; after, at `tenant:1/app:tasks/entity:task`; the roles are custom roles with scoped permissions and the rule, the installer holds *Manager*, the Role Editor's list (`authzservice.Roles`) has them, and saving or deleting one is refused (`app_role`); the *Owner* may grant *Member* and an *Editor* may not;
  - a query: Alice and Bob (*Member*) each see their own task, Mia (*Manager*) and the *Owner* both, Eve (only *Editor*) is refused (403);
  - a command: Alice cannot change Bob's task (it is not there), take it with `tasks.task.reassign`, or give hers away (`row_rule`), and may rename her own; Eve cannot send the app's command (403, the manifest's action on the app's path);
  - a subscription: Alice's open `records.find` gains Bob's task when it is given to her and loses it when it goes back, and nothing else;
  - a chat tool: `records.query` gives Alice only her task;
  - settings: `app.tasks` is in `GET /config/sections`; a page of the app bound to `="Due in " + string(app.settings.defaultDueDays) + " days"` shows 7, then 3 after a tenant override; `app.settings` and the *App setting* node give 3;
  - an upgrade that takes `update` from *Member* changes the role in place and Alice can no longer update; uninstall removes both roles and every grant of them and the section, and puts the records back at the tenant's path, where Eve sees them.
- `go test ./features/authz` (`AppRoles_test.go`): scoped permissions, the fence for each built-in role and an app role granted on the tenant and on the app, `RowRules` restricted and lifted by a second role, `MayGrant`, a grant outside the app refused, sync changing a role in place and removing another with its grants, removal.
- Every Foundation backend package (`scripts/build.sh --test ./...`), Foundation's `serve` and Forge's vet, Forge's starter tests pass; the common frontend type-checks and the example and Axiom apps build and lint.
- Forge's server, run fresh, in the browser: over the live socket an entity type, an app with *Manager*, *Member* and a setting, published and installed; `GET /authz/roles` lists both roles with their scoped permissions and rule, `GET /authz/me` shows the installer's *Manager* grant and the `record` fence, `GET /config/sections` has `app.tasks`, and saving the role is refused with 409. The Role Editor shows *App roles* read-only with the permissions table, and Settings shows *Apps › Tasks* with its default. The first run found the server crashing when a check asked where an entity type was before the installs' store was made (startup); the cache now answers none until then.

### Decisions to review after seeing it
- The Role Editor's *App roles* group and its permissions table; granting an app role from the Share dialog and Tenants (forge 5f, 5g).
- An app's section in Settings › Apps, and a setting changed there reaching an open page (5d).
- Decision 5 (a row rule on every action) and decision 7 (authors who are only *Editors* lose the records of an installed app), once the Tasks app is used (forge 5g).

## Built in slice 34
Forge's part 5d ([forge 05d-AppRuntime.md](../../forge/documentation/plans/05d-AppRuntime.md)): an installed app runs in the workbench. It opens from the Editors view's **Apps** section as its own tab, its frame around the page at the current route with the tab's own back and forward; its editor kinds open their own tabs, titled by the manifest's templates over the page's data; pages render the versions the install pinned; a deep link opens it at a route; an upgrade reaches open tabs. Every page is multi-user without the app doing anything: its queries are subscriptions, its writes commands, and its tab shows who else is on the same subject.

Answered before building (2026-11-07), each with its proposal: forge iteration question 1, a draft app runnable by its authors, built with forge 5f's Run tab, so this part runs only installed versions; 2, the app's own history, in its frame (the workbench owns the URL); forge 05d question 1, Hello stays a Go app and the starter library's Records app becomes a manifest app that `cli seed` installs, the Apps section listing both kinds; 2, the default frame, *App frame*, is a Forge starter prefab, seeded with the others, used by an app that names no frame.

### The runtime
- **`ui.AppSources`** (point `ui.appSource`, `ui/Page.go`): where apps installed per tenant come from, beside the Go apps of `ui.Apps`, `ui.Pages` and `ui.EditorKinds`. A source gives a tenant's apps and editor kinds (`Apps`), resolves a page id (`Page`: not its own, a page, or an error when its app has no such page now) and says when they change (`Changed`). `uiservice.Open` resolves Go pages first, then the sources (404 `app_page_gone` when an app was uninstalled or an upgrade removed the page); `EditorKinds` adds the sources' apps and kinds.
- **`apps/installs/Pages.go`** is that source, from the in-memory installs cache (`cacheOf`), so resolving reads no projection:
  - **apps**: each installed app with its **reach**, what a user needs to use it, any one of them: `record.read` on one of its own entity types (`app:<appId>/entity:<id>`, so its roles and the tenant's *Owner* and *Administrator*, across the fence of slice 33), else `app.read` on the app;
  - **kinds**: `app:<appId>:main` (page `<appId>/main`) when the app has a page without params to open on (`Manifest.HomeRoute`: its home, else its first such page), and `app:<appId>:<kind>` (page `<appId>/<kind>`) per editor kind, titled by its page's title;
  - **pages**: `ui.Page`'s new fields carry the install: `Reach` (checked at open in place of `Action`: any one), `Pins` (the version's prefab versions; an instance naming no version renders the pinned one, nested ones too), `Graph` (the version's copies of its prefabs' graphs, which `uinodes` runs in place of the published ones), `AppCommands` and `AppQueries` (an app's own names → `<appId>.<name>`: a prefab's declared query, *Subscribe to records*, `Handle.Query` and `Handle.Command` use them, so `query:myTasks` reads `tasks.myTasks`), `Stale` (the install's version moved on), `Heading` and `Navigate` (below) and `Framed`.
- **The main tab** (`mainPage`) is `ui.AppPage`, a page whose root is a prefab instance its build function returns: the **frame** (the manifest's `frame`, else `apps.DefaultFrame`, `appFrame`, when the tenant has it published) with the page at the current route in its `page` slot (`apps.FrameSlot`; `apps.CheckManifest` refuses a frame without one). The frame gets, of `title`, `appName`, `nav` (`mui.list` items `{id: route, primary, icon}`), `selected`, `subject` (the page's presence object), `canBack` and `canForward`, those it declares. Without a frame the page is the root alone, under the tab's own header.
  - **Routes**: the view state's `route`, set at open from the params' `route` (a deep link) or the home; `apps.MatchRoute`/`Manifest.FindPage` give the page and the route's params, which are the page prefab's props; an unknown route (one an upgrade removed) shows the home. The page instance is keyed by its path (`page_task_123`), so another task mounts afresh.
  - **History** (iteration question 2): the view state's `back` and `forward` lists (at most 50), kept with the session's record, so a reload keeps them. The page's Go handlers `app.navigate {route}`, `app.back` and `app.forward` move along them; navigation is the runtime's, not the app's logic.
  - **Events of the root**: a root instance's events (the frame's `navigate`, `back`, `forward`; a page prefab's `navigate {route}`, which the runtime maps too) now reach the page's Go handlers (`Handle.Raise` with no parent instance), where before they went nowhere.
  - **The *Navigate* node** (`ui.navigate`, route from its input or config): on an app's main tab, `Handle.Do` hands a `navigate` action to `Page.Navigate`, which goes there in the tab (an unknown route toasts); elsewhere it reaches the browser, which opens the app's tab at that route.
- **An editor kind's tab** (`kindPage`): its page's prefab alone, the route's params (from the tab's params) as props. The page holds the record the params name as `record` (`records.get`, the entity type being the kind's subject type when the app uses it, else the `:entity` param), with `_title` from the entity type's title field (`entity.describe`). The kind's `title` and `subject.label` templates (`{record.title}`) are filled over `params`, the root instance's `props` and its queries by name, and `record`, the page's own winning over a prefab query of that name; empty templates fall back to the page's title.
- **Heading**: `Page.Heading` gives the tab's title and subject from the page's data; the session sends it in the tree frame and, when it changes, in a patch frame's new `title` and `subject` (`PatchFrame.Title|Subject`). `Subject.Object` is the presence object: `record_<entity>.<id>` for a page showing a record (as the starter prefabs' presence bars have it, so the main tab on `/task/123` and the task's own tab share it), else `app_<appId>.<path>`. The main tab's title is the app's name.
- **Upgrades**: an event to a session whose app moved on is refused with 409 `page_changed` (`Session.stale`), and the browser opens the session again on the new version, its view state kept; a page an upgrade removed fails to open with `app_page_gone`.
- **`ui.editorKinds`** (live query, `ui.read`, in `uihandlers.ContributeLive`): `{apps, kinds}` as `GET /ui/editor-kinds` gives them, replaced whole when a source says its apps changed. The REST endpoint stays.
- **Primitives**: `mui.box` and `mui.stack` take `grow` (the rest of their row or column, scrolling what does not fit) and `mui.stack` `fill` (the height it is given), since `sx` takes only theme tokens and a frame needs a page beside its navigation. The icon map adds `arrow_back`, `arrow_forward`, `list`, `task` and `table_rows`.
- **Tests' helpers**: `ui.ApplyPatch` applies a patch as the browser does (it was internal to the session tests); `Render.Instance(kp)` gives a mounted instance's props and queries, for a heading.

### Browser
- **The Apps section** (`src/ui/editorKinds.ts`) follows `ui.editorKinds` (falling back to `GET /ui/editor-kinds` once when refused): a `workbench.editor` per kind, and a command per app with a main tab, shown by its reach through `canIn` (`tenant:<id>/<resource>`; none, as Go apps, is everyone). When the list drops a kind (an uninstall, an upgrade), `onKindsRemoved` tells `Workbench.tsx`, which closes its tabs.
- **`openApp(registry, app, route?)`** and `openAppAt(registry, appId, route)`: an open main tab goes to the route (`onNavigateRequest`, registered by its `AppHost`; a route asked for before the tab mounts waits), a new one opens there (params `{route}`).
- **Deep links**: `/workbench?app=<appId>&route=/task/123` (`openDeepLink`, run by `Workbench.tsx` as the Log Explorer's are).
- **`AppHost`**: a framed page (`UiSessionState.framed`) has no header of its own, only the connection chip when offline; the tab is renamed to the page's title as it changes; a `navigate` action from an editor kind's tab opens the app's main tab there; `app_page_gone` closes the tab. Presence is the subject's `object` (else the tab's own, as before), joined by the tab only when no `presence.avatars` in the tree joins it already, so a page's presence bar and its tab never join the same object twice from one window.
- **`UiSession`**: patch frames' `title` and `subject` update the state; `page_changed` reopens the session, as `session_not_open` does.
- `Ui.ts`: `AppInfo.reach` (`AppReach`); `types.ts`: `UiSubject.object`.

### Wiring
`serve.go`, in Foundation and in Forge, declares `ui.AppSources` with the UI points; `installs.Contribute` registers the source. Forge's `serve.go` no longer contributes the Go Records app.

### Decisions
Made while building:
1. **`ui` asks a point, `apps` answers.** `ui` cannot import `apps` (apps imports ui), so installed apps are a `ui.appSource` contribution, as config sections from a source are (slice 33).
2. **Navigation is in view state, run by Go handlers of the main page**: the route and history are the session's own, saved with it; they are the runtime's, so they are not graphs.
3. **A root instance's events reach the page's handlers.** Before, an event raised at the root went nowhere; now a frame and the page in its slot raise to the page, which is how the frame's buttons and a page's `navigate` work without the app writing a handler.
4. **The frame gets only the props it declares** of the runtime's seven, so an app's own frame declares what it shows.
5. **Without the default frame, no frame.** A Foundation server without Forge's starter library shows the page alone under the tab's header, rather than an error.
6. **Who may use an app is who may read its own records**, else who may read the app: the Apps section and opening agree, and a tenant *Editor* does not see an app whose records they cannot read.
7. **An editor kind's tab holds its record**, so `{record.title}` works whatever the prefab's queries are called; the page's `record` wins over a prefab query of that name (the starter Record detail has one, a `records.find` page).
8. **A page showing a record is present on `record_<entity>.<id>`**, the object the starter presence bars use, so the app's tab, a task's own tab and the Records app's tabs show the same people.
9. **An upgrade reopens a session at its next event** rather than pushing a new tree to every open session; the event is not run.
10. **The Records app has no main tab**: both its pages need params, so it is not in the Apps section and opens from the Data Designer, as before; its tab ids are now `app:records:<kind>:<record id>`.
11. **`grow` and `fill` as props**, not new `sx` keys: `sx` stays theme tokens only.

### Verification of slice 34
- `go test ./features/apps/...` (also with `-race`, three runs in a row; `pages_test.go`), against embedded NATS and SQLite: the Tasks app with *My tasks* (a heading and a button whose `ui.handler` graph navigates to `/task/t1`), *Task detail* and a frame declaring five of the seven props, published and installed:
  - the source lists the app with its reach (`record.read` on `app:tasks/entity:task`) and the kinds `app:tasks:main` and `app:tasks:task`; a page of an app never installed is not its own;
  - after the install, *My tasks* and the graph are published again: the main tab still shows the pinned version, and the button still goes to `t1` (the version's copy of the graph), the frame showing `My tasks` with the presence object `app_tasks.mine`, then `record_task.t1` with Back enabled;
  - back, forward, `app.navigate` to `/task/t9`, a route the app lacks (nothing changes), and back home; a deep link (`{route: "/task/t1"}`) opens on the task;
  - the editor kind's tab for `t1` is titled *Write docs*, its subject *Task Write docs* on `record_task.t1`; renaming the record sends the new title and label in a patch frame;
  - an upgrade removing the kind: the main tab's next event gets `page_changed`, the kind is gone from the list and its page fails with *no editor kind task since version 2*; after uninstalling, the list is empty and the main page fails as the source's own;
  - without *App frame* in the tenant, the main tab's root is the page itself.
- `go test` of every Foundation backend package (`scripts/build.sh --test ./...`); Foundation's and Forge's `serve` vet; the common frontend type-checks; the example and Axiom apps build and lint.
- Forge's tests (`go test ./features/starter`): seeding publishes nine prefabs and the Records app, seeding again changes nothing, a reset prefab republishes the app as version 2 and upgrades it, a removed kind is put back; `TestRecordsApp` opens `records/record` for a record: the Record detail, the tab titled by the record's title field and following its rename, present on `record_task.<id>`.
- **In headless Chromium**, Forge's server run fresh with Alice (*Super Administrator*, the tenant's *Owner*) and Bob (*Editor*), seeded with `cli seed`, and a Tasks app made over Alice's socket (an entity type, *My tasks* and *All tasks* over Record table, *Task detail* over Record detail, an *Open a task* handler graph, nav, an editor kind `task` titled `{record.title}`, *Manager* and *Member*), published and installed, with three tasks: Bob's Apps section lists only *Hello*, Alice's *Hello* and *Tasks*. Tasks opens in its frame on *My tasks* (the rail with *My tasks* selected, the three tasks); *All tasks*, Back, Forward and Back move through its history; double-clicking *Write the plan* opens a tab named *Write the plan* with the record; after a reload both tabs are back. A deep link to *Ship it* opens the app's tab on that task. Two of Alice's windows on *Write the plan*, one in the app's tab and one in the task's tab, each show the other's avatar. An upgrade removing the kind closes the task's tab in the first window, and the other window's next click reopens its session and navigates. The first run showed the frame's back, forward and *list* icons as clipped text: the icon map lacked them.

### Decisions to review after seeing it
- The Apps section once several apps are installed, and the frame, its navigation and back and forward ★ (forge 05d).
- A page with its own presence bar inside the frame shows two (the frame's and the page's), and the frame's title on a route with no page title is the app's name rather than the record's.
- Decision 6 for an app with no entity types of its own: everyone who may read the app sees it.
- Decision 10: the Records app not in the Apps section.


## Built in slice 35
Forge's part 5e ([forge 05e-ExportImport.md](../../forge/documentation/plans/05e-ExportImport.md)): a published app is exported as one JSON document and imported into another tenant, and every export runs the **blueprints-only check** (forge Architecture rule 1).

Answered before building (2026-11-07): forge iteration question 3, sample records are opt-in (`seed: true`), carried by the version and created at install only into an entity type with no records of its own; 05e question 1, the app keeps its id when free, else import is refused unless a new id is given; asked again, since parts name each other deep in prefab props and graph configs, **every part keeps its id** and only the app's `cmd-<app>-…`/`qry-<app>-…` graphs follow a new app id; 2, the tenant's parts an app uses are copied into the document, kept apart from its own, and import asks before overwriting one the target has, showing both sides' versions and dates.

### The document
`apps.Document` (`apps/Document.go`, format `forge.app/1`):
```json
{ "format": "forge.app/1", "app": "tasks", "version": 3, "publishedAt": "…", "exportedAt": "…",
  "manifest":    { "id": "tasks", "pages": […], "roles": […], "settings": {…}, … },
  "parts":       { "prefabs":  [ { "id": "myTasks", "version": 2, "publishedAt": "…", "prefab": {…} } ],
                   "graphs":   [ { "id": "cmd-tasks-task-reassign", "version": 1, "publishedAt": "…", "graph": {…} } ],
                   "entities": [ { "id": "task", "version": 1, "publishedAt": "…", "entity": {…} } ] },
  "tenantParts": { "prefabs": [ { "id": "recordTable", … } ], "graphs": […], "entities": [ { "id": "customer", … } ] },
  "appParts":    { "prefabs": [], "graphs": [], "entities": [] },
  "primitives":  { "components": [ "mui.stack", "mui.text" ], "nodes": [ "data.expression", … ], "graphs": [] },
  "seed":        { "task": [ { "id": "t1", "values": { "title": "Write docs" } } ] } }
```
- **parts** are the app's own (owner `app:<id>`), **tenantParts** the tenant's it uses (a tenant-library prefab, an entity type with owner `tenant` or none, a graph of no app), **appParts** those of the apps it requires: carried so the document checks whole, never imported.
- Each part is the version the app version pinned: prefabs and entity types from their version tables, graphs from the version's copies (dated by the app version, since graphs keep no publication times). Built-in graphs the server provides are named in `primitives.graphs`, not copied.
- **The closure**: the entity types the parts name beyond the manifest's (a relation's target, a node's `entity` config, a prefab prop's `entity` or a literal `entity` prop on a template node) are added at their last published version.
- **Seed** (with `seed: true`): the records of the app's own entity types the exporter may read, with only what a command may set (no computed or `file` fields, no removed fields, no metadata), oldest first.

### The check
`apps.CheckDocument(doc, env)` (`apps.DocumentEnv`: the server's components, node types, built-in graphs and live queries) gives every problem at a path (`parts.prefab[myTasks].template.root/note.type`, `tenantParts.graph[x].graph.nodes.2.config.entity`, `primitives.components[mui.hologram]`, `manifest.pages[/mine].prefab`), each with a fix:
- the format, and the manifest being the document's app; each part where its owner says (an own part with another owner, a tenant part owned by an app, an app part of an app not in `requires.apps`), once;
- **primitives**: every component, node type and built-in graph the document names exists here; every component a template uses and node type a graph uses is among them;
- **references**: every prefab instance, prefab graph, query, entity type (props, relations, node configs, literal `entity` props) is in the document or a primitive; `ui.CheckPrefab` on every prefab against the document alone; an instance pinning a version (`prefab.x@3`) is refused, since another tenant numbers versions anew;
- `apps.CheckManifest` against the document's parts; sample records only of the app's own entity types, with valid ids.

`apps.Used` gives the primitives a document's parts use, which the export lists.

### appservice
`apps/services/appservice` (`/apps`), following the service → handler → DTO layout:
- **`Export {id, version?, seed?}`** (`POST /apps/export`, `app.read` on the app): the document, or 422 `app_check_failed` with the check's problems; 404 `app_version_not_found`.
- **`Check {document}`** (`POST /apps/check`, `app.read`): the problems against this server.
- **`Import {document, id?, overwrite?, keep?, dryRun?}`** (`POST /apps/import`, `app.write` on the tenant):
  1. the check, against this server (422);
  2. the app's id, the document's or `id`; refused 409 `app_import_exists` (path `id`) when the tenant has an app of that id. Under a new id, the app's own parts take its owner and its `cmd-<old>-`/`qry-<old>-` graphs are renamed, with what names them;
  3. **the plan**: each part of `parts` and `tenantParts` is `new`, `same` (the tenant's published definition equals the document's, versions aside: nothing to import) or `duplicate`, with `document` and `target` sides `{owner, version, publishedAt, updatedAt, changed}`. A duplicate needs a decision, `overwrite: ["prefab:note"]` or `keep: ["prefab:note"]`; any left undecided refuses the import with 409 `app_import_duplicate`, one problem per part saying both sides (*this tenant already has the prefab note (version 1, published 2026-11-07 21:10 UTC, changed …); the document has version 1, published …*) and the plan, and nothing is changed. A required app's part the tenant lacks is refused (`app_requires`). `dryRun` returns the plan;
  4. **the commands**, sent as the importer through the bus, each waited for: `app.create` first (so the app's command and query graphs are never live alone), the entity types (`entity.import`; one relating to a type not here yet first without its relations, computed fields, indexes and rules, then whole; each published after the types it relates to), the graphs (`bp.import`, `bp.publish`), the prefabs, each after those it uses (`prefab.import`, `prefab.publish`, breaking changes allowed when overwriting), then `app.setManifest`, `app.setSeed` and `app.publish`: version 1 here, ready for `app.install`. A refused command stops it there with that command's problems (422).
- **Tools** (`apptools.ContributeTransfer`): `app.export`, `app.check` and `app.import` (write), whose description tells the model to ask the user about each duplicate with both sides' versions and dates before calling again.
- **The browser**: `Apps.ts` has the document's and plan's types and `Apps.export|check|import`, resolving 409 and 422 with their problems and plan. The import dialog is the Applications editor's (forge 5f). The dev proxy routes `/api/v1/apps`.

### New commands
- **`entity.import {entity}`** (`records/entities`, event `imported`): replaces an entity type's whole draft or makes it, fields keeping their ids, so records keep their values; its published versions stay until it is published again.
- **`app.setManifest {manifest}`** (event `manifestSet`): replaces the whole manifest, checked whole.
- **`app.setSeed {seed}`** (event `seedSet`): the draft's sample records (`Draft.Seed`), of entity types the app uses; `app.publish` copies them, as the draft was at the rev published, into `Version.Seed`.

### Sample records at install
The follower `apps.seed` (`installs/Seed.go`, started with the installs follower) creates an installed or upgraded version's sample records as the tenant's service principal (`record.create` with the record's id), into each of the app's **own** entity types that has no records but those, so installing again or upgrading creates only the missing ones. A refused record is logged and left out.

### Wiring
`serve.go`, in Foundation and in Forge: `apptools.ContributeTransfer` after `apptools.Contribute`; `appimpl.CreateService(appimpl.WithDeps(...))` with the projections, the tenant database, the bus, the subscriptions and the registry, after the installs' deps; `apphandlers` registered on `api`. `ui.WalkTemplate` walks a template's nodes, children and slots.

### Decisions
Made while building:
1. **An `appservice` after all**: export and import are a file going out and coming in, not a live read or one command, so they follow the REST layout; slice 32's decision 1 still holds for the rest.
2. **Import is the author's commands**, sent as the importer and checked by each part's own command, rather than events written behind them: access, checks and projections are those of any edit.
3. **`same` parts are not asked about**: a part published here exactly as the document has it is left alone.
4. **Keep and overwrite are per part**, named `<type>:<id>`; an own part may be kept too, and the check at publish then says whether the tenant's version fits.
5. **Graphs are dated by their app version**: graphs keep no publication time of their own.
6. **Pinned instances (`prefab.x@3`) fail the check**: version numbers differ per tenant.
7. **File field values are not exported** as sample records: the files' content would have to travel too.
8. **A partial import stays** when a command is refused midway (the app's draft and the parts so far), named by the error; importing again under the same id is then refused until the draft is removed (`app.delete` is not built).
9. **The default frame is not exported**: an app naming no frame uses the target's *App frame*.
10. **Component and node versions**: the plan's "primitives by id and version" is by id only; primitives carry no versions yet (slice 31's decision 6).

### Verification of slice 35
- `go test ./features/apps/...` (also with `-race`, three runs in a row; `installs/transfer_test.go`), against embedded NATS and SQLite: Tasks in tenant 1 with its own entity type, two prefabs and two graphs, the tenant prefab *Note* that *My tasks* renders, the tenant entity type *Customer* a task relates to, two roles, a setting and two tasks:
  - the export: own and tenant parts apart, *Customer* found through the relation, *Note*'s version and date, the primitives `mui.stack` and `mui.text` and the graphs' node types, the two tasks as seed only when asked;
  - imported into tenant 3 (every part `new`; *Task* imported first without its relation to *Customer*), installed there and in tenant 1: the same pages, nav, roles, settings, editor kinds, commands, pinned prefabs and owned types; the two tasks created at install; the app's command decided by the imported graph;
  - the round trip: exported again from tenant 3, the same document but for its dates;
  - duplicates: with tenant 3's own *Note* and the same *Customer*, the import is refused once, at `tenantParts.prefab[note]`, its message giving both versions and dates and its fix the `overwrite` and `keep` arguments; the plan has *Note* `duplicate` with the target's date, *Customer* `same`, *My tasks* `new`; nothing was made; a dry run keeping *Note* plans; overwriting imports and publishes *Note* as version 2 with the document's definition; importing again is refused at `id`; under `tasks2` its command graph is `cmd-tasks2-task-reassign`, owned by `app:tasks2`;
  - the check: an export passes its own; a missing component (`primitives.components[mui.hologram]`), a node type not listed, the tenant prefab left out, a node config naming `invoice`, another app's owner on an own part and a pinned instance are each named at their path; exporting an app whose handler graph reads a never-published entity type is refused (422) naming it.
- `go test` of `records/entities`, `ui/...`, `live/...` and `cognition/drafts` still pass, and every `app.*` command's example still fits its schema; the full backend suite (`scripts/build.sh --test ./...`) passes but for `kb/services/collabservice/impl`'s `TestRoomSeedRelaySaveHandover`, which fails every run on its temp directory's cleanup (*directory not empty*: a file still written into the volume as the test ends), in code this slice does not touch; Foundation's and Forge's `serve` vet; Forge's starter tests pass; the common frontend type-checks.
- Forge's server, run fresh and seeded with `cli seed`: `POST /apps/export {id: "records"}` gives the Records app with the starter's five prefabs and four graphs as tenant parts and no problems; `POST /apps/check` passes it; importing it again is refused at `id`; a dry run as `records2` plans every part `same`; with *Empty state* changed in the document, the import is refused with 409 `app_import_duplicate` naming both sides' versions and dates; with `keep: ["prefab:emptyState"]` it imports `records2` as version 1. The server's extensions list `app.export`, `app.check`, `app.import`, `app.setManifest` and `app.setSeed`.

### Decisions to review after seeing it
- The export document, read as a person would ★ (forge 05e).
- Decisions 3 and 4, once the Applications editor's import dialog (forge 5f) asks about duplicates; and decision 8, a partial import, once `app.delete` exists. (`app.delete` exists since slice 39, but it hides the draft and keeps its id, so importing again under that id is still refused: import under another id.)

## Built in slice 36
Forge's part 5f ([forge 05f-ApplicationsEditor.md](../../forge/documentation/plans/05f-ApplicationsEditor.md)), its Foundation side: an app's **draft run**. The Applications editor is Forge's ([forge ApplicationsEditor.md](../../forge/documentation/ApplicationsEditor.md)).

Answered before building (2026-11-07): forge iteration question 1, a draft app runs for its authors, sharing its data; forge 05f question 3, its shape: a live tab for the app's authors, the draft manifest with its prefabs and graphs in their drafts, its commands and queries sent for real on the app's records; records need published entity types, so the editor finds entity types with unpublished changes and publishes them alone, without the app.

### The draft run
- **`<appId>/draft`** (`apps/installs/Draft.go`, resolved by the `apps` app source before installed pages, `mine` when the tenant has the app's draft): the main tab as the draft has it, `mainPage` over a `manifestOf` that reads the draft manifest from the page's own `app.describe` query (`__draft`), so a page, a route, nav or the name changed in the draft renders the open session again; handlers (navigation) read the draft from the projection (`manifests.GetDraft`). Its reach is `app.write` on the app (the authors), its title `<name> (draft)`, its frame the draft's (or the published *App frame*). Nothing is installed, and the installed app, if any, is untouched.
- **`ui.Page.Drafts`**: the evaluator renders every prefab instance in its draft (`prefab.describe {id}`), and `uinodes` runs every handler and logic graph in its draft (`Handle.Drafts`, true on a dry page too), but unlike `Dry` the page's commands are sent.
- **`draft.<appId>.<name>`** (`apps.DraftScoped`, `DraftUnscoped`, `DraftPrefix`): the page maps the app's own command and query names to these (`AppCommands`, `AppQueries`), and `installs.CommandFor`/`QueryFor` resolve them before installed names: a command by the draft of the graph the draft manifest names for it, read again when it is decided; a query by its graph's draft, run again when that draft changes (`livenodes.QueryRun.Watch` on `cognition.graph`). Both need `app.write` on the app. A command the draft no longer names is not one (404).
- **What reopens a session**: its commands' and queries' names and its frame are fixed when it opens (`draftShape`); when they change, `Stale` makes the next event reopen it (`page_changed`), as an upgrade does. Every other change reaches the open session as it lands.
- `apps.CheckManifest` refuses the app id `draft`. `ui.AppPage`'s main page no longer captures one manifest: `mainPage(page, src, frame, declared)` serves installed versions (`fixed(m)`) and drafts alike; `frameOf` reads a draft frame for a draft run.

### Browser
`AppHost` names its app by its page (`<appId>/<page>`) when its kind is not `app:<appId>:<kind>`, so Forge's `forge-app-draft` tab hosts `<appId>/draft`.

### Decisions
Made while building:
1. **The draft run is a page of the `apps` source**, not an install: nothing is recorded, nothing is pinned, and the app's roles and settings section are not made. It runs as the author, so row rules of the draft's roles do not apply.
2. **Draft commands and queries need `app.write`**, in place of the manifest's entity actions, since no app role exists before an install. Their record nodes still check the author's access to the records, so an author who is only an *Editor* reaches an installed app's records only through its roles (slice 33's decision 7).
3. **The draft's records are the app's records**: the same entity types, under the app's path while it is installed; the authors share them with each other and with the installed app's users.
4. **The page follows the draft through its own `app.describe` subscription**, so a change re-renders the session; only a change to the command and query names or the frame reopens it, at its next event, since those are fixed when it opens.
5. **Editor kinds do not open their own tabs from a draft run**: their routes open inside the draft's main tab.
6. **`draft.` is a reserved prefix** rather than another separator, since live names are dotted and an app id is a token.

### Verification of slice 36
- `go test ./features/apps/installs` (`draft_test.go`, `TestDraftRun`), against embedded NATS and SQLite: the Tasks app with only its entity type published: `tasks/draft` shows *My tasks* from its draft, titled *Tasks (draft)*; a `prefab.updateNode` reaches the open page; a page added to the draft is navigated to in the same session, and a route with a param shows its prefab's draft; `draft.tasks.task.reassign` sets the record by the graph's draft, and after `bp.import` of a changed graph the next command runs the new draft; `tasks.task.reassign` is not a command (nothing installed); `draft.tasks.myTasks` answers from its draft, and from the new one after an import; a removed command answers 404; `app.create` with the id `draft` is refused at `manifest.id`.
- `go test -race` of `apps/...`, `ui/...`, `live/...`, `records/...` (but `records/search`, which needs `scripts/build.sh`) and `cognition/drafts` pass, but for one run of `ui/prefabs`' `TestAPrefabPageRendersPrimitives`, whose race is the test reading the session's tree (`ss.tree()`) while a later query patches it, in the test, not in this slice's code; it passed three runs in a row on its own. Foundation's `serve` vets; Forge's builds and its starter tests pass; the common frontend type-checks.
- In headless Chromium, with Forge's Applications editor: the draft run showed the draft of *My tasks*, and a `prefab.updateNode` sent over the socket reached the open tab (see forge [ApplicationsEditor.md](../../forge/documentation/ApplicationsEditor.md#verification)).

### Decisions to review after seeing it
- Decisions 1 and 2 (the draft run is the author's, no app roles), once the Tasks app is tried by two authors with different access (forge 5g).
- Decision 5: editor kinds inside the draft's main tab.

## Built in slice 37
Forge's part 5g ([forge 05g-TasksApp.md](../../forge/documentation/plans/05g-TasksApp.md)), the iteration's demo: the Tasks app is Forge's, a sample of its starter library ([forge TasksApp.md](../../forge/documentation/TasksApp.md)). What it needed from Foundation:

- **An imported prefab reads its app's own queries.** `prefab.import` of a new prefab owned by `app:<appId>` checked its queries against the owner of the draft before the command (none), so a page prefab naming the app's `myTasks` was refused (*there is no query "myTasks"*). The check now takes the owner from the definition the command leaves (`decision.env(owner)` in `ui/prefabs/Commands.go`).
- **Icons** `open_in_new`, `swap_horiz` and `task_alt` in the browser's icon map (`ui/renderers/mui/iconMap.ts`).

### Decisions
1. **An editor kind's subject type is how the runtime finds the tab's record** (`entityOf`, slice 34): an app whose kind names another type than its entity type's id gets a tab titled by the page's title. Forge's Tasks names `tasksTask`; nothing checks it yet.

### Verification of slice 37
- `go test ./features/apps/installs` with `TestAnImportedPrefabReadsItsAppsQuery`: a new prefab owned by `app:tasks` reading `myTasks` imports, the same with no owner is refused at `prefab.queries.mine.query`; the test fails without the fix. `go test` of `ui/...`, `apps/...` and `live/...` pass; the common frontend type-checks.
- Forge's Tasks tests and its browser run of the demo: [forge TasksApp.md › Verification](../../forge/documentation/TasksApp.md#verification).

### Decisions to review after seeing it
- Decision 1: whether `apps.CheckManifest` should refuse an editor kind whose subject type is not one of the app's entity types when its page has an `:id`.

## Built in slice 38
Forge's part 6a ([forge 06a-OperationLog.md](../../forge/documentation/plans/06a-OperationLog.md)), the first part of iteration 6: the **operation log**. Every definition command is an **op**: its first event carries its arguments, the request it belongs to, who sent it, the actor beside them and the ops that undo it. The `ops.log` projection keeps one entry per op, `ops.list` reads them by app, request and principal, and `ops.Replay` sends an app's ops into another tenant. Undo ([forge 06b](../../forge/documentation/plans/06b-UndoByRequest.md)) and the chat's view of others' changes ([forge 06e](../../forge/documentation/plans/06e-ApplyCheckPreview.md)) are built on it.

### Decisions
Answered before building, in [forge 06a-OperationLog.md](../../forge/documentation/plans/06a-OperationLog.md#todo), each as proposed:
- **Request ids are set by the client** (question 1). A command carries `request`, and one sent without one is its own request (its command id). The chat's tools set it per round, so the model never does.
- **Inverse ops, computed when the op is decided** (question 2), from the state it was decided against. Undo is an ordinary request, and other authors' later changes to other items stay.
- **Definitions only** (question 3): every command on the `def` stream (`live.Command.Logged()`). Record commands are data and are not on the log.
- **An op on a part no app owns is logged under the app its request was made for** (question 4): the command's `app`, else none.

Decided while building:
1. **The op rides on its first event** (`live.Event.Op`, a `live.LoggedOp`): `{command, target, args, app, inverse, irreversible, decided}`. The log is a projection over the `def` stream, not a stream of its own. `live.Projector.KeyOf` lets a projector key an event other than by its target, and `ops.log` keys each op `<app>.<sequence of its first event>` (`_` for no app), so an app's ops are one key pattern.
2. **One app per op.** It is the target's app as decided: the owner `app:<appId>` of a graph, prefab or entity type (as the command leaves it, else as it found it), the app itself for `app.*` and installs, and the page's app for an overlay. Only when the target has none does the client's `app` count. An app's own parts and the parts its authors change for it are then one `ops.list {app}`.
3. **How each family inverts:**
   - **Graphs** (`cognition/drafts/Inverse.go`) by the difference between the draft before and after the events, one function for every `bp.*` command: `bp.remove` for what was added, `bp.paste` for removed nodes and edges, `bp.setConfig` per changed node, `bp.moveNodes`, the variables and `bp.setInfo`. The Graph Editor's client-side `inverse` (`editing.ts`) does the same per command and can move onto the log in 6b.
   - **Prefabs** (`ui/prefabs/Inverse.go`) per command, by key path: an insert is removed; a removed node comes back with its subtree at its parent, slot and index; an update sets back only the props, bindings, handlers, key and variant it changed (`null` for those it added); a move moves back; an unwrap is wrapped again. A wrap is an unwrap, and when the wrapped siblings were not side by side, each then goes to the end of its list and back to its old index, lowest first, which no later move shifts.
   - **Entity types** (`records/entities/Inverse.go`) per command: a field update is undone by a patch that sets every property the field had and clears those it gained; a removed field is restored and a restored one removed; the title field an added field took is set back before the field is removed.
   - **Apps** (`apps/manifests/Inverse.go`) per command: a removed list item comes back at its index (`app.add<Item> {…, index}`), an update is undone by the merge patch that takes the item back (`unmerge`: keys it gained are `null`, changed ones set back object by object) with `rename` to its old key, which the home, navigation and editor kinds follow again.
   - **Page overlays** (`ui.insertNode` ↔ `ui.removeNode`).
4. **What no op undoes is marked, with why** (`irreversible`): publishing (`bp.publish`, `prefab.publish`, `entity.publish`, `app.publish`), installs (`app.install`, `app.upgrade`, `app.uninstall`), `bp.delete`, and making a prefab or app (`prefab.create`, a new `prefab.import`, `app.create`), which have no delete until 6b's restorable deletes. A new graph is undone by `bp.delete` and a new entity type by `entity.delete` (hidden, its records kept), as is the tenant's first change to an editable built-in graph.
5. **Undoing a new field hides it**, as `entity.removeField` does: fields are never deleted, so the type shows as it did, with a hidden field more.
6. **Redo is undoing the undo**: the undo's own ops are logged with their inverses, which give the change again (`entity.delete` after `entity.create` is undone by `entity.restore`). 6b needs nothing more to redo.
7. **The chat's request and actor.** A command tool's command is logged under `round-<roundId>` with the actor `chat`, or `run-<runId>` with `agent` in a Cognition run, else the tool call alone. Forge 6g may name the actor the Builder.
8. **`ops.list`** (`app.read` on the app, or on the tenant's apps without one) gives `{requests: [{id, app, principal, actor, seq, time, ops: [entry]}]}`, newest request first; a new op is a list change on `requests`: `insert` at 0 for a new request, `move` to 0 with the request's new value for one that grew. Each op is also checked on its own: an app's needs `app.read` on that app, a library part's the part's read action (`cognition.read`, `prefabs.read`, `records.read`).
9. **Replay is a Go function**, `ops.Replay(from, store, app, into, bus)`, for tests rather than a tool. Each op goes into the other tenant under its own command type, target and args, in the request `replay-<request>`. Before an op that reads other parts' published versions (`app.publish`, `app.install`, `app.upgrade`) it waits until every definition projection has folded the stream, since those were changed by the op just before.
10. **A prefab command reads other parts through its decision** (`Decision.Read`), which waits for this process's last commit on them. The cycle check read another prefab's draft straight from the store and, under the log's extra projector, sometimes saw it a commit behind.
11. **The bus refuses an `app` that is not an id**, and the ack says which request the command was logged under (`request`).

### Backend
- `live`: `Op`, `OpInfo`, `LoggedOp`, `Decision.Op` with `Decision.Undo(app, ops…)` and `Decision.NoUndo(app, why)`, `OpOf`, `Command.Logged`, `Event.Actor`, `Event.Op`, `Projector.KeyOf` (honoured by `live/project`'s fold).
- `live/bus`: `Request.App`, `Request.Actor`, `RequestID` defaulting to the command id, `Result.RequestID`; a logged command's decision gets an `OpInfo`, and `commit` puts the op on the first event.
- `live/services/liveservice`: the command frame's `app` and `actor`, the ack's `request`. `live/tools`: `requestOf`, `actorOf`.
- Each family's `undo`, called by its `Decide` after the command's events are checked: `cognition/drafts/Inverse.go`, `ui/prefabs/Inverse.go`, `records/entities/Inverse.go`, `apps/manifests/Inverse.go`, `apps/installs` (all irreversible), `ui/Overlay.go`.
- **`apps/ops`** (`ops.Contribute`, wired in `serve.go` after `installs.Contribute`): the `ops.log` projector, `ops.Entry`, `ops.Request`, `ops.Log`, the `ops.list` query, `ops.Entries` and `ops.Replay`.

### Browser
- `live.command(command, target, args, {request?, app?})` (`CommandOptions`); the ack carries `request`.
- `OpEntry`, `OpRequest` and `OpLog` in `src/services/Apps.ts`, for `useQuery<OpLog>('ops.list', params)`.
- Forge's Applications editor sends its commands with `{app}`, and Forge's starter seed sends a whole seed run as one request, `seed-<run>`.

### Verification of slice 38
- `go test ./features/apps/ops` (`ops_test.go`):
  - **`TestEveryOpUndoes`**: 93 steps run every logged command against the states the steps before it left (the Tasks parts and manifest, then entity types, graphs, prefabs, apps and an overlay). For each, the log's last op is the step's; its inverse, sent, restores the target's definition exactly (leaving out revs, times, authors and the order of a graph's nodes, edges and variables and of an entity type's indexes); undoing the undo gives the step's state again. Irreversible ops must be the listed ones, and every logged command must be run by a step.
  - **`TestTheLogByAppRequestAndPrincipal`**: the Tasks parts (request `parts`, Ada) and manifest (request `manifest`, Bob) list by app newest first, by request and by principal; the tenant library's *Page header*, made for Tasks, is in Tasks' log, and CRM's prefab only in CRM's. A third request, sent as the chat would (`round-7`, actor `chat`), arrives at a subscriber as an `insert` and then a `move`; a command without a request is its own.
  - **`TestReplayRebuildsAnApp`**: the published Tasks app replayed into tenant 3 gives the same drafts of its manifest and parts.
  - Five runs in a row pass.
- Forge's `TestTasksAppReplay` (`features/starter/tasks_test.go`): the Tasks sample's 41 ops, seeded in one request, replayed into a tenant that has only the starter library, give an export identical to the original's (38 KB) but for who published it and when.
- `backend/scripts/build.sh --test ./features/...` passes but for `kb/services/collabservice/impl`'s `TestRoomSeedRelaySaveHandover`, which failed once under the full run's load, passes alone and does not use the live core. Forge's tests pass, and both `serve` binaries build. `common`'s `yarn build`, and `frontend/example`'s and Forge Axiom's `yarn build && yarn lint`, pass.
- Against Forge's `serve` over the live socket: `app.create` in request `gesture-1`, then a library prefab and `app.setInfo` in `round-9` with `app: contacts` and actor `chat`, arrive in `ops.list {app: contacts}` as an `insert` per request and a `move` as `round-9` grows, the prefab under Contacts, `app.setInfo`'s inverse `{icon: ""}`; each ack names its request, and `app: "not valid!"` is refused.

### Decisions to review after seeing it
- One app per op (decision 2), and the client's `app` counting only for parts no app owns.
- `ops.list` reads an app's whole log, or the tenant's, and filters by request and principal: fine at today's sizes; an index by request or principal can come when logs grow.
- The actors `chat` and `agent` (decision 7).
- Undoing a new field hides it (decision 5); making a prefab or app is irreversible until 6b.
- Graphs inverted by difference (decision 3), prefabs, entity types and apps per command.

## Built in slice 39
Forge's part 6b ([forge 06b-UndoByRequest.md](../../forge/documentation/plans/06b-UndoByRequest.md)): **undo and redo by request**. `ops.undo` sends a request's inverses, newest op first, as one new request committed atomically; `ops.redo` undoes that undo. The caller's stacks in a scope are the live `ops.stack` query, so an editor's undo survives a reload and is shared by the user's windows, and undoing one's own request leaves what others changed meanwhile. Prefabs, graphs and apps got restorable deletes, so making a part is undone by deleting it. The browser has `live.gesture`, `useUndo(scope)` and the Undo and Redo buttons, which the Cognition Editor and Forge's editors use, and a chat round that changed definitions has *Undo changes* and *Redo changes*, previewed first.

### Decisions
Answered before building, in [forge 06b-UndoByRequest.md](../../forge/documentation/plans/06b-UndoByRequest.md#todo), each as proposed: items as what an op addresses (question 1), a conflict refusing the undo unless forced (2), any request in the log with no limit (3), the editor's scope including the chat's requests for the caller (4), the stacks on the server per user and scope (5), undo after publishing changing the draft only (6), definitions only (7), with the user's addition of Undo and Redo on a chat round with a preview (8).

Decided while building:
1. **An undo is one transaction** (`bus.Transact(r, ops, dryRun)`): each inverse is decided against the state the ones before it left, though nothing is committed yet. The bus keeps an overlay of the area's projections: after each op its events are folded by every projector of the area (but those with `KeyOf`) over the overlay's value or the projected one, and the next op's `State` and `Decision.Read` read the overlay first. All the events then commit as one atomic batch, each target's first event expecting the sequence it was read at; a conflict decides the whole again. Each op's first event carries its `LoggedOp`, with `Events` (how many events it committed) so the log gives each op of the batch its own sequence. With `dryRun` nothing commits, and a refusal comes back with the op's index in its problems' paths (`ops.<i>`).
2. **Undoes.** An undo's ops carry `undoes: <request>` (`LoggedOp.Undoes`, `bus.Request.Undoes`, `Entry.Undoes`); its request is `undo-<tsid>`, a redo's `redo-<tsid>`. Redo is undoing the undo (slice 38 decision 6), and undoing a redo undoes the request again.
3. **The stacks** (`ops.StackOf`) walk the caller's requests in the scope, oldest first: a request is pushed on the undo list and empties the redo list; an undo moves what it undid to the redo list (as the undo); undoing an undo (a redo) moves it back; an undo of a request on neither (another author's, named) counts as a new request. A scope is `app:<id>` (every op logged under the app), `app:<id>/manifest`, `prefab:<id>`, `graph:<id>` or `entity:<id>`; a request is in it when one of its ops is, so a gesture that made a prefab and its page is in both stacks, and one Undo takes back the whole request. Each item has a summary, the first op's command title, target and item key and how many more (`Add a page (tasks /mine) and 1 more change`); a redo's names what it brings back.
4. **Items and conflicts** (`ops.Items`, `Item.Overlaps`): an op's items are read from its arguments and its inverse's, which name what it made (a new node's key path, a new field's id). A node holds its subtree; an op that makes, imports, replaces, deletes or restores a target holds the whole target; publishing has none. A later op by another request on an overlapping item is a conflict, except requests cancelled out (an undo and what it undid, while the undo stands), which are no one's change. Conflicts are per op: forced, an op with a conflicting item is skipped whole, and the rest undone. An edge does not hold its nodes, and a node whose key path a wrap or rename changed is not followed (forge 06b TODO).
5. **Irreversible ops are skipped and named** (`skipped`, with why): publishing and installs. Undoing after publishing changes the draft; the published version stays.
6. **Restorable deletes.** `bp.delete` keeps deleting the draft and published version (so nothing reads a deleted graph), but its event now carries both, which the `cognition.trash` projection keeps until `bp.restore` brings them back. Prefabs and apps are hidden instead (`Draft.deleted`, on `prefab.describe`/`prefab.list` and `app.describe`/`app.list`), refusing every command but their restore: `prefab.delete` is refused while a published prefab (its last version) or an app's last published version uses the prefab, naming them, and `app.delete` while the app is installed. A hidden prefab's published versions keep rendering where pinned. The inverses: `prefab.create` and a new `prefab.import` are undone by `prefab.delete`, `app.create` by `app.delete`, and each delete and restore by the other. Only publishing and installs are irreversible now.
7. **An app reads its parts through the decision** (`manifests` `env`), as prefabs have since slice 38, so `app.addPage` right after `prefab.create` in one gesture finds the prefab, and an undo's `app.removePage` sees the prefab its earlier op restored. It was a race before (`TestUndoCreation` found it).
8. **`ops.undo` and `ops.redo` are calls and tools** (`ops.ContributeUndo`, wired in `serve.go` after `ops.Contribute` with `ops.Deps{Bus, Store}`): `{scope?, request?, force?, dryRun?}`, ending with one `result` event, an `UndoResult` `{status, request, undoes, app, summary, ops, changes, conflicts, skipped, sent, seq}` (`changes`: each op taken back in words, which the previews list), status `done`, `preview`, `conflicts` (refused; its `ops` are empty, and a forced dry run gives what the rest would send) or `nothing`. A redo may name an undo or the request it undid. Before planning, the log is settled (`ops.Entries`), so the caller's commands acknowledged just before are in it. Undoing another author's request needs only naming it: each inverse is checked against its own command's action. The tools' actor is `chat`.
9. **`ops.status {request}`** gives `{request, ops, undoable, undone, undoneBy}`, for the chat's Undo and Redo on a round (`round-<roundId>`).
10. **Gestures in the browser.** `live.gesture(fn)` gives the commands `fn` sends before it returns one request (`g-…`); a gesture whose commands are sent after awaits (make a prefab, then name it in a page) passes its request in each command's options (`newId('g-')`). `live.settled()` resolves once every command in flight is acknowledged, and `useUndo` waits for it before undoing.

### Backend
- `live`: `LoggedOp.Undoes` and `LoggedOp.Events`; `live/bus`: `Request.Undoes`, `Transact` and `TransactResult`.
- `apps/ops`: `Entry.Undoes`, per-op sequences in a batch; `Items.go` (`Item`, `Items`); `Undo.go` (`Scope`, `ParseScope`, `StackOf`, `Requests`, `PlanUndo`, `Undo`, `StatusOf`, the `ops.stack` and `ops.status` queries, the `ops.undo` and `ops.redo` calls and tools, `ContributeUndo`, `Deps`).
- `cognition/drafts`: `bp.restore`, `Trash`, `TrashProjector` (`cognition.trash`), the `restored` event; `ui/prefabs`: `prefab.delete`, `prefab.restore`, `Draft.Deleted`; `apps/manifests`: `app.delete`, `app.restore`, `Draft.Deleted`, parts read through the decision.

### Browser
- `live.gesture`, `live.settled`, `CommandOptions` on `useCommand`'s `send`; `useUndo(scope)`, `undoKeys(state)` (Ctrl+Z, Ctrl+Shift+Z, Ctrl+Y, leaving text fields, Monaco and CodeMirror alone), `runUndo` and `withRest` in `src/live/useUndo.ts`.
- `src/components/undo/UndoButtons.tsx`: `UndoButtons` (tooltips with the summaries, the conflict dialog with *Undo the rest*, a failed undo's message), `UndoPlan`, `useUserNames`.
- The Cognition Editor's `useDraft`: its client-side stack (`inverse`) is gone; a gesture is `live.gesture`, and Undo and Redo are `useUndo('graph:<id>')` in the toolbar.
- The chat: `components/chat/RoundUndo.tsx` on a finished round that ran a write tool, by `ops.status`.
- Types in `src/services/Apps.ts`: `UndoStack`, `UndoItem`, `UndoArgs`, `UndoResult`, `UndoConflict`, `RequestStatus`; `deleted` on `PrefabView`, `PrefabSummary`, `AppView` and `AppSummary`.

### Verification of slice 39
- `go test ./features/apps/ops` (`undo_test.go`, beside slice 38's tests); six runs in a row pass:
  - **`TestUndoTwiceRedoOnce`**: the Tasks manifest in three requests, undone twice and redone once, equals the draft the second request left; the stacks follow (`m1` under the redo on the undo list, one item on the redo list); undoing the redo undoes `m2` again, and a new request empties the redo list.
  - **`TestUndoLeavesAnotherAuthorsChange`**: Ada's page and role go, Bob's field rename stays; Bob's stack holds only his request.
  - **`TestUndoConflict`**: Bob's change to Ada's role refuses her undo, naming the role, Bob and his request; the forced dry run changes nothing; forced, the page goes and the role stays as Bob left it.
  - **`TestUndoAfterPublishing`**: the draft's edit is undone, `prefab.publish` named as skipped, and the published version keeps the edit.
  - **`TestUndoCreation`**: undoing the making of an app, its prefab and page hides the app and the prefab; redo brings them back with the page; undoing the making of a prefab a published prefab uses is refused naming it, and changes nothing.
  - **`TestUndoScopes`**: a request on a prefab and the manifest is in both stacks and in the app's; after its undo, a new request on the prefab empties the prefab's redo list only.
  - **`TestStackQuery`**: `ops.stack` sends the new request, then the undo's move to the redo list, with their summaries. **`TestStatusOfARound`**: `round-7`'s status and preview (its change named, the publish skipped), undone by its undo and redone by naming the round.
  - **`TestEveryOpUndoes`** now runs `bp.restore`, `prefab.delete`, `prefab.restore`, `app.delete` and `app.restore`, and undoes and redoes `bp.delete`, `prefab.create`, a new `prefab.import` and `app.create`; only publishing and installs are irreversible.
- `backend/scripts/build.sh --test ./features/...` passes but for `kb/services/collabservice/impl`'s `TestRoomSeedRelaySaveHandover`, which fails on its temp folder's cleanup (a file still written as it ends), alone too; it depends on nothing this slice changed. In a last run of the touched packages, `apps/installs`' `TestCheckNamesWhatIsOutside` (an export and its check, whose fixture adds pages right after making their prefabs, which decision 7 changed) failed once, its message not kept; it passed in 18 runs since, alone, in its package and beside other packages' tests. Forge's tests pass, and both `serve` binaries build. `common`'s `yarn build`, and `frontend/example`'s and Forge Axiom's `yarn build && yarn lint`, pass.
- In headless Chromium against Forge's `serve` (fresh data, the starter library seeded), Ada (*Owner*) and Bob (*Editor*), 22 checks, no console errors; the editors' are in Forge's [PrefabEditor.md](../../forge/documentation/PrefabEditor.md#built-in-slice-39), [BlueprintEditor.md](../../forge/documentation/BlueprintEditor.md#built-in-slice-39), [ApplicationsEditor.md](../../forge/documentation/ApplicationsEditor.md#built-in-slice-39) and [DataDesigner.md](../../forge/documentation/DataDesigner.md#built-in-slice-39). The chat round ran against a local stand-in for the Gemini API (`GOOGLE_GEMINI_BASE_URL`) that calls `prefab.setInfo` once: after *Approve*, *Undo changes* previewed *It takes back: Describe a prefab (undoDemo description)* without changing anything, then took the description back; *Redo changes* brought it back; then Ctrl+Z in the Prefab Editor undid the chat's request.

### Decisions to review after seeing it
- Conflicts per op, and forcing skipping a conflicting op whole (decision 4).
- A deleted app's parts hidden by Forge's views only, and deleted prefabs and apps refusing every command but restore (decision 6).
- `ops.stack` and `ops.status` reading the tenant's whole log on every change: fine at today's sizes, as `ops.list` (slice 38).
- Undoing another author's request checked only by each inverse's own action (decision 8).
- The summaries' wording (decision 3).

## Changed with forge 6c
Forge's part 6c, the editors' **Activity** tabs ([forge Activity.md](../../forge/documentation/Activity.md)), is built in Forge; it needed two additions to `ops.list`:
- **`scope`**: an undo scope (`app:<id>`, `app:<id>/manifest`, `prefab:<id>`, `graph:<id>`, `entity:<id>`, read by `ParseScope`) keeps the ops in it (`Scope.Has`), as `app` keeps an app's. A request appears with only its ops in the scope. `app:<id>` and `app:<id>/manifest` are checked as `app.read` on the app, as `app` is.
- **Summaries**: each request and op in the result has `summary`, in the words of the undo stacks (`describe`, `summarize`: `Add a page (tasks /mine) and 1 more change`). They are made when the query groups the ops, not kept in the `ops.log` projection; `storeTitles` names commands by the store's registry. A scoped request's summary counts only its ops in the scope.

In the browser, `OpEntry.summary` and `OpRequest.summary` in `src/services/Apps.ts`, and `UndoConflictDialog` takes any `{conflict, dismiss, force, busy}` (an Activity tab's *Undo request* as well as `useUndo`'s).

Verified by `go test ./features/apps/ops` (**`TestTheLogByScope`**: one prefab's ops, the manifest's and the app's, with their summaries) and Forge's walk in [Activity.md](../../forge/documentation/Activity.md#verification).

## Built in slice 40
Forge's part 6d ([forge 06d-CatalogMetadata.md](../../forge/documentation/plans/06d-CatalogMetadata.md)): **the catalog, metadata and the read tools**. Every catalog item (UI primitive, node type, field type, prefab) carries a title, a description for a model, examples as JSON and tags; every command documents each of its fields and has an example its schema accepts; an app's pages gain a description, and `app.describe` gives each part's. Four read tools give a chat one view of what it builds with and of an app: `forge.catalog`, `forge.describe`, `forge.app` and `forge.read`.

### Decisions
Answered before building, in [forge 06d-CatalogMetadata.md](../../forge/documentation/plans/06d-CatalogMetadata.md#todo): the `forge.*` tools sit **beside** the existing read tools, for every chat (question 1, the user's choice over the proposal of giving the Builder only the `forge.*` ones); part descriptions are optional, `forge.check` (6e) to warn of a missing one (2); the `connectors` area is accepted and empty (3); node types keep their prose example and gain a JSON one derived from their config schema's defaults, hand-written where a config has required fields, and tags derived from their category, source and kinds (4, a gap found while reading the plan).

Decided while building:
1. **Refs.** A catalog item is named `component:<id>`, `node:<type>`, `field:<type>` or `prefab:<id>` (`component:prefab.<id>` also names the prefab); a part `prefab:<id>`, `graph:<id>` or `entity:<id>`. `forge.catalog` gives each item's ref, and `forge.app`'s parts name theirs by type and id.
2. **The catalog's prefabs are the published ones** (not deleted), the starter library's among them, as `ui.listComponents` lists them: only a published prefab can be used in a tree. An app's drafts are read through `forge.app` and `forge.read`. `forge.describe` of a prefab describes its last published version (its draft if never published), as a component (`ui.PrefabComponent`: props schema, events, slots, examples from its preview data and variants) with its variants, owner and version.
3. **Search** keeps the items holding every word (in ref, title, category, description or tags), scored two per word in the title and one per word in the tags, then by area and ref; items carry their description's first sentence. `area` is optional.
4. **`forge.app`** is `app.describe` with the manifest's problems (`apps.CheckManifest` against `manifests.Env`, so the tool takes the projections, `ForgeDeps.Store`), the other users present in `app.<id>` (the caller's own connections left out) and the position (stream → sequence) the read was at. Parts carry their `description` (`manifests.Part.Description`).
5. **`forge.read`** reads a part's draft with its rev and position: `prefab.describe`, `cognition.graph` or `entity.describe`.
6. **Prefab tags** are a field of the definition (`ui.Prefab.Tags`), set by `prefab.create` and `prefab.setInfo` (undone with the rest of the info), listed and searched by `prefab.list`, and added to the owner in the component's tags.
7. **Field types' examples** are fields as `entity.addField` takes them; a formula's names `quantity` and `price` and a rollup's `lines`, fields of the entity type it would be in (the test puts them there).
8. **A field's description is checked one level down**: every top-level property of every non-internal command's args. Shared schema pieces given a description in place use `described()` (`cognition/drafts`); an app list's remove command takes its key's schema from the list's props (`List.keyProp`).

### Backend
- `apps/tools/forge.go`: `ContributeForge(reg, func() ForgeDeps)` (`forge.catalog`, `forge.describe`, `forge.app`, `forge.read`), `ForgeDeps{Subscriptions, Store}`, `Item`, `Areas`; wired in `serve.go` after `apptools.ContributeTransfer`, its deps set with the live tools'.
- `apps`: `Page.Description`; `apps/manifests`: the page's `description` in the list props, every list field described, `Part.Description`.
- `cognition`: `NodeType.Examples` and `Tags`, `ExamplesOf`, `TagsOf`, `ConfigExample`, `ConfigDefaults`, `ConfigRequired` (`Meta.go`); `bp.listNodeTypes` searches tags and `bp.describeNodeType` gives `examples` and `tags`.
- `records`: `FieldType.Title`, `Tags` and `Examples`.
- `ui`: `Prefab.Tags`; every primitive tagged; `prefab.list`'s summaries carry tags.
- Every command's fields described, and `hello.connect|disconnect|moveNode` given examples.

### Browser
- Types only: `tags` on `Prefab` and `PrefabSummary` (`src/services/Prefabs.ts`), `description` on `AppPage` and `AppPart` (`src/services/Apps.ts`).

### Verification of slice 40
- `go test ./features/apps/tools` (`metadata_test.go`, over a registry with every feature's contributions): **`TestMetadata`**: every component has a title, description, category, tags and example; every node type passes `MetaProblems`, has tags, and its JSON example is `{type, config}` with each required config field and a config its pins accept; every command describes each field and has an example `tools.CheckArgs` accepts. It found 26 untagged primitives, 123 undocumented fields, three commands without examples and 18 node types needing examples before passing. **`TestCatalog`**: the areas, the empty `connectors`, ranking (`money` finds the field type first), describing a component, node type and field type, and refusing unknown refs and areas.
- `go test ./features/records` (**`TestFieldTypesMetadata`**: every field type's example passes `records.Check` in an entity type), and `go test ./features/apps/ops` (`TestEveryOpUndoes` now sets and undoes a prefab's tags).
- Forge's `go test ./features/starter` (**`TestForgeTools`**, over the seeded starter library and Tasks sample): `forge.catalog {area: "prefabs", search: "table"}` finds *Record table* first; `forge.describe` gives its props and an example that `ui.Preview` renders without problems; `forge.app` on Tasks gives three pages, its parts with descriptions, no problems, Alice (not the caller) as present, and its position; `forge.read` reads `prefab:taskCard`, `graph:cmd-tasks-task-reassign` and `entity:tasksTask` with their revs and positions.
- The touched packages' tests pass (`cognition/...`, `records`, `records/entities`, `records/nodes`, `ui/...`, `apps/...`, `live/...`), as do all of Forge's `starter` tests; both `serve` binaries build with `backend/scripts/build.sh`, and `common`'s `yarn build` passes. No browser walk: the slice adds no UI.

### Decisions to review after seeing it
- Search's scoring and first-sentence descriptions (decision 3), once a model has used them.
- Only published prefabs in the catalog (decision 2).
- The derived node tags (category, source, kinds), which are coarse; hand-written `Tags` can be added per type.
- Whether the general chat should keep both sets of read tools (question 1), once 6g's Builder mode exists.

## Built in slice 41
Forge's part 6e ([forge 06e-ApplyCheckPreview.md](../../forge/documentation/plans/06e-ApplyCheckPreview.md)): **applying, checking and previewing**. The chat changes an app by batches of ops committed as one, hears what other authors did since it read, checks the draft as it would be exported, previews a page as one of the app's roles, and undoes its own request: `forge.apply`, `forge.changes`, `forge.check`, `forge.preview` and `forge.undo`.

### Decisions
Answered before building (2026-11-08), each on its proposal: forge 06e question 1, the batch is **`bus.Transact`** (built for undo in slice 39), not a new `bus.SendBatch`, refused past `project.MaxBatch` with a fix to split it; 2, a batch over a stale view is **applied**, each op decided against the current state, and others' changes since `after` are reported beside its results; 3, `as` names a **manifest role**, whose actions and row rules are applied to the previewing user, on the draft run's records, else the page's preview data; forge iteration question 2, **drafts change without asking**: `forge.apply` asks only for a batch that publishes, installs or writes records, and `forge.undo` never asks.

Decided while building:
1. **New targets in a transaction.** `bus.Transact` gives an op with no target, of a command that makes its own (`NewTarget`, `NewTargetFor`), a new id, as `Execute` does, and its `Decided.Op` names it; so a batch can `prefab.create {name}` and then edit `contactList`. A batch past `MaxBatch` is refused with the code `batch_too_large`, for undo too.
2. **A tool decides per call whether it asks**: `tools.Tool.Asks func(args) bool`, read through `Tool.AsksFor`, in place of its effect's; `forge.apply` stays `write` (so its calls still run one at a time) and asks when any op is a `publish`, `app.install|upgrade|uninstall`, or in an area other than `def`. A batch is in one area, so record writes go in a batch of their own.
3. **A batch is logged under the round's request** (`round-<id>`, actor `chat`, the app named), as each command tool's is, so the round's *Undo changes* and `forge.undo` take back every batch of a round together. `livetools.RequestOf` and `ActorOf` are exported for it.
4. **A refused batch** returns its problems (paths `ops.<index>.…`, 0-based) as the tool's content and an error naming `ops.<i> (<command>, the op at index <i>)`, the message and the fix, ending *Nothing was committed*. An applied one returns `{status, request, seq, results: [{index, command, target, events}], problems}`: the app's manifest problems once every definition projection has folded the batch.
5. **`after` is the app's last op.** `forge.app`'s position counts only what its query read (a publish or an install after the draft's last change is past it), so `forge.app` also gives `after`, the sequence of the app's last op in the log, for `forge.apply` and `forge.changes`.
6. **Others' changes** are the app's ops after `after` (`ops.list {app, after}`, so read access applies) that are not the calling round's, oldest first: `{seq, request, command, target, summary, principal, actor, you}`, where `you` marks the caller's own ops from another round or by hand.
7. **`forge.check` checks a document made of the draft**: `manifests.DraftDocument` builds what `app.export` would write were the draft published (the draft manifest; the drafts, else last published versions, of every part it names or owns and every part those use, grouped by owner; built-in graphs as primitives; the primitives used), so `apps.CheckDocument` runs validation and the blueprints-only check unchanged. Its **errors** are those; its **warnings** (`manifests.Lint`, code `warning`) are parts of the app nothing reaches (from the manifest's names and triggers, through the parts that name them by id, `prefab.<id>` for prefabs), pages nothing reaches (not home, in the nav or an editor kind, and no reached part names the route up to its first param), and parts and pages with no description. `apps.DocumentEnvOf(reg)` is the check's environment, shared with `appservice`.
8. **`forge.preview` opens the draft run** (`<appId>/draft`, from the `apps` app source) at the route in a throwaway session (`ui.Open`, no store, no commander, Go handlers only) and keeps the page's subtree (`page_<route>`) without the frame. It needs `app.write`, as the draft run does. When none of the manifest's entity types is published, there can be no records, and it renders the page prefab's draft with its preview data (`ui.Preview`) instead; `data` says which.
9. **Previewing as a role is an authz override in the context**: `authz.WithAs(ctx, authz.As{Name, Covers, Rules})` decides what the role covers in `authz.Check` and `authz.RowRules`, for user principals, and leaves every other check to the user's grants. `apptools.AsRole(manifest, role, owned)` covers `record.<action>` on the app's own entity types: allowed when the role gives it, with the role's row rule. So the author's own access still opens the page and reads definitions, and only the records are the role's; `principal.user` stays the author's id, so a *Member* preview shows the author's own rows.
10. **The summarized tree** (`apptools.Summarize`) keeps each node's type, key, props, the event names it sends, the bindings left for the browser, children and slots; it drops styling props (`sx`, `spacing`, `variant`, `icon`, …), records' bookkeeping (every `_` field but `_id` and `_title`) and lists past 20 items.
11. **`forge.undo`** undoes the request named, else the newest request in the caller's `ops.stack` on `app:<appId>` whose actor is `chat`, through `ops.Undo`; a conflict is reported, and `force` is left to `ops.undo`, after asking the user.
12. **Fixes for a model**: a command naming a field an entity type does not have now says which it has, or that the field named so has another id (`fieldsFix` in `records/entities`).
13. **An entity type's relation targets are read through the decision**, so a batch may make a type and relate another to it (`known` in `records/entities` reads with `Decision.Read` when it has one).

### Backend
- `live/bus/transact.go`: new targets, `batch_too_large`. `tools`: `Tool.Asks`, `Tool.AsksFor`, used by the loop's gate. `live/tools`: `RequestOf`, `ActorOf`.
- `authz/As.go`: `As`, `WithAs`, consulted by `Check` and `RowRules`.
- `apps/DocumentEnv.go`: `DocumentEnvOf`. `apps/manifests/Check.go`: `DraftDocument`, `CheckDraft`, `Lint`.
- `apps/tools/apply.go`: `ContributeApply(reg, func() ForgeDeps)` (`forge.apply`, `forge.changes`, `forge.check`, `forge.preview`, `forge.undo`), `Change`, `OpResult`, `AsRole`, `Summarize`; `ForgeDeps` gains `Bus` and its `Store` is an `ops.Store`; `forge.app` gives `after`. Wired in `serve.go` after `ContributeForge`, with `Bus: liveBus`.

### Verification of slice 41
- Forge's `go test ./features/starter` (`apply_test.go`, over the seeded starter library and the Tasks sample): **`TestForgeApply`**: which batches ask; one batch (an entity type with a field added, `app.addEntity`, `prefab.create` with no target, a *Record table* inserted in it, `app.addPage`, `app.setNav`) commits with the entity type's, the prefab's and the app's projections all at sequences inside the batch's range and all seven ops logged in `round-7` as the chat; a second batch makes *Company* and relates *Contact* to it; a batch whose third op names a field `phone` on a type with `mobile` commits nothing (its first op's type does not exist) and names `ops.2` with the fix naming `"mobile"`; `forge.check` has no errors and warns of an unused prefab and of missing descriptions, not of the used prefab or the page in the nav, and of a page added outside the nav; with no entity type published, `forge.preview` uses preview data; `forge.undo` from the next round undoes `round-7` whole. **`TestForgeApplyAfterAnotherAuthor`**: Bob renames a Tasks field after the chat's `forge.app`; the chat's `forge.apply` with `after` lists exactly his rename, by him; `forge.changes` the same, and from the next round the chat's own op too, marked `you`. **`TestForgePreview`** (with access control, Tasks installed, a task for the owner and one for Bob): *All tasks* as *Member* shows only the owner's, as *Manager* and as the owner both, *My tasks* as *Member* the owner's; no `sx`, `_seq` or frame in the tree, no problems; an unknown route or role is refused naming the app's. `TestForgeTools` still passes. Also with `-race`.
- `go test ./features/authz` (**`TestAs`**: covered and allowed, covered and refused, not covered, the role's rule and none, a service principal untouched).
- The touched packages' tests pass (`live/...`, `apps/...`, `records/...` but `records/search`, which needs `scripts/build.sh`, `tools/...`, `authz`, `ui/...`, `cognition/drafts`; `live/bus`, `authz` and `apps/ops` with `-race`), and all of Forge's; both `serve` commands vet. `records/data`'s `TestFormulasAndMetadataFields` failed twice when many packages ran at once (the republished formula's recompute missing its five-second deadline) and passed five runs in a row alone; its type has no relations, so this slice's change does not reach it. No browser walk: the slice adds no UI; 6g's Builder mode is where it shows.

### Decisions to review after seeing it
- Drafts applied without asking (forge iteration question 2), once the Builder runs a real build.
- The preview's user (decision 9): a *Member* preview shows the author's own rows; a chosen user may be wanted.
- The warnings' heuristics (decision 7): reachability by ids named in parts' JSON, and a page reached by its route's text.
- What the summarized tree keeps (decision 10), once a model reads it.

## Built in slice 42
Forge's part 6f ([forge 06f-SandboxTests.md](../../forge/documentation/plans/06f-SandboxTests.md)): **testing blueprints in a sandbox**. `forge.test {graph, inputs?, event?, as?}` runs a graph's draft once against the app's records with the run's own writes over them, and says what it would do: its outputs, the events its commands would commit, each command it decided with its problems, a trace of the nodes run, and the node it failed on with its inputs. Nothing reaches the stream or the projections, and the sandbox is dropped when the run ends.

### Decisions
Answered before building (2026-11-08), each on its proposal: forge 06f question 1, the sandbox **starts from the app's records** (the draft run's, which are the app's real records), and `inputs.records: [{entity, id?, values}]` seeds more for the run; 2, nodes with an **external effect are stubbed**: they give a sample output and are listed in the trace.

Decided while building:
1. **The sandbox is a dry run of the bus** (`live/bus/dry.go`). `bus.Overlay` holds projected values as events that are not committed yet leave them, by projector and key, folding each event through the projectors that keep its target (those keyed by target; their `Keep` is not run). `bus.Dry` decides commands as `Execute` does (resolution, authz, the args' schema, `Decide` with `Decision.Read`) against the projections with the overlay over them, folds each command's events into it, and keeps the events and a `DryCommand` per command (`{command, target, args, events, problems, error}`) in memory. A dry run waits for nothing, so it reads each target as projected now. Its writes have `_seq` 0, since no sequence is ever given to them.
2. **The context carries the dry run** (`bus.WithDry`, `bus.DryOf`). *Send command* sends to it (`busSender`), `uinodes.TestHost.Command` decides in it (so a handler continues by *OK* or *Rejected* as it would for real), *Read projection* reads through it, and the record nodes use `data.Deps.For(args)`, which swaps the projections for the **records' sandbox** (`records/data/Sandbox.go`: `Get`, `Watch` and `WaitFor` through the dry run). The sandbox's writes are not in the indexes, so its queries scan, up to the `records` section's `scanLimit`. *Run query*, live queries through the subscriptions, *Search records*, and the followers (rollups, recomputes, the search index) still read the real data.
3. **A command graph reads its own writes, for real too.** `livenodes.DecideObserved` wraps `Decision.Read` so it reads through an overlay of the events the graph's nodes have decided so far: a graph that creates a record and then updates it reads the record as created. The bus now checks a target's expected sequence on its **first event in a batch only**, so a later event on the same target is no longer refused as a conflict. Before this slice, such a graph could not be committed.
4. **External nodes.** `cognition.NodeType.External` marks a node that reaches outside the tenant's data, and `Sample` holds the outputs its stub gives. `engine.RunArgs.Stub` runs External nodes as stubs that give their sample (each data output left out gives nothing) and leave by their first exec output, including in the graphs a run calls. `Result.Stubbed` lists them. Marked so far: *Agent* (`llm.agent`), *Match context* and *Update memory* (model calls), and *Upload file* (the object store; its sample is a file reference with id `sample`, which a record command then refuses as not uploaded). Mail and webhooks get the mark when iteration 7 adds them.
5. **Each kind runs as its runtime runs it.**
   - `command`: `livenodes.CommandOf` builds the command from the draft's spec, decided by the draft itself, and `Dry.Decide` sends it to `inputs.target`, or to a new id.
   - `data.query`: run with a `QueryHost`, `inputs.params` given as the event's fields and as `params`; `result` is its output.
   - `server.action`: run with no host, so *Send command* goes to the dry run as the user.
   - `ui.handler` and `prefab.logic`: run with the kind's test host, from the On event's fields with `state` and `props`. The host's report (view-state changes, actions, raised events, subscriptions) comes back as `report`.
   - Other kinds (`mode`, `agent`, `function`, `app.trigger`) are refused with a pointer to `bp.test`.
6. **An app's graph runs as its draft run does.** `Dry.Names` maps the manifest's command types to `draft.<appId>.<type>`, so a handler that sends `task.reassign` decides the draft's command graph.
7. **Seeds** are made by `record.create` in the dry run as the user, before `as` applies, and are left out of the result's events and commands; `seeded` lists their ids. **`as`** names a role of the app that owns the graph, applied through `roleArgs` (shared with `forge.preview`, and `AsRole` underneath it).
8. **The trace.** `engine.RunArgs.Steps` keeps the run's steps in its `Result`, whether or not the trace is stored. `forge.test` gives the first 50, each with its node, type, title, loop context, inputs, outputs, exec pin, error and `stubbed`. A value over 800 bytes is cut to `{$cut, preview}`. `failed` is the last step with an error, with its inputs, else the run's failed node. A failed run's trace is stored as any failing run's is (`TraceFailures`), so it shows in the graph's Log Viewer.
9. **The tool** is `write` and never asks (forge iteration question 2), and needs `cognition.execute` on the graph, as `bp.test` does. It reads the graph's draft once the drafts projection has settled, else its published or built-in version. `ForgeDeps` gains `Engine`; `ContributeTest` is wired after `ContributeApply` in both `serve.go`s.

### Backend
- `live/bus/dry.go`: `Overlay`, `NewOverlay`, `Dry`, `DryCommand`, `WithDry`, `DryOf`. `live/bus/bus.go`: a target's expected sequence on its first event in a batch.
- `live/nodes`: `DecideObserved`, `Observer`, `CommandOf`; *Send command* and *Read projection* in a dry run. `ui/nodes`: `TestHost` decides in a dry run.
- `records/data/Sandbox.go`: the sandbox, `Deps.For`; `Find` scans in it. `records/nodes`: every node reads through `Deps.For`.
- `cognition`: `NodeType.External`, `Sample`; `engine.RunArgs.Stub`, `Steps`; `Result.Steps`, `Stubbed`.
- `apps/tools/test.go`: `ContributeTest`, `Test`, `TestArgs`, `TestResult`, `TestEvent`, `TraceStep`, `TestFailure`; `roleArgs` in `apply.go`.

### Verification of slice 42
- Forge's `go test ./features/starter` (`sandbox_test.go`, over the seeded starter library and the Tasks sample):
  - **`TestForgeTestReassign`**: `forge.test` of `cmd-tasks-task-reassign` on a real task returns one `updated` event on it with the new assignee, one decided command and a trace in which *Update record* got the assignee. The `rec` stream's last sequence and the task's projected record are unchanged. The *Task detail* handler `tasks-onReassign` decides `draft.tasks.task.reassign` with one event and a *Reassigned* toast in its report. As *Member*, the row rule refuses the command.
  - **`TestForgeTestSeesItsOwnWrites`**: a command graph that creates two notes and renames the first gives `created`, `created`, `updated` with no problems, and the stream does not move. Published and sent for real, the same graph commits, and the note reads *renamed*.
  - **`TestForgeTestFailure`**: a query graph whose expression fails on `"many"` names the node `boom` with its inputs `{count: "many"}`; with `"41"`, its result is 42.
  - **`TestForgeTestStubsExternalNodes`**: a server action with *Upload file* (the test stack has no file store) runs, the node stubbed and marked in the trace, the output the sample.
  - **`TestForgeTestRunsApart`**: two runs at once, each seeding six notes, see only their own seeds and the one real note; afterwards, a run sees only the real note.
  - Also run with `-race`.
- Foundation's tests pass but one: `kb/services/collabservice/impl` `TestRoomSeedRelaySaveHandover` fails with or without this slice's changes. `live/bus` also passes with `-race`. `apps/ops` `TestReplayRebuildsAnApp` read tenant 3's projections without waiting for them, and failed every time with this slice's timing. It now settles them first. All of Forge's tests pass, and both `serve` commands vet. No browser walk: the slice adds no UI. 6g's Builder mode is where it shows.

### Decisions to review after seeing it
- What the trace keeps (decision 8), once a model reads it: 50 steps, values cut at 800 bytes.
- What a sandbox does not reach (decision 2): live queries, search and the followers read the real data. A rollup or a search over the run's own writes is not seen.
- The stubs' samples (decision 4): a stubbed upload's reference is refused by a record command, which a model may misread.
