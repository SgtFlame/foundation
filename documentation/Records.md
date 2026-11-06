# Records: entity types and their records

Applications get their own data. An **entity type** is a schema for records, such as *Task* or *Customer*: its fields, title field, indexes and row rules. Entity types are definitions on the live core ([Live.md](Live.md)). They change only by `entity.*` commands, so several people can edit one at once and each sees the others' changes as they commit, and they are read by queries that are subscriptions. **Records** of those types are aggregates on the live core too: the generic `record.*` commands change them, one projector keeps them in KV with their **index entries**, and the `records.get`, `records.list` and `records.find` queries read them, through a query planner that uses an index when one fits. `records.find` is a sorted page that stays current without running again. A query can expand relations, and `records.aggregate` keeps counts and sums current; both run again, debounced, when what they read changes. An entity type's row rules hide records from every query and check every command. Blueprints read and write records with the record nodes, and a page binds a record query to a component prop.

It was designed in [forge 03-DataModel.md](../../forge/documentation/plans/03-DataModel.md) and is built as Forge's iteration 3, in parts. Part 3a, entity types, is slice 16. Part 3b, records, is slice 17; Forge's Data Designer (part 3c, [forge DataDesigner.md](../../forge/documentation/DataDesigner.md)) was built with it. Part 3d, indexes and the query planner, is slice 18. Part 3e, live queries, is slice 19. Part 3f, expansions and aggregates, is slice 20. Part 3g, access and row rules, is slice 21. Part 3i, record nodes and the Hello task list, is slice 23 (part 3h, atomic batches, was slice 22 in [Live.md](Live.md#built-in-slice-22)).

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

## Built in slice 17

### Decisions
Answered before building:
- **One bucket per tenant** ([forge 03-DataModel](../../forge/documentation/plans/03-DataModel.md#open-questions) question 7). The `records.record` projector folds the whole `rec` area into `PRJ_<tenant>_records_record`, keyed `<entityId>.<recordId>`, so a type's records are the keys `<entityId>.*`. It fits the live core as built: no projector per entity type.
- **A field's type can change** ([forge 03b-Records](../../forge/documentation/plans/03b-Records.md#todo) question 1). When the change is published, the records' values are converted. A value that cannot be converted is cleared, with a problem on its record.
- **Deleting an entity type hides it** (03b question 2). `entity.delete` and `entity.restore` take effect at once, without publishing. The records are kept, and record commands on a deleted type are refused.

Decided while building:
- **The record package is `records/data`**, holding the projector, commands, queries and converter, as `records/entities` does for entity types. The plan had `records/store`, `records/commands` and `records/queries`.
- **Records follow the published version.** Record commands check values against the last published version, so a type must be published before it has records, and fields added to the draft take effect when it is published. A rename touches no record: values are kept by field id.
- **A command's target type can come from its args** (`live.Command.TypeOf`). The record commands serve every entity type, and their subject is `evt.<tenant>.rec.<entityId>.<recordId>.<event>`, so the bus reads the type from `args.entity`. See [Live.md › Changed in slice 17](Live.md#changed-in-slice-17).
- **New record ids.** A record's id is a TSID. Go code and tools leave the target out and the bus makes one (`live.Command.NewTarget`), returning it in the ack's `target`. The browser makes its own (`newRecordId()` in `Records.ts`), so a command resent after a reconnect is a duplicate rather than a second record.
- **Only changed fields are recorded.** `record.update` commits `updated` with the fields whose value changes and the values they replaced, so two people changing different fields both keep their change. A value equal to the stored one commits nothing.
- **Lists change by item.** `record.addTo` and `record.removeFrom` commit `added` and `removed` with the items that changed, for `multiEnum`, `tags`, `list` and to-many relations.
- **`expect` is the record's `_seq`.** A stale one is rejected with error code `record_changed` (`live.Problem.Code`, see Live.md).
- **Values are stored normalized.** A decimal is a string with the field's scale, money with a currency a decimal string and without one `{amount, currency}`, a `dateTime` in UTC, a duration in Go's form (`1h30m`), and an enum its option's id (a name is accepted when sent). `richText`, `file` and `volumeFile` are kept as sent until Forge's 3j. Computed fields cannot be set. A field's default is checked against its type when the field is defined.
- **Field ids cannot start with `_`.** A record keeps its own data under `_id`, `_entity`, `_seq`, `_createdAt`, `_updatedAt`, `_createdBy`, `_updatedBy` and `_problems`.
- **The converter is a follower** (`records.converter`) of published events, started like the versions follower: by `entity.publish` before its event commits (`entities.Deps.OnPublish`), and by record commands and `records.list` after a restart. A published version names the fields whose type changed since the last (`Publication.TypeChanges`). For each record holding a value of one, the converter sends the internal `record.convert` command as a service principal. Its id names the event, record and field, and a value that already fits is left alone, so redelivery changes nothing. A computed field's type cannot change, and a new type drops what the old one needed (options, relation, scale, default and the rest) unless the change gives them again.
- **Access**: the `record` resource type, with `read`, `create`, `update` and `delete` (classes read, create, write and delete), on the entity type's path (`tenant:<id>/records/entity:<entityId>`). Forge's 3g adds row rules and moves it under the owning app. It is named `record`, not the plan's `entity`, so that `entity.create` keeps meaning the command that makes an entity type. The built-in *Editor* role has no delete class, so editors cannot delete records.
- **`records.list` scans** up to the `records` section's `scanLimit` (10 000), and fails with `scan_limit` beyond it until indexes come (3d). It takes `where` (field id → value, equality) and keeps a filtered list current: a record that stops matching is removed. Sorting is the client's for now.
- **`records.import` sends one `record.create` per row**, `importConcurrency` (8) at a time. Command ids name the import and the row. A rejected row is reported with its problems (the first 20) and the rest are imported. The plan's "one command per batch" waits for a batch-creating command; atomic batches exist since slice 22 ([Live.md](Live.md#built-in-slice-22)).
- **REST** serves only reading and import (`/records/records/get|list|import`). Records change by commands on the live connection; `recordservice.Create`, `Update` and `Delete` are for Go only.
- **No per-entity event retention.** Events are kept forever on every stream (Live.md, 01-ServerUI question 6), so the setting the plan listed was not made.

### Backend
- **`features/records`**:
  - `records.CheckValue(field, value)` checks and normalizes a value, and `records.Convert(from, to, value)` converts one to a new type: kept if it fits, else read as text, a number or a list and checked again. `records.ItemOf` gives the field each item of a list-like field is.
  - `records.Check` also checks a field's default, and refuses field ids starting with `_`.
  - `EntityType.Hidden` marks a deleted entity type.
  - `records.ContributeRecordAccess` declares the `record` resource type, and `records.ContributeAccess` calls it.
- **`records/entities`**:
  - `entity.updateField` can change a field's type, except to or from a computed type.
  - `entity.delete` and `entity.restore` commit `deleted` and `restored`, which both projections fold at once. `entity.list` summaries carry `hidden`.
  - `entity.publish` records `typeChanges` and calls `Deps.OnPublish` first.
- **`records/data`** (`recordsdata.Contribute`):
  - The `records.record` projector over the `rec` area. Events are `created {values}`, `updated {values, old}`, `deleted {old}`, `added`/`removed {field, items}` and `converted {values, old, problems}`, each with the command, request and principal on the event.
  - The commands `record.create {entity, values}` (no target makes one), `record.update {entity, set, expect?}`, `record.delete {entity, expect?}`, `record.addTo` and `record.removeFrom {entity, field, items, expect?}`, and the internal `record.convert`. Problems are at `args.values.<field>`, `args.set.<field>` and so on.
  - The queries `records.get {entity, id}` and `records.list {entity, where?}`, both needing `record.read`.
  - `Follow(args, deps)` starts the converter, and `Entity(args, store, id)` reads the published version records follow.
- **`records/settings`** (`recordssettings.Contribute`): the `records` section (System › Records), with `scanLimit` and `importConcurrency`.
- **`services/recordservice`**, **`rest/recordhandlers`** and **`rest/recorddto`**:
  - `Get`, `List` (oldest first) and `Import`, served at `POST /records/records/get|list|import`;
  - `Create`, `Update` and `Delete`, for Go only.
- **`records/tools`**: `recordstools.ContributeRecords` adds `records.query` (a one-time read, 50 records by default) and `records.import`. The `record.*` commands are tools from `livetools`, and `record.create`'s tool takes no target and returns the one it made.
- **Wiring**: `serve.go`, in Foundation and in Forge, registers `recordssettings` and `recordsdata.Contribute` and `recordstools.ContributeRecords` after the entity types. It sets `entities.Deps.OnPublish` to start the converter, and registers `recordhandlers` on `api`.

### Browser
- `src/services/Records.ts` is the REST client (`get`, `list`, `import`), with the `RecordData` type, `newRecordId()` (a TSID made in the browser) and `compareRecordIds`.
- `EntityType` in `Entities.ts` has `hidden`. In `live/`, `Ack` has `target` and `Problem` has `code`.
- Editors read records with `useQuery('records.list', {entity, where?})` and change them with `record.*` commands. Forge's Data Designer is the first ([forge DataDesigner.md](../../forge/documentation/DataDesigner.md)).

### Verification of slice 17
- `go test ./features/records/... ./features/live/... ./features/ui/... ./features/cognition/drafts/... ./features/authz/...` passes, and `records/data` passed five runs in a row.
  - `records/data`, with *Task*:
    - Create, update, add to, remove from and delete each commit the event described above, with the replaced values.
    - Two connections, each with its own bus, toggle `done` at once: both commit, in turn, and the projection ends with the later one at its sequence.
    - Two connections change different fields at once, and both are kept.
    - Rejections, each with its problem: a stale `expect` (`record_changed`); a wrong type; a missing or cleared required field; an unknown field; an unpublished entity type; a deleted one (and accepted again after `entity.restore`).
    - Renaming a field writes no record event, and the records read the same.
    - `points` changes from text to number: `3` and `8` become numbers, and `five` is cleared with a problem.
    - **Acceptance**: the projection's bucket and consumer were deleted and rebuilt from the stream, and the 999 records equal the ones before.
    - Importing 1 000 rows (one invalid) creates 999 and reports row 7. A second subscriber to `records.list {where: {done: false}}` sees the 500 open ones arrive, and loses one when it is toggled done.
  - `records/entities`: a type change drops the enum's options, `entity.delete` and `restore`, a bad default, and a `_` field id.
  - `records/tools`: `record.create` without a target makes an id, `records.import` creates 2, and `records.query` with `where` finds 1.
- `serve` builds and runs (Forge's, `backend/scripts/build.sh`), and so does Foundation's. `cd frontend/common && yarn build` passes. Forge's Data Designer was seen with two users in headless Chromium ([forge DataDesigner.md](../../forge/documentation/DataDesigner.md#verification-of-part-3c)).

### Decisions to review after seeing it
- Records follow the published version, so a type must be published before it has records.
- A value that cannot be converted is cleared, with a problem on its record, and kept only in the record's history.
- Deleting an entity type takes effect at once, without publishing.
- The `record` resource type, and *Editors* not deleting records.
- Values stored normalized: a decimal as a string, an enum as its option's id.

## Built in slice 18

### Decisions
Answered before building, in [forge 03-DataModel.md](../../forge/documentation/plans/03-DataModel.md#open-questions) and [forge 03d-Indexes.md](../../forge/documentation/plans/03d-Indexes.md#todo):
- **Records and their index entries change in the same fold** (03-DataModel question 2). The `records.record` projector's `Keep` (Live.md › Changed in slice 18) writes a record's index entries before the record is written and the event acknowledged. An index is never behind the records, and both are rebuilt together. The planner checks every record it reads against the query, so a query never returns a record that does not match; at worst a record is missing for the moment between its entry and its value.
- **An index's order lives in memory** (03d question 1). NATS KV lists keys in write order and filters only whole tokens, so it cannot walk a range in order. The entries stay a KV projection, and each index a query uses is loaded into a B-tree (`google/btree`) on first use and kept current by watching its keys.

Decided while building:
- **One side bucket per tenant**, `PRJ_<tenant>_records_index`, beside the records' (03-DataModel question 7 left index buckets to 3d). An entry is the key `<entityId>.<indexId>.<token>….<length><recordId>`: a token per field, then the record id after its length in three digits, so ids sort as `compareRecordIds` does (a shorter TSID is an earlier one). The value is `1`. A record keeps the list of its entries in its projected value's `kept`, so a change deletes exactly the entries it no longer has.
- **Order-preserving tokens** (`records.SortTokens`). A value becomes bytes that compare as it does, written in base32hex, whose alphabet is in ASCII order, so one token is one key part. `.` sorts below every token character, so a shorter token sorts first.
  - No value is `0` and sorts first. Every value starts with `1`.
  - A number is its IEEE bits with the sign flipped (all bits, when negative).
  - A decimal or money is a sign, an exponent and its significant digits, which are flipped for a negative number and end in `0xFF`. Scale does not matter: `1.2` and `1.20` are the same token. Money without a fixed currency sorts by currency first.
  - A `dateTime` is its Unix seconds and nanoseconds, a `duration` its nanoseconds, a `date` its text.
  - An enum sorts by its option's position in the field, so *low, medium, high* sort as listed.
  - Text sorts by its first 120 bytes in lower case, then as written. Texts that agree that far sort by record id.
  - A list-like field (`tags`, `multiEnum`, a to-many relation, a list) is entered once per item.
  - `richText`, `json`, `object`, files and computed fields have no order. `records.Check` refuses an index over them, an index with more than one list-like field, and an index id starting with `_`.
- **Indexes follow the published version**, as records do. `entity.publish` names the indexes a version adds, removes or changes (`Publication.IndexChanges`: their fields, or how those fields sort). The **indexer** (`records.indexer`, a follower of published events started with the converter) rebuilds them with `Rekeep`, record by record. It makes two passes: the second, once the projector has settled, takes the records that changed during the first, since one folded while the version was being set may have kept the old entries.
- **Every entry is written on every fold**, and only those no longer wanted are deleted. A fold whose write was refused (`Rekeep` changed the record meanwhile) is folded again, and it cannot leave an entry missing. It can leave a stray one, which the planner skips: a record is read at an entry only when that entry is one of its current ones.
- **The query** (`data.Query`): `entity`, `where` (equality, as in slice 17), `filter` (`{field, op, value}` or `{and}`/`{or}` of filters), `sort` (`[{field, desc}]`), `limit`, `cursor` and `fields`. The ops are `eq`, `ne`, `lt`, `lte`, `gt`, `gte`, `in`, `prefix` and `contains`. A record without a value matches only `eq null` and `ne` a value. `prefix`, and `contains` on text, ignore case. `contains` on a list-like field is an item it holds. Ranges compare in sort order. `_id`, `_createdAt` and `_updatedAt` can be filtered and sorted by.
- **The order**: by the sort's tokens, then by record id in the last sort field's direction (ascending without a sort). A record without a value sorts first ascending and last descending. A scan sorts with the same tokens, so an index and a scan always agree.
- **The planner** scores each index by how many of its leading fields have an `eq`, `contains` (list-like) or `in` condition, plus one for a range or prefix on the next field. Only the top-level `and`'s conditions count.
  - When the sort is exactly the index's remaining fields, in one direction, the planner **walks** the index in order from the cursor and reads only the page (`ordered`). A query with no usable condition walks an index whose fields are the sort.
  - Otherwise it **looks up** the index's matches, then sorts them in memory. An `or` whose branches each have an index takes the union (`index: "a+b"`).
  - Otherwise it **scans** the type's records, up to the `records` section's `scanLimit`, and says so: `plan {scan, read, complete}`. A scan, or an ordered walk, that stops at the limit has `complete: false`.
- **The cursor** is the page's last row (its sort tokens and id) in base64url. An ordered walk seeks to it, and the other plans drop the rows up to it.
- **`records.list` stays a live, keyed list.** Its snapshot comes from the planner (`where` and `filter`, no sort or limit), and its changes from `Updates`, started before the snapshot is read. It still fails with `scan_limit` when a scan cannot read every record. Sorted pages that stay live (moves and windows) are Forge's 3e. One-time sorted pages are `recordservice.Find`, `POST /records/records/find` and the `records.query` tool.
- **Not built**: unique indexes are declared but not enforced. The plan has no answer yet for a check made while a concurrent create is still being folded.

### Backend
- **`features/records`**: `Order.go`, with `SortTokens`, `SortToken`, `PrefixRange`, `Token` and `NullToken`, and `Sortable` and `Indexable`. `records.Check` checks indexes as above.
- **`records/entities`**: `Publication.IndexChanges`, made by `entity.publish`.
- **`records/data`**:
  - `Index.go`: the projector's `Keep`, `IndexBucket`, `IndexKeys`, `EntryTokens`, `EntryID`, the in-memory trees and the indexer (`Follow` starts it before the converter). Published entity types are cached per set of projections, so a fold does not read them; the indexer refreshes them.
  - `Find.go`: `Query`, `Filter`, `Sort`, `Result {records, next, plan}`, `Plan {index, ordered, scan, read, complete}`, `Ops`, and `Find(args, deps, query)`.
  - `records.list` takes `filter`.
  - `Deps.Projections` also needs `Updates`, `Side`, `Rekeep`, `Settle` and `Context` (`project.Projections` has them), so `serve.go` is unchanged.
- **`services/recordservice`**: `Find(FindArgs{Query})` returns `FindResponse{Result}` (`record.read`). It is served at `POST /records/records/find` (`FindRecordsRequest`, `FindRecordsResponse`), and `List` takes `Filter`.
- **`records/tools`**: `records.query` reads a page through `Find`, with `filter`, `sort`, `limit` (50), `cursor` and `fields`, and reports `next` and the plan.
- The `records` section's `scanLimit` now bounds what a query reads without an index.

### Browser
- `Records.ts` has `find(request)`, with `RecordFilter`, `RecordSort`, `FindRecordsRequest`, `FindRecordsResponse` and `QueryPlan`, and `ListRecordsRequest.filter`.

### Verification of slice 18
- `go test ./features/records/... ./features/live/... ./features/ui/... ./features/cognition/drafts/... ./features/authz/...` passes, and `records/data` passed eleven runs in a row. Slice 17's type-change test stopped waiting once `five` had its problem, before the converter reached `8`, and failed about half the time once folds got slower. It now waits for all three.
  - `records`: the encodings sort as their values do, checked against exact rationals, times and durations. The cases cover negative and positive numbers, random decimals at scales 0, 2 and 4 (and money), dates and date-times from 1600 to 2400 with nanoseconds, durations, text in mixed case, enums and booleans. No value sorts first, and a prefix's range holds exactly the texts starting with it.
  - `records/data`, the **acceptance** test: two entity types, `item` with eight indexes (number, decimal, money, text, `done`+`due`, `dateTime`, enum, and `tags`+`n`) and `plain` with none, hold the same 300 random records, some values missing.
    - 300 random queries are read page by page from both: nested `and`/`or`, every operator, twelve sorts in both directions, and pages of 13 and 60 or all. Each gives the same records in the same order, with `plain` always scanned and most of `item`'s from an index.
    - After 150 random updates (fields cleared too) and deletes on both, 300 more agree.
  - An unindexed filter over 150 records with a scan limit of 100 reads 100 and says `scan, complete: false`. An indexed one reads only its matches.
  - An index added to a type with 60 records is built by the indexer. It then follows a create, an update that moves a record out and a delete, and the page reads only the matches. Removing the index removes its entries.
  - A cursor walks every page exactly once, ascending and descending, over an index's order and over a sort no index gives, with duplicate sort values and missing values. An ordered page reads at most one record more than it returns.
  - Bad queries are refused with their reason: an unknown field, a range or sort on a list, an unknown operator, a value of the wrong type, a bad cursor. So is an index with two list-like fields.
  - `records/tools`: `records.query` with `where` reads through `Find`.
- **Benchmark** (`BenchmarkQueries`, 100 000 records, two indexes, a 4-core AMD EPYC 7763 codespace):
  - Publishing the records as events takes 1.3 s. Projecting them with their index entries takes 25.7 s (about 3 900 records a second), and loading both indexes into memory 0.76 s.

  | Query | Reads | Time |
  |---|---|---|
  | `done` = false, sorted by `due`, page of 50 (ordered walk) | 51 | 3.5 ms |
  | sorted by `n` descending, page of 50 (ordered walk) | 51 | 3.4 ms |
  | `n` = 500 (lookup, 102 matches) | 102 | 7.1 ms |
  | 100 ≤ `n` < 110, page of 50 (lookup of 1 020, sorted by id) | 1 020 | 58 ms |
  | a `title` prefix, no index (scan) | 100 000 | 0.85 s |
  | sorted by `title`, no index (scan) | 100 000 | 1.13 s |

  A record read is a KV get of about 65 µs, which is most of an indexed query's time.
- `serve` builds and runs (Forge's and Foundation's, `backend/scripts/build.sh`). With a WebSocket client and `fetch` against Forge's:
  - *Task* was defined, its `done, due` index added, and an index over `tags` twice refused;
  - 40 rows were imported;
  - `POST /records/records/find` with `where {done: false}`, sorted by `due` with 5 a page, gave two pages from the index (`ordered`, reading 6 each);
  - a filter on `tags` scanned 40 and said so, and an unknown field was refused with `bad_filter`;
  - a `records.list` subscription with an `and` of `done` and a `due` range received its 6 records, and lost one when it was toggled done.
- `cd frontend/common && yarn build` passes, and so do Forge's `frontend/axiom` `yarn build` and `yarn lint`.

### Decisions to review after seeing it
- Text sorts ignoring case, then as written, by its first 120 bytes.
- Enums sort by their options' order in the field.
- A record without a value sorts first ascending.
- Ties are broken by record id in the last sort field's direction.
- `records.list` stays an unsorted keyed list; sorted live pages wait for 3e.
- Every index a query uses stays in memory for as long as the server runs (about 100 bytes an entry).

## Built in slice 19

### Decisions
Forge's [03e-LiveQueries.md](../../forge/documentation/plans/03e-LiveQueries.md) had no open questions. Decided while building:
- **A new query, `records.find`**, is `Find` as a subscription. It takes the same parameters (`entity`, `where`, `filter`, `sort`, `limit`, `cursor`, `fields`) and gives the same `{records, next, plan}`. `records.list` stays the unsorted list keyed by record, which the Data Designer reads.
- **Ordered lists in the live core.** `live.Delta` has `list`, `index` and `from`. On the list `records`, the ops are:
  - `insert` (the value at `index`);
  - `update` (the value at `from`);
  - `move` (the row at `from` taken out, and the new value put at `index`);
  - `remove` (the row at `from`).

  `key` is the record's id. Indexes count the list as the changes before left it. `next` changes as an `upsert` or `remove` of the key `next`. See [Live.md › Changed in slice 19](Live.md#changed-in-slice-19).
- **One record change, one group of changes.** Rows that left the page are removed first. Then the changed record is moved or updated, then rows that entered are inserted, and then `next` changes. An update is sent only when the fields the query returns changed. An event the page already holds (its `_seq` is not newer) sends nothing.
- **`reached`.** A change that changes nothing tells the subscriber how far it has got. It is sent once the subscription has caught up, when the last event showed nothing, so a client sees its position reach the ack of a write the page does not show. `records.list` sends it too.
- **The snapshot's position is the projector's**, read before the page (`Projections.Folded`, the consumer's ack floor), not the highest sequence among the records read. Every write folded before the read is reported as reached. After that, the position moves only with the updates the subscription has applied.
- **Two ways to hold a page.**
  - **An ordered window.** An index gives the sort and there is a limit (an ordered walk). The view holds the window, and the matches after it that it has seen, up to 256 (*beyond*). It reads the index again only when those run out: the rows missing plus four. A removal from the window does not run the query again. A match read whose value is ahead of the updates the view has applied is left for its own update, so a record is never shown before the position reaches its write.
  - **Every match.** Otherwise (a lookup, a scan, or no limit), the view holds every match after the cursor in memory, with its sort tokens and record. The window is the first `limit` of them. A scan that cannot read every match fails with `scan_limit`, as `records.list` does.
- **A page after a cursor keeps its place.** The cursor is a position in the order (keyset), so a record entering above it changes nothing. A first page, with no cursor, takes a record entering at its top and drops its last row.
- **`next`** is there while matches past the window exist. It is the window's last row, so it changes with that row.
- **The planner reads rows, then pages them.** `Find`'s plans give their rows after the cursor in order (`read`), and `page` keeps the limit. The live view uses `read` for its snapshot and for reading past its window.

### Backend
- **`features/live`**:
  - `Delta` has `List`, `Index` and `From`, and `live.Reached` is the `reached` change;
  - `live.Apply(data, delta)` applies a change to a result held as decoded JSON. UI sessions use it, so a page bound to `records.find` keeps its rows, and a `reached` re-renders nothing;
  - `project.Projections.Folded(ctx, tenant, projector)` is the position the projector has folded.
- **`records/data`**:
  - `Live.go`: the `records.find` view (`runFind`), and `ListName` (`records`);
  - `records.list` sends `reached`, and its snapshot is at `Folded`;
  - `Find.go` is split into `read`, `order` and `page`;
  - `Deps.Projections` also needs `Folded`, which `project.Projections` has.

### Browser
- `Delta` in `LiveConnection.ts` has the list ops, `reached`, `list`, `index` and `from`. `applyDelta` applies them, and `useQuery` moves its `pos` on `reached` without changing its data.
- `Records.ts` has `RecordPage`, the result of `useQuery<RecordPage>('records.find', request)`.

### Verification of slice 19
- `go test ./features/records/... ./features/live/... ./features/ui/... ./features/cognition/drafts/... ./features/authz/...` passes. `records/data`'s live tests passed ten runs in a row and two under the race detector, and the acceptance test passed with six more seeds.
  - **Acceptance** (`TestLiveQueriesEndAsAFreshRead`): `item`, with slice 18's eight indexes, and `plain`, with none, hold 80 random records each.
    - There are 60 subscriptions to `records.find`: random filters, slice 18's twelve sorts, limits of none, 4, 9 and 20, a third of them after a cursor (a page of 5) and some with `fields`.
    - Three connections, each with its own bus, then send 120 random commands at once: creates, updates (some clearing fields) and deletes, on both types.
    - Every subscription ends equal to a fresh `Find`: the same records in the same order, each with the same values, and `next` when it has one.
    - Every subscription reaches the sequence of its type's last write. Every change named the row it applied to: the row at `from` was the key, and an insert was never a row already there.
    - Moves were seen, and more than five of the pages were ordered windows.
  - **Each change on its own**, over `done = false` sorted by `n`, a page of 3:
    - a record that stops matching is removed, and the next one is inserted at the end;
    - one that matches again is inserted;
    - changing `n` moves it, and changing `title` updates it;
    - a record moved past the window is removed.

    A page after a cursor sent nothing when a record entered above it.
  - **Position**: a page opened after four writes is at the last write's sequence, one of those writes not shown. Over 20 creates, half of them outside the filter, the position reached each ack, and every record shown was shown at or after its ack.
- Forge's `serve` (`backend/scripts/build.sh`) was checked with a WebSocket client.
  - *Task* was defined with a `done, due` index, and 12 tasks were created.
  - `records.find` over open tasks, sorted by `due` with 4 a page, sent a snapshot at sequence 12 from the index (`ordered`, 5 read), with `next`.
  - Marking the second task done sent `remove` (from 1), `insert` of the fifth (at 3) and a new `next`.
  - Moving the fourth task's `due` first sent `move` (from 2 to 0).
  - A task created done sent `reached` at its ack.
  - Deleting the first task sent `remove` and the next task's `insert`.
- `cd frontend/common && yarn build` passes, and so do Forge's `frontend/axiom` `yarn build` and `yarn lint`.

### Decisions to review after seeing it
- `records.find` is its own query, and `records.list` stays unsorted.
- A `move` carries the new value, and `next` changes as a key of the result.
- `reached` is sent after every burst of events on the type that shows nothing, to every subscription on it.
- Without an index giving the sort, a live page holds every match in memory, so a lookup with many matches and a small page holds them all.
- An ordered window remembers at most 256 matches past it.
- A first page takes records entering at its top.

## Built in slice 20

### Decisions
Forge's [03f-ExpansionsAggregates.md](../../forge/documentation/plans/03f-ExpansionsAggregates.md) had no open questions. Two gaps found while reading it were asked before building (03f questions 1 and 2):
- **A relation to `user` expands from identity.** Users are rows in the application database and commit no events, so an expansion reads `{_id, username, displayName}` each time the query runs, and only for members of the principal's tenant. A change to a user does not run the query again.
- **Expansions are `expand` on a query, and aggregates are their own query.** `records.find`, `records.query` and `Find` take `expand: [fieldId]`. `records.aggregate` is a new live query, with `recordservice.Aggregate` and a tool beside it.

Decided while building:
- **Where expansions go.** Each record gets `_expanded.<field>`: the related record, or `null` when there is none or it is gone. A to-many relation gives a list in the field's order, leaving out records that are gone. The records are copies; the projection never holds `_expanded`. Expanded records are whole, whatever `fields` the query keeps, and the relation is read from the whole record even when `fields` leaves it out.
- **Expansions are checked when the query is prepared**: a visible relation field, named once, whose target is published (or `user`). Anything else is `bad_expand`.
- **What runs again.** A `records.find` with `expand` and every `records.aggregate` follow the records of each type they read (its own, and the types its expansions point to). After a change they wait the `records` section's `debounceMs` (100) for more, run once, and send the differences: list changes on `records` or `groups` (`remove` for rows that left, then each row moved, updated or inserted in turn), and `next` as a key. A run that changes nothing sends `reached`. A run's position is the projector's (`Folded`), as a snapshot's is. A `records.find` without `expand` still applies changes without running again (slice 19).
- **Aggregations.** `{entity, where, filter, groupBy?, aggregates: [{op, field?, as?}]}` gives `{groups: [{key, values}], plan}`.
  - `count` counts records, or with a field the records that have a value. `sum` and `avg` take a number, decimal, or money field with a currency. `min` and `max` take a field with an order, and not a list.
  - A value is named by `as`, else `op_field` (`count` without a field). Two with one name are refused.
  - A decimal's sum is exact, at the field's scale. Its average is rounded to two more digits, halves away from zero. A number's sum and average are numbers. A sum over no values is 0, and an average, min or max over none is `null`.
  - Groups are ordered by the key's sort token, so no value comes first and enums keep their order. A list-like `groupBy` puts a record in a group per item, and in the `null` group when it has none. Without `groupBy` there is one group, key `null`, even with no records.
  - Changes to groups are keyed by the key's JSON (`true`, `"red"`, `null`). An aggregation reads every record it keeps, from an index or a scan, and fails with `scan_limit` past the scan limit.
- **Rollup fields are not built here.** They are computed fields, which Forge's 3j builds; aggregates are a query.

### Backend
- **`records/data`**:
  - `Expand.go`: `Query.Expand`, `MetaExpanded`, and the expansion of a page's records;
  - `Aggregate.go`: `Aggregation`, `Aggregate`, `Summary`, `Group`, `GroupKey`, `AggregateOps` and `Summarize(args, deps, aggregation)`;
  - `Rerun.go`: the debounced re-run that sends differences, used by `records.find` with `expand` and by `records.aggregate`;
  - `Deps.Users(tenant, ids)` reads users for expansions (`data.User {Username, DisplayName}`). Without it, a relation to `user` expands to `null`.
- **`records/settings`**: `debounceMs` (Re-run delay, 100 ms by default).
- **`services/recordservice`**: `Aggregate(AggregateArgs{Aggregation})` returns `AggregateResponse{Summary}` (`record.read`), served at `POST /records/records/aggregate` (`AggregateRecordsRequest`, `AggregateRecordsResponse`). `bad_expand` and `bad_aggregate` are 400.
- **`records/tools`**: `records.aggregate`, and `expand` on `records.query`.
- **Wiring**: `serve.go`, in Foundation and in Forge, sets `Deps.Users` to read users joined with their membership of the tenant.

### Browser
- `Records.ts` has `FindRecordsRequest.expand`, `aggregate(request)`, `AggregateRecordsRequest`, `RecordGroup`, `AggregateRecordsResponse`, and `RecordGroups`, the result of `useQuery<RecordGroups>('records.aggregate', request)`.

### Verification of slice 20
- `go test ./features/records/... ./features/live/... ./features/ui/... ./features/cognition/drafts/... ./features/authz/...` passes. `records/data` passed four runs in a row before the expansion fix below, and two after it. Its new tests pass under the race detector, before and after that fix.
  - **Expansions** (`TestExpansionsFollowChanges`), with `person` and `job` (an owner, helpers and an assignee):
    - a job expands its owner, its helpers in the field's order (none is an empty list) and its assignee, read from a stand-in for identity, where an unknown user is `null`;
    - the projection holds no `_expanded`;
    - with `fields: [title]` the owner still expands;
    - a live page received a person's rename; after a person was deleted and a job created, it equals a fresh `Find`, with the deleted person gone from a helpers list and an owner;
    - expanding a text field or an unknown one is refused.
  - **Aggregates** (`TestAggregatesEqualAScans`):
    - `item`, with slice 18's eight indexes, and `plain`, with none, hold the same 150 random records.
    - Six aggregations are checked on both: `count` by `done`; `sum`, `avg` and `count` of a decimal; `sum` and `avg` of a number with no group; `count`, `min` and `max` of a date by `tags`; and two filtered.
    - Each equals the same aggregates computed by the test from a scan's records, with exact rationals, before and after 80 random updates (some clearing the decimal) and deletes.
    - A live subscription to each ends equal to a fresh one. Seven bad aggregations are refused.
  - **Debounce** (`TestABurstRunsAnAggregateOncePerWindow`): four connections sent 60 creates in 17 ms with a 400 ms window. The aggregate ran once, and ended equal to a fresh one.
- Forge's `serve` (`backend/scripts/build.sh`) was checked with a WebSocket client and `fetch`:
  - *Person* and *Task* (a decimal `n`, an `owner` relation) were defined and published, and six tasks created.
  - A `records.aggregate` by `done` (count, sum and avg of `n`) and a `records.find` page expanding `owner` with `fields: [title]` sent their snapshots.
  - Marking a task done and renaming the owner in the same window sent one run: the page's two rows updated with the new name, and the two groups updated.
  - `POST /records/records/aggregate` gave the same counts.
  - The first run showed `owner: null` with `fields`, the bug fixed above. The run after the fix showed the owner.
- `cd frontend/common && yarn build` passes, and so do Forge's `frontend/axiom` `yarn build` and `yarn lint`.

### Decisions to review after seeing it
- A relation to `user` expands from identity, and a change to a user does not run the query again.
- Expanded records are whole; `fields` applies only to the records of the query's own type.
- A query with `expand` runs again, debounced, rather than applying changes; 100 ms by default.
- A decimal's average has two digits more than the field.
- A list-like `groupBy` counts a record once per item.

## Built in slice 21

### Decisions
Forge's [03g-Access.md](../../forge/documentation/plans/03g-Access.md) had no open questions. One gap was asked before building (03g question 1):
- **Row rules are `expr-lang/expr` expressions, and they apply to every user**, administrators included. Only service principals (the converter, background work) skip them. Create checks the new record, update checks the record before and after the change, and delete checks it before. An expanded record follows its own type's read rule.

Decided while building:
- **What a rule sees.** `record` is the record's values by field id and its own data (`_id`, `_createdBy`, `_updatedBy` and the rest). `principal` is `{user, tenant, kind}`. A user is named by their id as text, as a relation to `user` holds it, so `principal.user` and `_createdBy` are text and `record.assignee == principal.user` works. A rule must give a bool. `records.Check` refuses one that does not parse, names something other than `record` and `principal`, or is not a bool. A rule over untyped values (`record.n + 1`) cannot be refused there, and when it runs and does not give `true`, it allows nothing.
- **Rules follow the published version**, as fields and indexes do. `entity.setAccess` changes the draft, and `entity.publish` makes it take effect.
- **The read rule is a filter on every read.** The planner checks it with the filter on every record it reads (`planned.match`), so walks, lookups and scans all apply it, and a page fills past hidden records. `records.get` gives `null` for a hidden record, and changes to `null` when it becomes hidden. `records.list` and `records.find` remove a record that stops matching, as they do for a filter. An aggregation counts only what its principal can read, and an expansion leaves hidden records out (`null`, or missing from a list). An index cannot answer a rule, so a rule that hides most records still reads them.
- **Commands.** A record the read rule hides is not there for a command: update, delete, add to and remove from are refused with the same problem as a missing record, so a rule does not tell a principal that a record exists. A create, update or delete rule that refuses is rejected with code `row_rule` at `target`, naming the rule. The after-change check folds the command's events into a copy of the record with the projector's own `Apply`.
- **Open queries run afresh on a new version.** Every record query (`records.get`, `records.list`, `records.find`, `records.aggregate`) watches the published versions of the types it reads (its own and those it expands to). When one is published again or deleted, the query stops and starts again from a new snapshot, so a new rule takes effect on open pages at once. Fields and indexes do too.
- **Access stays `record` on `tenant:<id>/records/entity:<entityId>`.** The plan's `tenant:<id>/app:<appId>/entity:<entityId>` waits for apps: there is no `app` resource type yet. The built-in roles grant `record.*` by class, and an app's roles come with forge 05.

### Backend
- **`features/records`**:
  - `Rules.go`: `CompileRule(src)` (compiled once per source), `RuleEnv`, `Allows(rule, record, principal)` and `EntityType.Rule(action)`;
  - `records.Check` checks the rules, at `access.<action>`.
- **`records/data`**:
  - `planned.match` applies the read rule with the filter. `records.get` has its own feed (`runGet`), applying the read rule.
  - The record commands check the read rule on the target, then the action's rule (`decision.rules`).
  - `republished` restarts a record query when a type it reads gets a new published version.

### Verification of slice 21
- The tests above pass (the same runs as slice 20's).
  - **Acceptance** (`TestNoSubscriberReceivesAHiddenRecord`):
    - `doc` has the rules: see your own and shared docs, create only your own, change only what you created, delete only what you own. It starts with 40 docs by three users.
    - Each user holds six `records.find` pages (three sorts, page sizes of none, 3 and 7) and a `records.list`.
    - The three users then send 180 random commands at once, each on its own bus: creating docs owned by anyone, changing owners, sharing, and deleting. 43 were accepted and 137 refused.
    - No snapshot or change any page or list received shows a doc its user may not see. Every page ends equal to a fresh `Find` as its user, and every list holds what a fresh read gives.
    - Each user's `count` equals what they see, and is less than everyone's.
  - **Each rule on its own** (`TestRowRulesInCommandsAndQueries`):
    - creating another's doc is refused by the create rule;
    - updating or deleting a doc the user cannot see is refused as a missing record;
    - a shared doc can be seen, but its update and delete are refused by their rules, with code `row_rule`;
    - a service principal changes it anyway;
    - a doc reassigned away is removed from its old owner's open `records.find`, and their `records.get` turns `null`;
    - a note expanding the doc shows it to its new owner and `null` to the old one;
    - publishing the type without its read rule ran two open pages afresh, and both then showed both docs;
    - a rule that does not parse, gives a number, or names an unknown variable is refused.
  - **Grants** (`TestActionsNeedTheirGrant`, with a policy installed): a *Viewer* cannot create, an *Editor* can create their own doc but not delete it, and a user without a grant cannot open `records.find`.
- Forge's `serve`, with the same client: a read rule `record.assignee == principal.user` was set and published. The open aggregate and page ran afresh, showing only the three tasks assigned to user 1. A rule that does not parse was rejected at `entity.access.read` with its position.

### Decisions to review after seeing it
- Rules apply to administrators too; only service principals skip them.
- A command on a hidden record is refused as a missing record, not as denied.
- Users in rules are text ids, `_createdBy` included.
- A new published version restarts every open record query on its type, from a new snapshot.
- Access stays on `tenant:<id>/records/entity:<id>` until apps exist.

## Built in slice 23

Forge's part 3i ([forge 03i-TaskList.md](../../forge/documentation/plans/03i-TaskList.md)): the Blueprint record nodes, page queries bound to view state, and the Hello app's task list, the iteration's demo. The binding and the page are in [ServerUI.md](ServerUI.md#changed-in-slice-23) and [Blueprints.md](Blueprints.md#changed-in-slice-23).

### Decisions
Gaps found while reading the plan, asked before building ([forge 03i › TODO](../../forge/documentation/plans/03i-TaskList.md#todo), questions 1-4):
- **Hello seeds *Task*.** The first time a tenant opens the task list, Hello defines and publishes *Task* (owner `app:hello`, fields `title`, `done`, `due`, `assignee`; indexes `by_due` and `by_done_due`) as the tenant's service principal. The Data Designer edits it like any other type afterwards.
- **The list is its own top-level tab**, `app:hello:tasks`, opened from *Open task list* on Hello's main page.
- **Rows bind `records.find` directly**, with `where` and `sort` bound to view state, so rows arrive as list changes. The "N open" count is a `data.query` graph over the *Aggregate* node.
- **The chat acceptance runs through a real model** (Gemini), as well as through the same commands in a Go test. Columns come from *Task*'s published fields, so a published new field is a column at once.

Decided while building:
- **Write nodes decide as the record commands do.** *Create record*, *Update record*, *Delete record*, *Add to* and *Remove from* (command graphs only) call `data.Write`, which checks the principal may run the record command on the entity type, checks the args against its schema, reads the record with `Decision.Read` and runs the same decision `record.*` runs (values, defaults, required fields, `expect`, row rules). Their events name their record as `NewEvent.Target`, so a graph's writes and its own events commit as one atomic batch ([Live.md](Live.md#built-in-slice-22)). A refusal ends the run as *Reject* does: nothing commits, and the command is rejected with the record command's problems. A command graph that writes records has `area: rec`; its own target may be a record (`task.toggle`) or a name with no events of its own (`taskList.main` for `task.clearDone`).
- **Each write sees the record as committed**: two write nodes on one record in one command both decide on the state before the command, so a record created in a command cannot also be changed by it.
- **Read nodes work in every kind**: *Get record*, *Query* (records.find's request, from the config with a `query` input over it) and *Aggregate* (records.aggregate's, with a `request` input, and `values`, the first group's values, for a plain count). Each applies the read rule as the queries do.
- **A `data.query` graph that read records runs again when they change.** The read nodes note a key pattern (`<entity>.*`) and the entity type's published version; the graph runs again, debounced by 100 ms, when a matching record changes (an updates-only watch), and sends a `replace`. A change folded between the run's read and the watch starting is caught by the projector's folded position, taken before the read.
- ***Subscribe to records* is not built.** The plan lists it, but a node that keeps a subscription needs `prefab.logic` runs (forge 04-Prefabs); a page binds the query instead. *On record changed* is registered as an `app.trigger` event, with no runs until forge 07-Email.
- ***Current user*** (`live.currentUser`, every kind) gives the run's user id as text, as a relation to `user` holds it; a handler uses it to assign a task to whoever clicked.
- **`live.Apply` no longer changes the data it is given.** It copied nothing before, and a UI session's rendered props share the held query's data, so a list change applied in place left the bound prop equal to itself and no patch was sent: an updated row never reached the page. It now copies the list or map it changes.

### Backend
- **`records/data`**: `Write(decision, deps, command, id, args)` (the record commands' decision for another target, `decideRecord` shared with the commands) and `Get` (one record, once, with the read rule).
- **`records/nodes`** (`recordsnodes.Contribute(reg, deps)`, wired in `serve.go` after `recordsdata.Contribute`): the write nodes, *Get record*, *Query*, *Aggregate* and *On record changed*.
- **`live/nodes`**: `CommandHost.Decision`, `Read.Pattern`, the debounced rerun on a pattern, and *Current user*.

### Verification of slice 23
- **Go tests** (`records/nodes`, on embedded NATS with the engine and built-in graphs):
  - `TestWriteNodesCommitAsOneBatch`: one `task.plan` command graph creates two tasks (one with a default, one with tags), updates a third, adds to and removes from its tags and deletes a fourth; all six events are one batch. The same command naming a missing record, or a create without its required title, is refused with the record command's problem, and nothing commits.
  - `TestReadNodesInAHandler`: *Get record* (and a missing one), *Query* with `where` and a descending sort, and *Aggregate* grouped by `done`, in a `ui.handler`; a write node there is an error.
  - `TestAQueryGraphFollowsTheRecords`: a `data.query` graph over *Aggregate* sends a new count after a create, an update, and once after a burst of five creates.
- **Go tests** (`ui/hello`, the demo through the live connection, two users): `TestTheTaskListWithTwoUsers` opens the list (seeding *Task*), adds tasks from the form (with *Assign to me*, expanded to the user's name), sorts them by due, refuses an empty title, filters Bob's list to open tasks while Ada ticks one done, sorts Bob's by title, toggles one task from both sessions at once (it ends open, two events on, and both sessions' rows show the same `_seq`), clears the done tasks, and adds and publishes a `priority` enum with `entity.*` commands, as the chat's tools send them: Bob's open page gains the column with its labels, and a new task gets `medium`. `TestTheGraphsAreValid` resolves all 20 Hello graphs.
- **In headless Chromium**, Forge's `serve` and Axiom, with two accounts (Ada, owner; Bob, editor):
  - Opening the list from Hello seeded *Task*; the columns came from its fields.
  - Ada's new task reached Bob's list in about 200 ms, with Ada as assignee; her tick reached her own row in about 250 ms and left Bob's open-only list at once, and both counts agreed.
  - Ada and Bob clicked one task's checkbox at the same moment: both screens showed it open again, and the counts agreed.
  - Bob sorted by title, descending, without changing Ada's order; *Clear done* removed the done task from Bob's list in about 190 ms.
- **The chat acceptance with a real model** (Gemini, from Ada's Chat, ⚡ Fast, `gemini-3.5-flash-lite`): "add a priority field with low, medium and high to tasks, default medium, and show it as a column" listed and described the entity types, sent `entity.addField` (enum, options low/medium/high, default medium) and `entity.publish {rev: 5}`, each approved in the chat, and the task list then had a *Priority* column. 💡 Standard was refused by Gemini as overloaded, and 🧪 Deep's configured `gemini-2.5-pro` is no longer offered (see [plans/Backlog.md](plans/Backlog.md)).
- **The whole suite** passes but for `records/data`, which fails about one run in five on HEAD as well: a race making a projection's bucket (`stream name already in use`) loses records in whichever test hits it (in [plans/Backlog.md](plans/Backlog.md)).

### Decisions to review after seeing it
- Hello seeds *Task* as the tenant's service principal, the first time anyone opens the list.
- A record written twice in one command sees the committed state both times.
- A `data.query` graph over records sends its whole result again on every change (debounced), unlike `records.find`.
- *Subscribe to records* waits for prefab logic.
- A field's default applies to records created after it: existing tasks showed an empty *Priority*.
- From chat, the model added and published the field, then looked for a way to "show it as a column" (Volumes, queries) and stopped without a reply: it cannot know that the list's columns follow the published fields.
