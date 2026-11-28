# CRM

The CRM library app (forge iteration 40, `forge/documentation/plans/040-CRM.md`). Foundation holds the `crm` library app: entity types, roles, command, query and tool graphs. Forge seeds it into every tenant and holds the CRM app's pages (40e), Mail's interactions (40f) and the golden test (40h). Invoicing, Support, the Nexus and Forms use its companies and contacts.

## Built in forge 40d: the `crm` library app

`features/crm/app` (`crmapp`, app id `crm`) is laid out as Calendar's library app is: `files/app.json`, `files/entities/*.json` and `files/graphs/*.json`, embedded as `crmapp.Files` (Forge's `starter.LoadApps` appends `LoadDir(crmapp.Files)`) and given as an export document by `crmapp.Document()`. `TestDocument` checks that every manifest part has a file and every graph is used.

- **Entity types** (owner `app:crm`):
  - *Company* (`crmCompany`): `name`, `domain` (lower case), `industry`, `size`, `owner` (user), `tags`.
  - *Contact* (`crmContact`): `name`, `email` (lower case, what imports match on), `phone`, `title`, `company`, `owner`, `tags`, `source`.
  - *Pipeline* (`crmPipeline`): `name`, `default`.
  - *Stage* (`crmStage`): `pipeline`, `name`, `position` (rank scoped by pipeline), `probability` (0 to 100), `category` (`open`, `won`, `lost`).
  - *Deal* (`crmDeal`): `name`, `amount` (money in USD), `pipeline`, `stage`, `position` (rank scoped by stage), `closeDate`, `probability`, `company`, `contacts` (to-many), `owner`, `state` (`active`, `proposed`).
  - *Activity* (`crmActivity`): `type` (`call`, `email`, `meeting`, `note`, `message`), `subject`, `body` (markdown), `due`, `done`, `contact`, `company`, `deal`, `source` (`{link, title, kind}`), `at`, `owner`.
  - *Saved view* (`crmSavedView`): `entity`, `name`, `where`, `filter`, `sort`, `owner`, `shared`.
  - Companies, contacts, deals and activities are large tables (`storage: {projection: "sql"}`). The indexes cover owner, domain, email, company, name, stage with position, pipeline, and activities by contact, company and deal with `at`.
- **Roles and rules**:
  - Both roles hold every action on every type. *Sales manager* is the installer's role; *Sales rep* is the other.
  - The types' own `create`, `update` and `delete` rules decide who writes what. A company, contact, deal or activity is written by the app, the tenant's Owner, a Sales manager (`principal.hasRole('manager', 'tenant')`), or a person whose id is its `owner`, before and after the change. One with no owner is shared, so any rep may change or claim it.
  - So a rep cannot give a deal away by `record.update`. Pipelines and stages are written by managers alone. A saved view is read by its owner, or by everyone when `shared`.
  - The queries' action is `record.read` on the tenant, as every app's is, so CRM users are also tenant members (Editors, `identity.DefaultMemberRole`). The fence keeps the records themselves to the CRM roles.
- **Commands** (installed as `crm.<type>`):
  - `deal.moveStage {stage, after?, before?}` on the deal:
    - Checks the stage is in the deal's pipeline.
    - Moves the deal with the new *Move record* node (Records.md › *Built in forge 40d*), at the end of the column when no neighbour is given, and sets the probability to the stage's when it was the old stage's or empty.
    - Logs a note activity in the same commit. Two moves at once both commit in turn, the later winning, both logged.
  - `deal.accept` and `deal.dismiss` act on a proposed deal: accept activates it, dismiss deletes it.
  - `pipeline.create {name, default?, stages?}` makes a pipeline with its stages, Lead, Qualified, Proposal, Won and Lost when none are given, at most 11. With `default`, it takes the default from the others.
  - `record.reassign {entity, owner}` is for Sales managers only, checked with *Has role*, on the target company, contact, deal or activity.
- **Queries**:
  - `pipelineDeals {pipeline}`: the stages in order, each with its active deals in order, their count and total.
  - `timeline {contact | company | deal}`: activities newest first, at most 200.
  - `pipelineTotals {pipeline, groupBy: stage | owner}`: count, total and weighted amount per group, with totals.
- **Tools** (manifest `tools`, as the caller):
  - `crm.findContact {email | name}`: contacts with their company and open deals.
  - `crm.logActivity`
  - `crm.createDeal`: the default pipeline's first open stage when none is given, at the end of its column. It makes a proposal when `propose` is set or the calling agent's `autonomy` is `suggest`.
  - `crm.updateDeal`: refuses `stage` and `owner`.
  - `crm.moveDeal`: through `deal.moveStage`.
  - `crm.importContacts`: up to 500 rows through `record.upsert`, matched on the trimmed, lower-cased email.
  - `crm.agentOptions`
  - The write tools keep to the run's `reach`.

Tests: Forge's `go test ./features/starter/crm`, over the full stack with a policy and the large tables.
- A rep changes their own deals, not others', and cannot give one away or reassign.
- A manager reassigns, and the rules follow the new owner.
- A move lands between its neighbours and is logged; the probability follows the stage unless set by hand; another pipeline's stage is refused.
- Two people moving one deal at once end in one stage, both logged.
- 1 000 contacts imported twice are updated, not duplicated.
- One owner's page among 10 000 contacts is read from the table in milliseconds.

Not built:
- A default pipeline seeded per tenant: a library app has no seed records yet, so 40e's first open or a manager makes one.
- A per-tenant currency: amounts are USD.
- An `entity:<type>.read` action for an app's queries.
