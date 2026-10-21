# Required review policy

The `review-policy` commit status enforces independent human approvals for PRs
into the default branch (initially `main`). It supplements CI, automated bug and
security review, and conversation resolution; it does not replace those checks.
The design follows [how Anthropic reviews code](https://claude.dev/blog/solving-code-review-at-anthropic/):
the change, not the author, sets the tier, and agent approvals never count.

## Review requirements

| Effective risk | Independent human approvals |
| --- | --- |
| Low | 0 for a trusted author; otherwise 1 |
| Medium | 1 |
| High | 2 |

A trusted author is a human with current write, maintain, or admin permission.
Bot-authored PRs (including coding agents and Dependabot) and PRs from people
without write permission always need at least one approval. The maintainer who
prompted an agent may give that approval: for agent PRs, the prompter counts as
a reviewer, and no separate sponsorship step exists.

Approvals count only from humans with write permission other than the author.
Each reviewer counts once; their latest decisive review must approve the exact
current head SHA. Comments do not erase an earlier approval. Dismissed approvals
do not count, and an outstanding change request from a maintainer blocks the
policy. Drafts never pass.

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

Maintainers should raise the title tier for a sensitive test or documentation
change. The classifier does not perform semantic AI analysis. Changes to the
classifier itself (under `.github/`) always require high review.

## Trusted execution and refreshes

The evaluator is a standard-library-only Go program in
`.github/review-policy/`. `review-policy.yml` checks out only the protected
default branch, disables stored checkout credentials and Go caches, and runs it
from there. It never checks out or executes PR code with its status-writing
token.

`review-policy-events.yml` is a separate read-only notifier for review and merge
queue events; it also runs the policy's Go tests against proposed changes. Its
completion wakes the trusted evaluator through `workflow_run`. The evaluator
reads current PRs, reviews, permissions, and files from GitHub, never notifier
artifacts. PR changes refresh the status directly. Scheduled and manual
refreshes reconcile missed events and permission changes.

Checks fail closed for invalid titles, incomplete file lists, or changed PR
snapshots. Multiple PRs sharing a head SHA are evaluated together so one cannot
overwrite another's stricter result. Queue evaluation finds every open
default-branch PR whose head is included in the merge-group commit and validates
its current reviews before reporting on the group SHA.

## Administrator activation

Adding a workflow does **not** make its status mandatory. Activate it as follows:

1. Merge these files through the existing review process. Privileged evaluation
   uses default-branch code, so this initial PR cannot bootstrap its own check.
2. Open a small PR or manually run `review-policy-evaluator` to publish the first
   `review-policy` status. Confirm a low-risk trusted PR passes, a medium PR
   waits for one other person, a high PR waits for two, and an agent PR waits
   for one.
3. Protect enforcement before reducing blanket review counts. In a branch
   ruleset, require two approvals from `@celestiaorg/protocol` for `.github/**`,
   `.agents/**`, `.claude/**`, `.cursor/**`, agent/reviewer instruction files,
   and `docs/ai/**`.
4. Require the **`review-policy` commit status**, selecting GitHub Actions as its
   expected source, and require `review-policy-tests`. Keep the existing build,
   lint, test, and security requirements.
5. Keep the GitHub merge queue required. The policy revalidates on the
   merge-group SHA; direct merging could use a status from before a review was
   dismissed. Keep conversation resolution and stale-approval dismissal enabled.
6. Disable "Allow GitHub Actions to create and approve pull requests" so no
   workflow can supply an approval.
7. Set the global approval count to zero only after the above requirements work.
   Do not globally require approval of the latest push, because that also blocks
   the low-risk path. If global code-owner approval is required, replace it with
   the conditional rules; otherwise the `*` entry in `CODEOWNERS` still requires
   a review for low-risk PRs. Keep routine bypass disabled for people and agents.

This implementation governs `main` only. Do not relax release-branch protections
until event filters, evaluation, and queue coverage are extended to them.

GitHub Actions shares an app identity across workflows, so binding the status to
that app does not isolate this evaluator from another privileged workflow.
Restrict workflow modification permissions and audit any other workflow that can
write statuses. For enforcement against malicious users with workflow-writing
access, host the evaluator in a separately controlled GitHub App instead.

## Local verification

```sh
cd .github/review-policy && go test ./...
```
