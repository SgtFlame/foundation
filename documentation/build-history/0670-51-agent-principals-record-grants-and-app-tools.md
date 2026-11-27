51. **Agent principals, record grants and app tools** (Forge iteration 7, part 7j, [forge 007j-AgentPrincipals.md](../../forge/documentation/plans/007j-AgentPrincipals.md)). *Built* ([RBAC.md](RBAC.md#built-in-slice-51), [Apps.md](Apps.md#changed-in-slice-51), [Records.md](Records.md#changed-in-slice-51)).
    - **Built**:
      - `features`/`authz`: the `agent` principal kind; agents read from their records (`authz.Agents`, `recordsdata.Agents`); agent grants (`RoleGrant.AgentID`); grants on one record reaching related types through row rules; `principal.hasRole`; `agent.use`, `MayUseAgent`, `AsAgent`; `authzservice` grants to and lists agents; *Grant role* and *Revoke role* (`authz/nodes`);
      - `records`: the `principal` field type; `live`: events carry `agentId`/`onBehalfOf`, presence shows agents; `jobs`/`cognition`: runs as an agent (*Enqueue job*, *Call graph*); `tools`: `tools.Sources`, `tools.Available`, an agent's writes not asked;
      - `apps`: manifest `tools` installed as `<appId>.<name>`; `sandbox.run` with `agent` and `stub`, the sandbox carried on the context (`engine.WithSandbox`, `NodeType.Model`);
      - browser: agents in the Share dialog, the Role Editor and the Access checker; the `share` UI action and *Share* node.
    - **Blockers**: none. Forge 07j questions 1 and 2 answered with their proposals before building (2026-11-20).
    - **Decisions to review after seeing it**: in [RBAC.md](RBAC.md#decisions-to-review-after-seeing-it-1).
    - **Pending, carried forward**: revoking a deleted agent's grants; agents' presence set by the Agents app's runner (7m).
