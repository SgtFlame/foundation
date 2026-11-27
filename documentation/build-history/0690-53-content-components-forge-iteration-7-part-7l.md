53. **Content components** (Forge iteration 7, part 7l, [forge 007l-ContentComponents.md](../../forge/documentation/plans/007l-ContentComponents.md)). *Built* ([ServerUI.md](ServerUI.md#built-in-slice-53), [Records.md](Records.md#changed-in-slice-53), [MarkdownEditor.md](MarkdownEditor.md#changed-in-slice-53)).
    - **Built**:
      - `ui`: `mui.virtualList` and `mui.virtualGrid`, windowed collections in the template evaluator (`Window.go`: `records.find` pages after cursors, skipping, a count, the `window:` handler); `mui.htmlView`, `doc.markdown` and `mui.chipsInput` with their metadata;
      - `records`: the `markdown` field type; the `record` collab document kind (`data.ContributeCollab`), saved by `record.update` as the writer;
      - `kb/collabservice`: documents of a kind (`Document`, `DocumentKind`, `OpenDoc`, `POST /collab/docs/open`), saves in order off the relay, and a fix to an empty save on handover (a seed no longer dirties the room; a dirty writer saves once synced);
      - browser: the five renderers (`ui/renderers/doc/`), `record.fieldEditor`'s markdown field as `doc.markdown`, remote cursors named after the signed-in user; Forge's `contentdemo` sample.
    - **Blockers**: none. Forge 07l question 1 (yes, for the new type) and gaps 2 (a `markdown` field type), 3 (server windows about three screens tall) and 4 (two row implementations) answered by the user before and while building (2026-11-21).
    - **Decisions to review after seeing it**: in [forge ContentComponents.md](../../forge/documentation/ContentComponents.md#decisions-to-review-after-seeing-it).
    - **Pending, carried forward**: merging changes made to a markdown field outside its open room; rooms across processes; selecting more than the window holds.
