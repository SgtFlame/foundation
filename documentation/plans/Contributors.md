# Contributors: what remains

The extension registry on both sides and every point in use are built; see [Contributors.md](../Contributors.md). Every hand-wired workbench piece has been converted.

## Points not built yet
| Point | Contribution | Owner |
|---|---|---|
| `chat.composerAction` | a button in the composer, such as the level toggle or attachments | `editors/chat` |
| `workbench.subjectDescriber` | describes what an editor kind is editing, for editors that cannot call `useWorkbenchSubject` ([WorkbenchContext.md](WorkbenchContext.md)) | workbench |
| `cognition.nodeView` | a custom node body for the Cognition Editor, such as Reconcile's +/- controls ([Cognition.md](Cognition.md)) | `editors/cognition` |

## The command palette
`workbench.command` contributions are listed in the Editors view today. A command palette lists every command, and commands gain keybindings.

## Runtime plugins
A manifest listing a bundle URL; loading it with `import()` calls the same `contribute` API. The registry already never assumes compile-time loading. This needs signing or trust decisions before it is built.

## TODO
### Open questions
1. **Runtime plugins**: do third-party bundles loaded at runtime ever matter? If so, plugin signing and trust need a design first. *Decision* (can wait):

### Not built yet
- `chat.composerAction`, `workbench.subjectDescriber` and `cognition.nodeView`.
- The command palette and keybindings.
- The Volumes side view ([ChatCompletion.md](ChatCompletion.md)).
- Runtime plugins.
