## Forge 30d: editor cells in `mui.table`

Built 2026-11-28 ([ServerUI.md › Changed in forge 30d](ServerUI.md#changed-in-forge-30d)). A `mui.table` column of `type: "editor"` edits its record field in place with the field's own editor and sends `edit` with `expect`, the row's `_seq` when editing began, so a stale edit is refused by `record.update`. Later parts: a table that edits records passes `expect` on; a new field type's editor reaches tables through `FieldInput` with nothing more.
