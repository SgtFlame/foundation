## Forge 40e: the CRM app

Forge's part 40e built the CRM sample app (`sales`) over the `crm` library app. Foundation changed in one place: a page may read a required app's query by `<app>.<query>`. Draft documents, exports and `apps.CheckDocument` now find its data.query graph (`Apps.md` › *Changed in forge 40e*). An app reading another app's query must list that app in `requires.apps`.
