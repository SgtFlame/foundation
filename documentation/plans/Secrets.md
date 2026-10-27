# Secrets: what remains

The sealed start, `SecretStore` with `vaultstore`, the scripts, `secretsservice` and the Secrets editor are built; see [Secrets.md](../Secrets.md).

## Unattended restarts
`--vault-token-file` (for example a Vault Agent sink), opt-in, so a production restart needs no operator; the default still waits for `unseal.sh` (question 2).

## Re-reading the bundle
The bootstrap bundle is read once, at unseal, so a server secret set in the editor applies at the next start. Re-reading it without a restart removes that.

## Vault connection options
TLS options (a CA file, client certificates) and Vault namespaces; the client trusts the system's CAs today.

## TODO
### Open questions
2. **Unattended restarts**: should `--vault-token-file` be allowed? The proposal is yes, opt-in, with the default still waiting for a token. *Decision*: (after seeing) not built in slice 12: every start waits for `unseal.sh`.
3. **Where to unseal**: only `unseal.sh`, or also a token field on the browser's sealed page? The proposal is the script only, so that no token passes through a browser. *Decision*: (after seeing) built on the proposal in slice 12: the script only.
4. **Hints**: show nothing about a stored secret but whether it is set and its version (the proposal), or keep slice 2's last-four-characters hint? *Decision*: (after seeing) built on the proposal in slice 12: with Vault, Settings shows only whether a secret is set, and the Secrets editor its version too; without Vault the hint stays.
6. **Losing Vault**: keep running on cached secrets as `degraded` (the proposal), or seal again? *Decision*: (after seeing) built on the proposal in slice 12, recovering by itself when the token renews again.

(Questions 1, 5 and 7 are decided; see [Secrets.md › Decisions](../Secrets.md#decisions).)

### Review after seeing it ★
- **The sealed start and the Secrets editor** (slice 12), by a person. Not seen: a real Gemini call with a user's own key (no key in the build environment), the dark theme beyond the known app-wide issue, a Vault with TLS, and a server running past one token period (1 hour). To review: `eval "$(backend/scripts/secrets/vault-dev.sh)"`, `backend/scripts/secrets/init.sh`, `cli secrets migrate --data-dir <a copy of your data directory>`, then `serve --data-dir <the copy> --vault-addr $VAULT_ADDR` and `backend/scripts/secrets/unseal.sh`. Open Administration › Secrets, and set your own key in Settings › AI › Providers at *User*. See [Secrets.md › Verification of slice 12](../Secrets.md#verification-of-slice-12).
- **The decisions made while building**, in [Secrets.md › Decisions to review after seeing it](../Secrets.md#decisions-to-review-after-seeing-it).

### Not built yet
- `--vault-token-file` for unattended restarts.
- Re-reading the bundle without a restart.
- TLS options and namespaces for Vault.
- An audit viewer for `SecretAudit`; Settings' own secret writes are logged but not audited in that table ([RBAC.md](RBAC.md)).
- Keys for other providers, when there are other providers ([AIChatEmbed.md](AIChatEmbed.md)).
