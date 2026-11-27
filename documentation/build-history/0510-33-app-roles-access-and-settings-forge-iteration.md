33. **App roles, access and settings** (Forge iteration 5, part 5c, [forge 005c-AppAccessSettings.md](../../forge/documentation/plans/005c-AppAccessSettings.md)). *Built* ([Apps.md](Apps.md#built-in-slice-33)).
    - **Built**:
      - an app's roles as custom roles of the tenant (`CustomRole.AppID|AppRole|Rules`), made, changed and removed by the system as it is installed, upgraded and uninstalled (`Policy.SyncAppRoles`, `RemoveAppRoles`, the `apps.access` follower), the installer given the marked or first role; scoped permissions (`app:<appId>/entity:<id>/record.<action>`, `Role.HoldsOn`); read-only in `authzservice` and the Role Editor;
      - an installed app's own entity types under `tenant:<id>/app:<appId>/entity:<id>` (`records.SetPlacement`, `installs.Placement`), the `app` fence on `record` (`ResourceType.Fence`), the manifest command's action checked;
      - row rules per role (`authz.RowRules`, `EntityType.RuleFor`) in every record query, subscription, command, node and tool;
      - per-tenant settings sections (`config.SectionSources`), the app's `app.<appId>`, the `app.settings` query, `app.settings` in prefab bindings and expressions, the *App setting* node;
      - the browser: `Authz.ts` types, `can` with scoped permissions and fences, the Role Editor's *App roles*.
    - **Blockers**: none. Forge 05c questions 1 to 4, and 5 found while preparing the build, were asked before building and answered with their proposals.
    - **★ review pending**: app roles in the Role Editor and the Share dialog; an app's section in Settings.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-2).
