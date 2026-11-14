1 findings: 1 blocker, 0 nits

# T002 tests-as-evidence review

Verdict: HOLD for the missing administrative-authority negative test. The implementation currently checks the flag correctly; the finding concerns proof of the security contract.

Read: approved foundation spec, plan section 0 and T002 interfaces, constitution, REVIEW.md, both new domain packages and their tests, and TDD evidence.

Observed baseline: `go test ./internal/domain/...` exited 0 for identity and security.

## Attacks and traceability

- Five-scope visibility: positive paths and independent tenant/project/owner/task mismatches are asserted. Missing grants, malformed scopes and zero authentication are denied.
- Topic uniqueness partitions: same-partition positives and changed tenant/scope/project/owner/task cases exercise the documented boundary choices. Invalid required boundary fields deny.
- Epistemic authority: principal/status matrix covers all principal kinds and statuses, including unknown values and an agent requesting verified.
- Sanitizer: exact forbidden ranges, retained whitespace controls, Unicode length limits, invalid UTF-8, empty cleaned text, sanitized flag, post-clean SHA-256 and idempotence are asserted. XML content and attribute escape attacks are decoded and inspected.
- Reverse mapping: tested behavior is within the documented domain contract. HTTP status/audit persistence and SQL uniqueness remain later-task responsibilities; these unit tests do not prove those end-to-end criteria.
- Surviving mutant: copied both domain packages and go.mod/go.sum into an isolated temporary directory, removed only `!a.IsAdmin ||` from the global_public write guard, and ran `go test ./internal/domain/...`. Both packages reported `ok`. The original candidate source was not modified.

## Finding

`TestPrincipalAuthority` sets `IsAdmin: true` for every principal. Its expected public-write result depends only on principal kind. Other public-write rejection coverage uses an agent, which remains rejected by the independent principal-kind check. Consequently the suite accepts user/orchestrator writes with no administrative grant if the admin check disappears. Add cases for both user and orchestrator with `IsAdmin=false`, an explicit global_public scope grant, and valid tenant/principal identifiers; require ErrForbidden while retaining the existing positive admin cases.

```json findings
[
  {
    "severity": "blocker",
    "file": "internal/domain/identity/identity_test.go",
    "line": 90,
    "claim": "The tests do not enforce the administrative grant required for global_public writes.",
    "failure_scenario": "A regression removes the IsAdmin check; a non-admin user or orchestrator with global_public in AllowedScopes can publish globally and the complete domain test suite remains green.",
    "evidence": "An isolated temporary-copy mutant deleting only !a.IsAdmin || produced ok for both identity and security under go test ./internal/domain/.... Existing user/orchestrator matrix fixtures always set IsAdmin=true; the nonadmin negative fixture is an agent and is rejected independently by type.",
    "class": "tests-as-evidence"
  }
]
```

## Round 2 — administrative-authority test delta

0 findings: 0 blockers, 0 nits

Verdict: PASS for the reviewed delta. The prior finding is resolved.

Inspected only the added `user_without_admin` and `orchestrator_without_admin` subtests in `TestPrincipalAuthority`. Each supplies a valid tenant/principal and explicit global_public grant while leaving IsAdmin false, then requires ErrForbidden. The existing positive admin cases remain. These assertions distinguish the required administrative flag from the separate principal-kind restriction.

The independent verifier reported that removing the IsAdmin guard now fails these two tests. My targeted `go test ./internal/domain/identity/... -run TestPrincipalAuthority -count=1` was started but had not returned at report time; no passing execution is claimed here. No other code was reviewed again.

```json
[]
```
