30. **Variants and the chat acceptance** (Forge iteration 4, part 4f, [forge 004f-VariantsAndChat.md](../../forge/documentation/plans/004f-VariantsAndChat.md)). *Built*, but for the acceptance's run with a real model ([ServerUI.md](ServerUI.md#built-in-slice-30); the editor's side in [forge PrefabEditor.md](../../forge/documentation/PrefabEditor.md#variants-built-with-slice-30)).
    - **Built**:
      - variants: `ui.Prefab.Variants`, `prefab.setVariants`, `ui.Node.Variant` set by `prefab.insertNode`/`updateNode`, checked, rendered under the instance's props, a variant on `ui.PrefabPage`, `ui.Preview` and `prefab.preview`, removing a used variant breaking;
      - for the chat: a failed tool's content (a command's problems and fixes) reaches the model; command tools refuse arguments beside `args`; "no node" lists the node paths; `prefab.publish` waits for the published projection;
      - the acceptance's commands as a Go test through their tools (`TestTheChatBuildsAContactCard`).
    - **Blockers**: none. Forge 04f question 1 (Gemini plus a Go test) and gap 2 (a node field for the variant) were asked before building and answered.
    - **★ review pending**: the chat acceptance with a real model. Standard was overloaded; at Fast the model got as far as the card's texts and the Edit handler's graph over several rounds, cut short by the 8-step limit and then by the free Gemini quota (429), and never published.
    - **Decisions to review after seeing it**: in [ServerUI.md](ServerUI.md#decisions-to-review-after-seeing-it-6).
    - **Pending, carried forward**: the chat acceptance with a real model; the default of 8 tool steps per round, and no retry on a provider's 429/503 (in [plans/Backlog.md](plans/Backlog.md)).
