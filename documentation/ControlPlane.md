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
