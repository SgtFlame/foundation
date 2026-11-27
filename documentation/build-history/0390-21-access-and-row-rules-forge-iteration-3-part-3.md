21. **Access and row rules** (Forge iteration 3, part 3g, [forge 003g-Access.md](../../forge/documentation/plans/003g-Access.md)). *Built.* ★ the decisions, once apps have roles ([Records.md](Records.md#built-in-slice-21)).
    - **Built**:
      - in `features/records`, row rules on `expr-lang/expr` (`CompileRule`, `Allows`, `EntityType.Rule`), checked by `records.Check`;
      - in `records/data`, the read rule in every query (`records.get`, `records.list`, `records.find`, `records.aggregate`, `Find`, expansions), and the create, update and delete rules in the record commands;
      - record queries that run afresh when a type they read is published again or deleted.
    - **Blockers**: none. 03g had no open questions; the rules' language and who they apply to were asked with 3f's gaps.
    - **Decisions to review after seeing it**: in [Records.md](Records.md#decisions-to-review-after-seeing-it-5).
    - **Pending, carried forward**: moving the `record` resource type under the owning app, and roles granting its actions, wait for apps (forge 05).
