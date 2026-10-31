# Foundation Architecture

## Frontend

The frontend is a Yarn workspace in `frontend/`. `frontend/common` (`@foundation/common`) holds the pages, components, editors and services, consumed as source; an app is a thin entry point that composes them. `frontend/example` is the reference app. The thin client web app will be `frontend/axiom` and the thick client `frontend/axiom-core`, both built the same way on `frontend/common`.

### Common

`yarn` - Don't use NPM; use Yarn for the frontend package manager
`React`
`Vite`
`Typescript`
`MUI.com`
`flexlayout-react`
`react-flow`

## Backend

`Go`
`LanceDB` - Vector DB for RAG and BM25
`Gin`
`GORM`
`samber/oops` - Structured errors, and the base of the `logging` builder

### Relational Database

For the thick client, the default is `SQLite`; for the thin client it's `PostgreSQL` but when services are hosted as a single server-side process then `SQLite` may also be used.

### Server side rendering

`a-h/templ`
