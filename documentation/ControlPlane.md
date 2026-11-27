# Control Plane

The runtime of forge iteration 20 (forge `documentation/plans/020-ControlPlane.md`). It takes Foundation from one process with embedded NATS to clustered NATS with accounts, daemons on LXD hosts and in instances, and the credentials those daemons and the services they start run with. It is built part by part. Each part below says what it built; the plans hold what remains.

Deployed NATS itself (cluster URLs, credentials, TLS, replicas for every stream) is forge 20a-1, documented in [Live.md › Built in forge 20a-1](Live.md#built-in-forge-20a-1-deployed-nats).

## Built in forge 20a-2: Operator, accounts and credentials

`features/controlplane/natsauth` makes the deployment's NATS authorization as **decentralized JWTs**, as decided in forge 20 question 5. It uses `nats-io/jwt` and `nkeys` directly, with no `nsc` binary.

- **Deployment** (`natsauth.Init(operator)`): an operator plus the accounts `SYS` (the system account), `CTL` (the control account, carrying `ctl.>`) and `APP` (every tenant's streams until forge 20a-3). Each account has its own signing key, which signs its users. `AddAccount(name, Limits)` makes further accounts, such as a tenant's. `Account.SetLimits` covers connections and JetStream storage, memory and streams, with zero meaning unlimited. `Account.Revoke(userKey)` revokes a user, and `Operator.Sign` re-signs an account after a change.
- **Users** (`Account.NewUser(name, Permissions{Pub, Sub, Responses}, ttl)`): a user JWT and a creds file. An empty direction denies everything in that direction, and `natsauth.Everything` allows every subject in the account.
- **Servers** run the **full resolver**. `Deployment.ServerConfig(dir)` renders the lines every `nats-server.conf` includes: `operator`, `system_account`, the resolver, and `resolver_preload` holding every account. Account changes are pushed with `natsauth.Push(sysConn, account)` over `$SYS.REQ.CLAIMS.UPDATE`, with no restart and no file edit. A pushed revocation disconnects the user at once.
- **Keys** (`natsauth.Keys`): `Deployment.Save`/`Load` keep the operator seed, each account's keys and JWT, and the account list. `DirKeys(dir)` stores each as a file with mode 0600. A deployment loaded without `operator.nk` still mints users but cannot re-sign accounts, so the operator seed may be kept offline (part question 1). Keeping these keys in the SecretStore comes with the credential issuer (forge 20h).
- **CLI**:
  - `cli nats init --keys <dir>` makes the deployment and writes `trust.conf`, `serve.creds` (an `APP` user) and `sys.creds` (a `SYS` user for pushing).
  - `cli nats user --account --name` prints a new user's creds file.
- **Tests**: `natsauth.StartTestSecure(t)` runs an in-process server with a fresh deployment, using JetStream, a listener and the rendered configuration. `CredsFile`, `Conn`, `Serve` (`natsd.Start` with an `APP` user) and `System` connect as users. The tests cover:
  - a `CTL` user's subjects never reach an `APP` user;
  - `serve`'s user has JetStream in `APP`, and `CTL` cannot read it;
  - a user scoped to one host's subjects gets a permissions violation on another host's;
  - a pushed account accepts its users at once;
  - revoked and expired users are refused.

## Built in forge 20a-3: An account per tenant

With a deployment's keys, each tenant's streams and buckets live in **that tenant's own NATS account** (`T<id>`, `natsauth.TenantAccount`), so an account boundary and not only a name prefix keeps tenants apart. Names keep their tenant suffix inside the account, so nothing that parses them changed, and development (embedded NATS, one account) works as before.

- **`natsd.Accounts`** (`For(ctx, tenant) jetstream.JetStream`) gives a tenant's JetStream. `natsd.Single` is one JetStream for every tenant; `natsd.NewPerTenant(opts, creds)` connects to each tenant's account on first use with the creds `creds(ctx, tenant)` returns, and keeps the connection (`Close` closes them all). `natsd.For(ctx, acc, js, tenant)` is `js` when `acc` is nil.
- **Who uses it**: `project.Projections.SetAccounts` and `JetStreamFor(ctx, tenant)` (event streams, projection and side buckets, folds, atomic batches; the bus and `Transact` publish through it), `jobs.Runner` (the `JOBS_<tenant>` queue, its schedules, the tenant's event streams for triggers), and `SetAccounts` on `presence.Presence`, `activity.Store`, `ui.KVSessions` and `recordsfiles.Store`. Constructors are unchanged. Without `SetAccounts` every tenant uses the shared connection.
- **What stays in `APP`** (the services account, part question 1): every connector state, `cx.call.…`, `cx.resolve`, `cx.status`, `cx_adapters`, `cx_sources`, `cx_leases`, ledgers, cursors, locks, secrets placeholders, discovered contracts and the `CX_<tenant>` event streams, until the services split (forge 21e). `Projections.JetStream()` is that shared connection; `natsd`'s `TestSharedJetStreamOnlyForServices` refuses its use outside the connectors.
- **Accounts**: `Deployment.AddTenant(id, Limits)` makes a tenant's account (unlimited by default, part question 2), `TenantOf(name)` reads an account name back. `SaveAccount` adds a new account to the account list. `natsauth.Minter(d, keys, name)` mints `serve`'s user in a tenant's account (every subject, no expiry), looking in `keys` again for an account added after it loaded.
- **serve**: `--nats-keys <dir>` (or `$FOUNDATION_NATS_KEYS`; `natsauth.TenantAccounts(opts, dir)` makes the `PerTenant`, as both serves call it), the directory `cli nats init` wrote, turns per-tenant accounts on; serve connects to `APP` with `--nats-creds` as before and to each tenant's account with users it mints. A tenant without an account fails its live core with `no_tenant_account`.
- **CLI**: `cli nats tenant add --keys <dir> --tenant <id>` makes and saves the account (it needs `operator.nk`) and, with `--nats-url`, pushes it to the running cluster as a `SYS` user (`<keys>/sys.creds` unless `--nats-creds`).
- **Tests**: `natsauth`'s `TestTenantAccountsKeepStreamsApart` (on `StartTestSecure`, two tenants: the live core makes tenant 10's event stream in `T10`, where neither `APP` nor tenant 11 sees it, and tenant 11 publishing on tenant 10's subjects reaches no stream) and `TestMinterFindsNewAccounts`.
