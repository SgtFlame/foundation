49. **Connector groups, contributed roles, connection validators and ref settings** (with Forge iteration 7, part 7f, [forge 007f-SMTP.md](../../forge/documentation/plans/007f-SMTP.md)). *Built* ([Connectors.md](Connectors.md#built-in-slice-49)).
    - **Built**:
      - `connectors`: `Type.Group`, checked by `CheckType`; `connections`: `Connection.Group`, tenant connections of a grouped type under `tenant:<t>/connectors:<group>` (`Resource`, `Parent`), the `connectors` resource type, the `connection.validator` point run by create and update, refs in settings resolved on `cx.resolve` (`Deps.Config`, `Deps.Extensions`, `RefItem`);
      - `authz`: contributed built-in tenant roles (`authz.role`, ids 100–999);
      - Forge: the `mail` section, the *Mail administrator* role, the mail account validator and `mail.smtp`.
    - **Blockers**: none. Forge 07f questions 1 and 2 answered with their proposals before building (2026-11-12); 2 was the user's direction while building.
    - **Decisions to review after seeing it**: in [Connectors.md](Connectors.md#decisions-to-review-after-seeing-it-3).
    - **Pending, carried forward**: IMAP and POP3 joining the `mail` group (7g, 7h).
