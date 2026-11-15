# Review Report — `phase-01-foundation` / `T005`

> Task: `T005` · Issue: [#5](https://github.com/CodeMonkReaper/romero/issues/5) · Branch: `feat/t005-provisioning-cli` · Base: `56778e2`

## Risk Assessment

- `npx @ericrisco/rsc risk --base 56778e2` (on `5702454`): **Tier 2** (`classes: auth, default, docs`, `reviewers: 3`, `human approval: required`).

## Round 1 (`56778e2..88a0446`)

- **Security (`49293b18`):** `3 findings: 0 blockers, 3 nits`.
  - Nits: orphaned `PrincipalAgent` (`agent_id IS NULL`) evaluated `agent_active = TRUE` in `queries/provisioning.sql` and invalid `PrincipalType` in `auth.go` lacked `recordDenied`; `recordDenied` / `recordDeniedTaskAdmin` returned `nil` when `audit == nil`; negative `--expires-in` silently issued a non-expiring key.
- **Correctness (`5f9e4e7a`):** `4 findings: 2 blockers, 2 nits` — `VERDICT: fix`.
  - Blockers: `PGProvisioningAdapter` did not translate `postgres.ErrAlreadyExists` -> `provisioning.ErrAlreadyExists` or `postgres.ErrInvalidInput` -> `provisioning.ErrInvalidInput`; negative `--expires-in` silently issued non-expiring keys.
  - Nits: PostgreSQL/goose/migrations wiring lived inside `internal/interfaces/cli/adapter.go` instead of Composition Root `cmd/romero`; duplicate `task_id` in `BootstrapSpec.InitialTaskGrants` silently overwrote earlier entries.
- **Tests (`f9b0bd34`):** `6 findings: 3 blockers, 3 nits` — `VERDICT: fix`.
  - Strengthened `postgres/provisioning_test.go` (`LookupPrincipalsByPrefix` inactive agent & orphaned agent, `GetPrincipalByID` same/cross-tenant, `UpsertTaskPermission` grant/revoke verified via `EvaluateTaskAccess`, `ExecTx` rollback checking `audit_events` allowed-rollback vs denied-survives), `provisioning_test.go` (inactive tenant, inactive principal, orphaned agent, invalid `PrincipalType`, >= 2-candidate prefix collision, `nil` `AuditLogger` fail-closed, persisted `store.taskPerms` checks + no implicit orchestrator access, `actor.IsAdmin` cross-project grant, non-admin `User`/`Service` denial, duplicate `task_id`), `cli_test.go` & `config_test.go` (`fakeAuth.failErr`, `--dsn` precedence, negative `--expires-in`, nil `Dependencies{}`), and `cmd/romero/wiring_test.go` (`mapPGError`, `CountTenantProjects`, `UpsertTaskPermission`, `ExecTx` error coverage).
- **Resolution:** Fixed in commit `5702454` (moved wiring into `cmd/romero/wiring.go` cleanly keeping `internal/interfaces/cli` free of infrastructure imports per Constitution Principle 8).

## Round 2 (`88a0446..5702454`)

- **Correctness (`2e5c1c5b`):** `2 findings: 0 blockers, 2 nits` — `VERDICT: pass`.
  - Non-blocking nits: `Service.validateBootstrapSpec` allows no-op `(false, false)` `BootstrapTaskGrant` for direct Go callers (`cli/config.go` rejects it at CLI layer); `root.go` `defer cleanup()` does not nil-check `cleanup` (`cmd/romero/wiring.go` always returns non-nil `pool.Close`).
- **Security (`6d048c51`):** `0 findings: 0 blockers, 0 nits` — `VERDICT: pass`.
- **Tests (`238de717`):** `3 findings: 0 blockers, 3 nits` — `VERDICT: pass`.
  - Non-blocking nits: `cli_test.go` asserts `bootstrapCalls[0]`/`createCalls[0]` core fields without asserting `TenantName`/`ProjectName`/`AgentName` or element values of `AllowedScopes`; `svc.taskErr` remains set after line 396 (`auth.failErr` & flag validation fail earlier); `fakeStore.ExecTx` copies `snap.deniedAudits` (real PostgreSQL out-of-rollback `RecordDenied` verified in `postgres/provisioning_test.go`).

## Panel Outcome (`5702454`)

- **0 blockers across all 3 fresh-context Tier 2 reviewers (`VERDICT: pass`).** Pending human Tier 2 approval before merging PR.
