# Backlog: ideas no plan owns yet, and app-wide issues

Workbench surfaces and features that have come up but have no plan of their own yet, and issues seen while verifying that belong to no single feature. When one of these is taken up, it gets its own plan (or joins an existing one) and leaves this list.

## TODO
### Candidate surfaces
- **Conversation history**: search, reopen, rename, export and delete past conversations, beyond the continuous scroll in Chat ([ChatCompletion.md](ChatCompletion.md)).
- **Audit log viewer**: configuration revisions, grant changes and denied access (`AccessAudit`), secret changes (`SecretAudit`), and tool-call approvals. It could be a saved view in the Log Explorer if audit records are also logged ([RBAC.md](RBAC.md), [Secrets.md](Secrets.md)).
- **Index administration**: indexing health, files pending, failed files, reindex, and the effect of an embedding-model change ([AIChatEmbed.md](AIChatEmbed.md)).
- **Tools and MCP manager**: tenant tool policies and the registry of MCP servers ([Tools.md](Tools.md)).
- **Runs and schedules**: Cognition runs across all graphs, and schedules, if background runs are approved ([Cognition.md](Cognition.md) question 3).
- **Budgets**: limits and alerts per tenant and user ([Usage.md](Usage.md)).
- **Notifications**: approvals waiting, runs finished, budget warnings, shares accepted.
- **Tenant and User editors**: invitations and tenant settings ([MultiTenant.md](MultiTenant.md)).
- **Account page**: linked OIDC identities and user-scope settings ([MultiTenant.md](MultiTenant.md)).
- **Moderation queue**: segments flagged by 👎 *Incorrect*, beside global Chronicle shares ([ContextRetrieval.md](ContextRetrieval.md)).

### Not yet placed
Budgets and alerts (including alerts on log queries); saved Log Explorer queries; MCP; background and scheduled runs; usage retention and roll-ups; runtime plugins; editing documents with the AI. Each is described in its plan.

### App-wide issues seen while verifying
- **Dark preference**: under a dark browser preference the page body turns dark but the MUI theme stays light, so side-bar headings (*COGNITION* and others) are hard to read. No MUI theme follows the preference yet (`ui.appearance`, [Configuration.md](Configuration.md)). Seen since slice 7.
- **Deep's model is retired**: the 🧪 Deep level's default model, `gemini-2.5-pro`, is refused by Gemini as no longer offered to new users (it suggests `gemini-3.1-pro-preview`). The `ai.chat.levels` defaults and the `ai.models` catalog need it replaced. Seen in slice 23.

### Building
- **`records/data` tests fail about one run in five** (on HEAD before slice 23 as well): two projections making the same KV bucket at once get `stream name already in use`, and the test that hits it loses records (`TestAScanStopsAtTheLimitAndSaysSo`, `TestACursorWalksEveryPageOnce`). Making a bucket should treat that error as made by the other, and read it. Seen in slice 23.
- **`ui/hello`'s task test fails when run more than once in a process** (`go test -count=3`): Hello's `seeded` and `prefabsSeeded` caches are package-wide, so the second run skips seeding *Task* and the prefabs against its fresh NATS (so the whole 4d demo is one test). The cache should be per projections (or cleared by the test). Seen in slice 26; one run passes, also with `-race`.
- **A record scan can miss a record rewritten while it lists**: `data.Find`'s scan lists the projection's keys, and a record updated during the listing moves past the listing's end, so it is missing from that page (seen in slice 28: a poll during two concurrent toggles found one task of two, about one run in thirty with `-race`). Live queries are not affected, since they follow updates; one-time scans (`records.list`'s snapshot, `Find`, the record tools) are. The `ui/hello` test polls again.
- On the 7 GB codespace the linker can be killed for lack of memory while linking `out/cli`, and test binaries while the vite dev server and `serve` run. Link with `GOGC=25 GOFLAGS=-ldflags=-s backend/scripts/build.sh` (see CLAUDE.md › *Building*), or stop the other processes.
