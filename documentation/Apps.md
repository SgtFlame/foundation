# Apps

Applications built from blueprints: a **manifest** of pages, navigation, editor kinds, entity types, commands, queries, roles, settings and triggers, edited by several people at once, then published, installed into a tenant and run in the workbench. Built as Forge's iteration 5 ([forge 05-Applications.md](../../forge/documentation/plans/05-Applications.md)), one part per slice. What remains is in Forge's plans for parts 5d to 5g.

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
