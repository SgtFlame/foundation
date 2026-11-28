## Forge 40f: app roles reach required apps' types

Built 2026-11-28 (`Apps.md` › *Changed in forge 40f*). An app's manifest roles may name actions on the entity types of apps it requires (`Version.Required`, `authz.Role.NamesOn` crossing the `app` fence). `ui.openEditor` passes `app:<appId>:<kind>` through unchanged.

Later work must follow this rule: an app that reads another app's records requires it and names the types in a role. Never give it a service principal.
