# Fragment files

Several threads build at once, and every one of them used to append to the same shared files, so nearly every pull request conflicted with the one merged before it. Since 2026-11-27 (the user's decision) those files are **generated** from directories of small **fragment** files, and nobody edits them by hand.

| Generated file | Fragments |
|---|---|
| `CLAUDE.md` | `documentation/claude-md/` |
| `documentation/BuildHistory.md` | `documentation/build-history/` |
| `documentation/Index.md` | `documentation/index/` |
| `documentation/plans/Index.md` | `documentation/plans/index/` |
| `documentation/plans/Backlog.md` | `documentation/plans/backlog/` |

`scripts/docs-index.conf` lists them; `scripts/docs-index.sh` builds them. A generated file is its directory's `*.md` files, sorted by name, joined by a blank line (a single newline between two table rows or two list items), after a comment saying it is generated.

## Rules

- **Never edit a generated file** in a feature pull request, and never commit one you regenerated. The *docs-index* GitHub Action regenerates them on `main` after every merge, and fails a pull request that changes one (unless it changes `scripts/docs-index.*` too).
- **Add your own fragment** for what you built: a build-history entry, a plan's row, a backlog item, a paragraph for `CLAUDE.md`, a row of the documentation index.
- **Name it `NNNN-<slug>.md`**. Existing fragments are numbered in steps of 10. To add at the end, take a number above the last one in the directory (after `2030-…`, use `2040-<your-slice>.md`); to add between two fragments, take a number between theirs (`0475-…`). Two branches that pick the same number with different slugs do not conflict; they sort by slug. Keep a trailing fragment such as `9990-…` last.
- **Change your own fragment in place**: a plan's status is its own row's fragment, so update that row rather than adding a row that supersedes it. Change another thread's fragment only to correct it.
- Each paragraph, heading, table row and top-level list item is its own fragment. In `claude-md/`, add to a long paragraph (*Extension points*, *Access and tenancy*, …) by a new fragment numbered just after it (`0185-<feature>.md`) rather than by appending to the paragraph's own file.
- To read a whole file as it will be after your change, run `scripts/docs-index.sh` and read it, then `git checkout` the generated files before committing (or just read the fragments).
- To turn another shared file into fragments: `scripts/docs-split.py sections|blocks <file> <dir>`, add the line to `scripts/docs-index.conf`, run `scripts/docs-index.sh`, and commit all three in one pull request.
