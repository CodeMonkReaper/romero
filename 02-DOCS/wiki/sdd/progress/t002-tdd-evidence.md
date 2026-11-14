# T002 — observed TDD evidence

Base: `026b599a1c871cd2ace666729296919c207ce1ad`.

## Slice 1: identity and XML security

`go test ./internal/domain/identity/... ./internal/domain/security/...` with compiling stub implementations exited 1:

```text
--- FAIL: TestAuthorizationBoundary
agent may not self-verify
task scope requires task id
public write requires admin
owner must read private memory
--- FAIL: TestSanitizeAndWrap
unexpected payload: {Summary: Content: ContentSHA256: TokenCount:0 Sanitized:false}
unsafe envelope:
```

After replacing stubs with domain behavior, `go test -cover ./internal/domain/identity/... ./internal/domain/security/...` exited 0, with 50.0% identity and 88.2% security. The boundary triangulation then passed with 90.9% identity and 100.0% security.

## Slice 2: topic-key partition boundaries

`go test ./internal/domain/identity/... -run TestTopicPartitions` against the compiling `return false` partition stub exited 1. Each of the five scopes failed `same scope must compete`; cross-field cases also failed `wrong project partition`, `wrong owner partition`, or `wrong task partition`.

The partition rule compares tenant and scope first; agent-private also compares project and owner, task-shared compares project and task, project-shared compares project, and organization/public scopes compare tenant only. Invalid/empty boundaries deny by default.

## Traceability

- AC-01/AC-02: package-boundary tests for all five visibility and write boundaries, cross-tenant/project/agent/task denial, missing task ID, no grants, zero authentication, and scope-partition comparisons.
- AC-04: every principal type against all five epistemic states; invalid principal/status denial; administrative public-write authority.
- AC-09: exact character-removal ranges, preserved tabs/newlines/carriage returns, UTF-8 and Unicode character limits, empty-after-sanitization rejection, SHA-256 after sanitization, idempotence, malicious XML content and attributes parsed through the standard XML decoder.

## Environment

Go 1.27.3 Windows/amd64 has CGO disabled; no GCC, sqlc, golangci-lint or govulncheck was found on PATH. The parent orchestrator owns Linux-container race validation and the remaining controls.
