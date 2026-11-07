# Apps

Applications built from blueprints: a **manifest** of pages, navigation, editor kinds, entity types, commands, queries, roles, settings and triggers, edited by several people at once, then published, installed into a tenant and run in the workbench. Built as Forge's iteration 5 ([forge 05-Applications.md](../../forge/documentation/plans/05-Applications.md)), one part per slice. What remains is in Forge's plans for parts 5b to 5g.

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
