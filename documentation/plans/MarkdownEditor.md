# Markdown Editor: what remains

The three collaborative views (Live Preview, Source, WYSIWYG) and `collabservice` are built; see [MarkdownEditor.md](../MarkdownEditor.md). The editor's Chat tab is planned in [ChatCompletion.md](ChatCompletion.md).

## TODO
### Not built yet
- **External changes**: detect changes made to a file on disk outside the editor (fsnotify) and bring them into the open room. This was out of scope for the first build.
- **Remote cursors in WYSIWYG**: Milkdown syncs through markdown (`yTextBridge.ts`) rather than a `Y.XmlFragment`, so it shows no remote cursors.
