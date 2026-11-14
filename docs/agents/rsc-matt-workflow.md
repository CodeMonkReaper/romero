# rsc and Matt skills in ROMERO

## Authority

User instructions take precedence. rsc owns lane selection, constitution, approved specification/plan/tasks, implementation gates, TDD, risk review, verification, evidence and shipping. GitHub owns operational ticket state, assignees, dependencies and PR links. Matt skills provide interviews, incoming triage and ticket publication within that workflow.

The local task checklist records implementation verified by evidence; an open issue may still await merge. Explain this distinction in the issue. Reconcile conflicting records against actual tests, reviews and merged PRs before advancing either record. A label, closed issue or approved PR never substitutes for an rsc gate.

## Routing and collisions

- Bare `implement`, `review`, `tasks`, `plan`, `verify`, `ship` and other colliding names resolve to the project rsc skill in `.agents/skills/` (backed by `.rsc/skills/`). Identify the source when invoking it. An explicit user request for another variant wins; retain the project's required gates.
- Matt `to-tickets` publishes the approved rsc task breakdown. Keep rsc task IDs and acceptance criteria. If vertical slices require regrouping, propose and approve the mapping before publishing; preserve all criteria and dependencies.
- Matt `to-spec` can supply input to rsc specify/clarify; it does not create a second authoritative approved spec. Existing approved work resumes at its current rsc stage.
- Matt `implement-spec` must not independently orchestrate a second implementation run over rsc tasks. Use rsc implementation and its review/verification loop for ROMERO.
- Matt triage handles incoming reports. Tickets produced from an approved plan already have specifications; check readiness and blockers directly.

## Execution

1. Read the issue, comments, blockers, linked rsc artifacts and domain documentation. Confirm rsc stage and approvals; select only unblocked work.
2. Assign the issue and link its rsc task ID(s), working branch and eventual PR. Follow rsc isolation rules.
3. Implement through rsc. Record observed commands, results, reviews and unresolved controls in its progress ledger; summarize them on the issue with references.
4. In the PR, use `Closes #<number>` only for fully delivered issues. Use `Refs #<number>` for partial work. Record the rsc verification and review outcome.
5. After merge to the default branch, verify that the issue closed and record the PR link. Pending tests or required human approvals keep work pending. Parent issues close only when their own acceptance criteria are verified.

## Adoption boundary

This configuration adds conventions, not an automatic synchronizer. The agent updates both records at task checkpoints and checks for drift on resume. Existing Phase 01 evidence, including T001's pending live PostgreSQL control, remains intact. Publish its remaining work only after reviewing the current task breakdown and existing issues; never infer delivery from historical status alone.
