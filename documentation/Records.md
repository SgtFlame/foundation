# Records: entity types and their records

Applications get their own data. An **entity type** is a schema for records, such as *Task* or *Customer*: its fields, title field, indexes and row rules. Entity types are definitions on the live core ([Live.md](Live.md)). They change only by `entity.*` commands, so several people can edit one at once and each sees the others' changes as they commit, and they are read by queries that are subscriptions. Records of those types come in Forge's part 3b.

It was designed in [forge 03-DataModel.md](../../forge/documentation/plans/03-DataModel.md) and is built as Forge's iteration 3, in parts. Part 3a, entity types, is slice 16.

## Built in slice 16

### Decisions
Answered before building, in [forge 03-DataModel.md › Open questions](../../forge/documentation/plans/03-DataModel.md#open-questions):
- **Where definitions live** (question 5): drafts in KV, published versions in SQLite. An entity type's draft is the `records.entity` projection. `entity.publish` commits a version, and a follower copies each version into the tenant database (`records_entity_versions`), so definitions are exported and imported with the tenant's other tables.
- **How the iteration is recorded** (question 8): one Foundation slice per Foundation part, from slice 16.

Decided while building:
- **The commands live in `records/entities`**, beside `records` (the types and checks). This follows `cognition/drafts` rather than putting them in `services/entityservice`, as the plan had it. `entityservice` serves the published versions over REST.
- **Field ids are made from names** when a field is added without one (`Due date` becomes `due_date`, `due_date_2` when that is taken, hidden fields included). An id never changes. Records keep values by field id, so a rename never touches them.
- **Every command checks the whole definition.** A command applies its events to a copy of the draft and runs `records.Check`, so no draft is ever invalid. Problem paths are within the definition (`entity.fields.assignee.relation.entity`), each with a fix a model can act on.
- **A field's type cannot change** (`entity.updateField` refuses it, with the fix *add a new field*). What should happen to stored values when a type changes is decided with the records (forge 03b).
- **Removing a field hides it** (`hidden: true`), and `entity.restoreField` brings it back. A hidden field's name is free for a new field, so restoring it can clash, and the clash is refused. The title field cannot be removed, and neither can a field an index or a rollup uses.
- **The title field defaults to the first text field**, in `entity.create` and in `entity.addField` while there is none.
- **Relations may point to `user`**, the built-in entity type of the tenant's users. No entity type can take that id.
- **Commands the plan did not list**: `entity.setInfo` (name, description, owner, title field), `entity.restoreField`, `entity.removeIndex` (or an indexed field could never be removed) and `entity.publish`.
- **No `entity.delete` yet.** Deleting a type that records use is decided with the records (forge 03b).
- **Checking a relation reads another target's projection**, so an entity type created a moment ago may not be known yet to a command on another type. It is eventually consistent, like every read across targets. A command on the same target always reads its predecessor's state.
- **The versions follower** (`project.Projections.Follow`, the durable consumer `fol_records_entityVersions`) is started per tenant by `entity.publish` before its event commits, and by every `entityservice` read. After a restart it catches up on the first read. The copy is idempotent: a redelivered event finds its row.
- **Access**: the `records` resource type, with `read`, `write` (edit drafts) and `publish`. Paths are `tenant:<id>/records/entity:<entityId>`. Records will get their own `entity` resource type (forge 03g).

### Backend
- **`features/records`**:
  - `records.EntityType` has `{id, name, description, owner, titleField, fields, indexes, access, version}`;
  - `records.Field` has `{id, name, type, description, required, default, hidden}` plus what its type needs: `options` (enum, multiEnum), `relation {entity, many, inverse}`, `scale` and `currency` (decimal, money; scale 2 when left out), `items` (list), `fields` (object), `schema` (json), `formula` and `rollup {relation, field, aggregate}`;
  - `records.FieldTypes` is the catalog of every type in the plan's table, grouped basic, choices, references, structured and computed. A definition accepts all of them; which values are checked is up to the records;
  - `records.Index {id, fields, unique}` and `records.Access {read, create, update, delete}` (row rule expressions) are declared only. Forge's parts 3d and 3g build them;
  - `records.Check(e, known)` is every problem with a definition:
    - names, and duplicate field names (case-insensitive, among visible fields);
    - field and option ids;
    - unknown types;
    - choices without options;
    - relations to unknown types;
    - scales and currencies;
    - lists without items, objects without fields;
    - formulas without an expression;
    - rollups over a field that is not a relation;
    - the title field, and indexes over missing or hidden fields;
  - `records.Models()`: `EntityTypeVersion` (`records_entity_versions`: tenant, entity id, version, name, owner, the definition as JSON, who published it and when, and the event's stream and sequence).
- **`records/entities`**:
  - The target type is `entity` on the `def` stream. Events: `created`, `infoSet`, `fieldAdded`, `fieldUpdated` (the whole field), `fieldRemoved`, `fieldRestored`, `indexAdded`, `indexRemoved`, `accessSet` and `published`.
  - The projectors are `records.entity` (`Draft {entity, rev, published, publishedRev, updatedBy, updatedAt}`) and `records.entityPublished` (the last published `EntityType`).
  - The commands are `entity.create`, `setInfo`, `addField`, `updateField` (a merge: a property left out stays, `null` clears it), `removeField`, `restoreField`, `addIndex` (id `by_<fields>` when left out), `removeIndex`, `setAccess` and `publish {rev}`. They need `records.write`, and publish needs `records.publish`. Like `bp.publish`, `entity.publish` refuses a draft that moved since the publisher's rev. Each command's tool reports `{rev, published, fields}`.
  - The queries are `entity.describe {id, published?}` (the draft, or with `published` the last version: `{entity, rev, published, publishedRev, changed}`) and `entity.list {owner?}` (summaries keyed by `entity.<id>`). Both need `records.read`.
  - `entities.Follow(args, deps)` starts the versions follower.
- **`records/tools`** (`recordstools.Contribute`): `entity.describe`, `entity.list` and `entity.listFieldTypes`. The commands' own tools come from `livetools`.
- **`services/entityservice`**, **`rest/entityhandlers`** and **`rest/entitydto`** serve the published versions under `/records/entities`:
  - `GET /records/entities` gives each type's last version;
  - `POST /records/entities/get {id, version?}` gives one version's definition (the last when no version is named);
  - `POST /records/entities/versions {id}` lists the versions, newest first.
- **`live/project`**: `Projections.Follow(ctx, tenant, area, name, subjects, fn)` runs a side effect per event in order, as a durable consumer. An error retries the event a second later. See [Live.md › Changed in slice 16](Live.md#changed-in-slice-16).
- **Wiring**: `serve.go`, in Foundation and in Forge, migrates `records.Models()`, calls `records.ContributeAccess` and then `entities.Contribute` and `recordstools.Contribute` before `livetools`, and registers `entityhandlers` on `api`.

### Browser
- `src/services/Entities.ts` is the REST client: `listEntityTypes`, `getEntityType` and `listVersions`, with the `EntityType` and `EntityField` DTOs. `records` is in the `foundation()` plugin's routes.
- Editors read drafts with `useQuery('entity.describe', {id})` and change them with `useCommand()`. Forge's Data Designer (forge 03c) is the first editor.

### Verification of slice 16
- `go test ./features/records/...` passes, as do `cognition/drafts`, `live/...` and `ui/...`.
  - `records/entities`: *Task* is defined by commands (`title` text required, `done` bool, `due` date, `assignee` relation → user). `title` is renamed `summary`, and a second subscription to `entity.describe` receives the rename with the same field ids. `due` is removed (hidden) and restored. A stale publish is refused, version 1 is published, `entity.describe {published}` returns it, and its row reaches SQLite with the event's sequence.
  - The test also checks rejections, each with problems: a duplicate field name, a relation to an unknown entity type, a title field that does not exist, an enum without options, an unknown type, a type change, removing the title field or an indexed field, and restoring a field whose name was reused.
  - `records/tools`: the tools a chat model gets (`entity.create` and `entity.addField` from `livetools`, `entity.describe`, `entity.list`, `entity.listFieldTypes`) define *Task* and describe it. No model was in the loop: there is no Gemini key here.
- `serve` (`backend/scripts/build.sh`) with a WebSocket client and `curl`:
  - commands over `/api/v1/live` created *Task*;
  - an `entity.describe` subscription received the rename as a change;
  - a field named like the renamed one was rejected with its path and fix;
  - after `entity.publish`, `GET /records/entities`, `/versions` and `/get` returned version 1.
- Forge's `serve` builds with the same wiring. `cd frontend/common && yarn build` passes, and `frontend/example`'s `yarn lint` passes.

### Decisions to review after seeing it
- Field ids made from names, rather than opaque ids.
- Removing a field hides it, and a hidden field's name can be reused.
- A field's type is fixed once it is made.
- Published versions are copied by a follower, which catches up when the versions are first read after a restart.
