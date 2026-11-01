# Records: entity types and their records

Applications get their own data. An **entity type** is a schema for records, such as *Task* or *Customer*: its fields, title field, indexes and row rules. Entity types are definitions on the live core ([Live.md](Live.md)). They change only by `entity.*` commands, so several people can edit one at once and each sees the others' changes as they commit, and they are read by queries that are subscriptions. **Records** of those types are aggregates on the live core too: the generic `record.*` commands change them, one projector keeps them in KV with their **index entries**, and the `records.get`, `records.list` and `records.find` queries read them, through a query planner that uses an index when one fits. `records.find` is a sorted page that stays current without running again.

It was designed in [forge 03-DataModel.md](../../forge/documentation/plans/03-DataModel.md) and is built as Forge's iteration 3, in parts. Part 3a, entity types, is slice 16. Part 3b, records, is slice 17; Forge's Data Designer (part 3c, [forge DataDesigner.md](../../forge/documentation/DataDesigner.md)) was built with it. Part 3d, indexes and the query planner, is slice 18. Part 3e, live queries, is slice 19.

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
- **`records.import` sends one `record.create` per row**, `importConcurrency` (8) at a time. Command ids name the import and the row. A rejected row is reported with its problems (the first 20) and the rest are imported. The plan's "one command per batch" waits for atomic batches (3h).
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

