# Required review policy

The `review-policy` commit status enforces independent human approvals for PRs
into the default branch (initially `main`). It supplements CI, automated bug and
security review, and conversation resolution; it does not replace those checks.

## Review requirements

| Effective risk | Additional human approvals |
| --- | --- |
| Low | 0 for a trusted sponsor; otherwise 1 |
| Medium | 1 |
| High | 2 |

A trusted sponsor is a human with current write, maintain, or admin permission.
For human-authored PRs, the author is the sponsor. For agent-authored PRs, exactly
one maintainer must post this comment with the full current head commit SHA:

```text
/review-policy sponsor 0123456789abcdef0123456789abcdef01234567
```

The comment must come from the sponsor's own account. A PR-body field does not
establish sponsorship. Renew the comment on every new head commit. To transfer
sponsorship, delete the old claim before the new sponsor claims the same commit.

The sponsor and PR author cannot fill additional review slots. Bots and users
without write permission cannot fill them either. Each reviewer counts once;
their latest decisive review must approve the exact current head SHA. Comments
do not erase an earlier approval. Dismissed approvals do not count, and an
independent maintainer's outstanding change request blocks the policy.

## Risk classification

The conventional-commit title's `[low]`, `[medium]`, or `[high]` tag is a requested
minimum, not permission to downgrade the diff. The evaluator takes the maximum
of that tag and every changed file's floor, including a rename's previous path:

- Low: root `README.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, and Markdown under
  `docs/`, except the protected documentation listed below.
- Medium: Go `_test.go` files and files under `test/`, except protected files.
- High: all other paths, including unknown/new production directories.
- Always high: `.github/`, `.agents/`, `.claude/`, `.cursor/`, `specs/`,
  `docs/ai/`, `docs/plans/`, agent/reviewer instruction files, and every module's
  `go.mod` and `go.sum`.

These conservative rules deliberately keep production, release, workflow, and
policy changes out of the low-risk path. Maintainers should raise the title tier
for a sensitive test or documentation change. The classifier does not perform
semantic AI analysis. Future changes to its allowlist themselves require high
review. External contributors cannot use the zero-additional-reviewer path.

## Trusted execution and refreshes

`review-policy.yml` checks out only the protected default branch, disables stored
checkout credentials, and runs the dependency-free evaluator from there. It
never checks out or executes PR code with its status-writing token.

`review-policy-events.yml` is a separate read-only notifier for review and merge
queue events; it also tests proposed policy code on PRs. Its completion wakes the
trusted evaluator through `workflow_run`. The evaluator reads current PRs,
comments, reviews, permissions, and files from GitHub, never notifier artifacts.
PR changes and sponsorship comments refresh the status directly. Scheduled and
manual refreshes reconcile missed events and permission changes.

Checks fail closed for missing sponsorship, invalid titles, incomplete file
lists, or changed PR snapshots. Multiple PRs sharing a head SHA are evaluated
together so one cannot overwrite another's stricter result. Queue evaluation
finds every open default-branch PR whose head is included in the merge-group
commit and validates its current reviews before reporting on the group SHA.

## Administrator activation

Adding a workflow does **not** make its status mandatory. Activate it as follows:

1. Merge these files through the existing review process. Privileged evaluation
   uses default-branch code, so this initial PR cannot bootstrap its own check.
2. Open a small PR or manually run `review-policy-evaluator` to publish the first
   `review-policy` status. Confirm a low-risk trusted PR passes, a medium PR
   waits for one other person, and a high PR waits for two. Test an agent PR too.
3. Protect enforcement before reducing blanket review counts. In a branch
   ruleset, require two approvals from `@celestiaorg/protocol` for `.github/**`,
   `.agents/**`, `.claude/**`, `.cursor/**`, agent/reviewer instruction files,
   and `docs/ai/**`. Require an expert review for changes to consensus code too.
4. Require the **`review-policy` commit status**, selecting GitHub Actions as its
   expected source. The evaluator job name is not the required status name.
   Also require `review-policy-tests`. Keep the existing build, lint, test,
   and security requirements.
5. Require the GitHub merge queue. The policy must revalidate on the merge-group
   SHA; direct merging could use a status from before a review was dismissed.
   Keep conversation resolution and stale-approval dismissal enabled. Validate
   that the new queue status appears before admitting normal traffic.
6. Set the global approval count to zero only after the above requirements work.
   Remove any conflicting blanket classic protection or organization requirement.
   Do not globally require approval of the latest push, because that also blocks
   the low-risk path. Keep routine bypass disabled for people and coding agents.
7. If global code-owner approval is currently required, keep `CODEOWNERS` for
   assignment but replace the blanket approval requirement with the conditional
   rules. Otherwise its `*` entry still requires a review for low-risk PRs.

This implementation initially governs `main` only. Do not relax release-branch
protections or backport review requirements until event filters, evaluation,
and queue coverage have been extended and tested for those branches.

GitHub Actions shares an app identity across workflows. Binding a status to that
app does not isolate this evaluator from another privileged workflow. Restrict
workflow modification permissions, protect the paths above, and audit any other
workflow that can write statuses. For enforcement against malicious users with
workflow-writing access, host the evaluator in a separately controlled GitHub
App and require that app's status instead. Do not grant coding agents policy or
branch-rule bypass privileges.

Review refresh is asynchronous. The required merge queue provides another
current-state evaluation before merging. Keep native critical-path review
requirements and stale-approval dismissal as additional controls.

## Local verification

No Go build or npm install is needed for policy-only changes:

```sh
node --test .github/scripts/review-policy.test.cjs
```

The tests cover risk escalation, renamed files, independent human counts,
sponsorship, stale/dismissed reviews, API pagination, shared commits, and queue
revalidation. CI runs these tests against proposed changes with a read-only token.
