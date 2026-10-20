# Foundation Architecture

## Frontend

The main React entry point is `frontend/axiom` but this will be refactored so that most of those components will e moved to `frontend/common`, and the thin client web app will be `frontend/axiom` and the thick client will be `frontend/axiom-core`.

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

### Relational Database

For the thick client, the default is `SQLite`; for the thin client it's `PostgreSQL` but when services are hosted as a single server-side process then `SQLite` may also be used.

### Server side rendering

`a-h/templ`
