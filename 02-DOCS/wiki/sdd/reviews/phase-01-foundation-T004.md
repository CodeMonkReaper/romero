# Review Report — `phase-01-foundation` / `T004`

> Task: `T004` · Issue: [#4](https://github.com/CodeMonkReaper/romero/issues/4) · Branch: `feat/t004-postgres-persistence` · Base: `50bd63d`

## Risk Assessment

- `npx @ericrisco/rsc risk --base 50bd63d`: **Tier 2** (`classes: default, migrations`, `reviewers: 3`, `human approval: required`).

## Round 1 (`50bd63d..23699c2`)

- **Security (`f9d15fcf`):** `3 findings: 0 blockers, 3 nits` — `VERDICT: pass`.
  - Nits: restrict `23505` idempotency lookup to `ConstraintName == "idx_opaque_requests_idempotency"`, enforce 64-char lowercase hex SHA-256 + reject whitespace-only `IdempotencyKey`.
- **Correctness (`5e22864d`):** `4 findings: 1 blocker, 3 nits` — `VERDICT: fix`.
  - Blocker: `opaque_requests.sql` used `NOW()` (transaction start timestamp) instead of `clock_timestamp()` inside multi-statement `Execute` lease checks.
  - Nits: wrap `EvaluateTaskAccessSnapshot` booleans in `COALESCE(..., FALSE)`, detach `rollbackTx` with `context.Background()` 5s timeout, use `MaxRetries *int32` so explicit `0` overrides default `3`, align `LockActiveTopicMemory` `NULL` `project_id` handling.
- **Tests (`af11cd3c`):** `8 findings: 5 blockers, 3 nits` — `VERDICT: fix`.
  - Strengthened `migrations/schema_test.go` with `extractBlock` structural checks, `TestMigration00003_UpDownUpAndDuplicateAbort` (`org_shared` + `global_public` duplicate aborts + `Up`/`Down`/`Up`), `TestTaskPermissions_CompositeFKAndAtomicAudit` (both tables + all 5 authorization gates), `TestUnitOfWorkAndAuditOutOfRollback` (`RecordAllowed` rollback + `RecordDenied` persistence + checked `Scan` errors), `TestOpaqueRequestQueue_IdempotencyLeaseFencingRetryAndPurge` (concurrent `-race` `Enqueue`, cross-tenant `GetStatus`, expired lease fencing, mid-flight expiry inside `Execute`, `1s/2s/4s` backoff deltas, `PurgeTerminal` + `idempotency_key` reuse), and `TestTopicPartitionLocking_ScopesReadCommittedAndZeroRetry` (all 5 scope partitions, `READ COMMITTED` inside `ExecTx` & `Execute`, `MaxRetries: &0`).
- **Resolution:** Fixed in commit `a9d35ac`.

## Round 2 (`23699c2..a9d35ac`)

- **Correctness (`c1db109e`):** `0 findings: 0 blockers, 0 nits` — `VERDICT: pass`.
- **Security (`eafa6f68`):** `2 findings: 0 blockers, 2 nits` — `VERDICT: pass`.
  - Remaining nits (non-blocking): `EvaluateTaskAccessSnapshot` `p.expires_at > NOW()` vs `clock_timestamp()`, and nil-receiver guards on `*Store` / `*OpaqueRequestStore` exported methods (`NewStore(nil)` already returns `ErrNilPool`).
- **Tests (`1bf64c46`):** `3 findings: 0 blockers, 3 nits` — `VERDICT: pass`.
  - Remaining nits (non-blocking): baseline `{CanRead: true, CanWrite: true}` assertion and `pool.Exec` check before `inactive_tenant`, explicit `OpaqueStatusRunning` assertion on `agedRunningRec`, and concurrent unseeded first `Enqueue`.

## Panel Outcome (`a9d35ac`)

- **0 blockers across all 3 fresh-context Tier 2 reviewers (`VERDICT: pass`).** Pending human Tier 2 approval before merging PR.
