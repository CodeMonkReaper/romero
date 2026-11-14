# Issue tracker: GitHub

Repository: `CodeMonkReaper/romero`. Use `gh` with an explicit `--repo CodeMonkReaper/romero` on issue and label operations.

Before creating tickets, read [the integration contract](rsc-matt-workflow.md) and [triage labels](triage-labels.md). Approved rsc specs remain canonical local documents; publish execution tickets as GitHub Issues with references to those documents.

## Operations

- Confirm target: `gh repo view CodeMonkReaper/romero --json nameWithOwner,hasIssuesEnabled`.
- Read: `gh issue view <number> --repo CodeMonkReaper/romero --json number,title,body,labels,comments`.
- List: `gh issue list --repo CodeMonkReaper/romero --state open --limit 100 --json number,title,labels,assignees` (paginate when needed).
- Create: `gh issue create --repo CodeMonkReaper/romero --title "..." --body-file <utf8-file>`. Use a file for multiline bodies and comments.
- Parent: `gh issue edit <parent> --repo CodeMonkReaper/romero --add-sub-issue <child>`.
- Dependency: `gh issue edit <child> --repo CodeMonkReaper/romero --add-blocked-by <blocker>`.
- Read open blockers: `gh issue view <number> --repo CodeMonkReaper/romero --json blockedBy`; inspect states, not only the number of edges.
- If native relationships are unavailable, record `Part of #<parent>` and `Blocked by: #<number>` in the body; check each blocker's state before claiming work.
- Assign: `gh issue edit <number> --repo CodeMonkReaper/romero --add-assignee @me`.
- Label: `gh issue edit <number> --repo CodeMonkReaper/romero --add-label <label>` or `--remove-label <label>`.
- Comment: `gh issue comment <number> --repo CodeMonkReaper/romero --body-file <utf8-file>`.
- Close: follow the evidence and merge conditions in the integration contract. For cancellation, comment the reason before closing.

Before publishing, search existing open and closed issues for the rsc task ID to avoid duplicates. Publish approved tickets blockers-first. Each issue includes task IDs, spec/plan references, acceptance criteria, blockers and a verification command or observable result. Maintain issue URLs alongside task IDs in the local task artifact.

**PRs as a request surface: no.**
