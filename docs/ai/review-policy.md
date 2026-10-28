# Required review policy

The `review-policy` commit status enforces independent human approvals for PRs
into the default branch (initially `main`) and for PRs stacked on them, whose
base is the head branch of another governed PR in this repository. Stacked PRs
reach the default branch when their stack merges, so they carry the status
before then. It supplements CI, automated bug and
security review, and conversation resolution; it does not replace those checks.
The design follows [how Anthropic reviews code](https://claude.dev/blog/solving-code-review-at-anthropic/):
tiered approval counts and agent approvals never count. Unlike that design,
approvals are not bound to a commit: they persist across later pushes.

## Review requirements

| Title tag | Independent human approvals |
| --- | --- |
| Low | 0 for a trusted author; otherwise 1 |
| Medium | 0 for a trusted author; otherwise 1 |
| High | 1 |

A trusted author is a human with current write, maintain, or admin permission.
Bot-authored PRs (including coding agents and Dependabot) and PRs from people
without write permission always need at least one approval. The maintainer who
prompted an agent may give that approval: for agent PRs, the prompter counts as
a reviewer, and no separate sponsorship step exists.

Approvals count only from humans with write permission other than the author.
Each reviewer counts once, and their latest decisive review must be an
approval. An approval persists when commits are pushed after it, whoever pushes
them. Comments do not erase an earlier approval. Dismissed approvals do not
count, and an outstanding change request from a maintainer blocks the policy.
Drafts never pass.

## Risk classification

The PR title's conventional-commit `[low]`, `[medium]`, or `[high]` tag is the
only input to the tier; the evaluator does not inspect changed files. A title
without a valid tag fails the check. Editing the title re-evaluates the PR
against the new tier, and earlier approvals still count. Reviewers are
responsible for confirming the chosen tag, as described in the README.

## Trusted execution and refreshes

The evaluator is a standard-library-only Go program in
`.github/review-policy/`. `review-policy.yml` checks out only the protected
default branch, disables stored checkout credentials and Go caches, and runs it
from there. It never checks out or executes PR code with its status-writing
token.

Two read-only workflows wake the trusted evaluator through `workflow_run`:
`review-policy-events.yml` notifies it of review events, and
`review-policy-tests.yml` runs the policy's Go tests on PRs and merge-group
commits. They use different job names so a review event can never satisfy the
required `review-policy-tests` check without running the tests. The evaluator
reads current PRs, reviews, and permissions from GitHub, never notifier
artifacts. PR changes refresh the status directly. Scheduled and manual
refreshes reconcile missed events and permission changes.

Checks fail closed for invalid titles or changed PR snapshots. Multiple PRs sharing a head SHA are evaluated together so one cannot
overwrite another's stricter result. Queue evaluation lists the commits the
merge-group commit adds on top of its base (named in the queue branch), reads
each PR number from the `(#N)` suffix GitHub appends to squash commits, and
validates every one of those PRs' current reviews before reporting on the group
SHA. This relies on the repository's squash merge method; any other commit in a
merge group fails the check.

## Administrator activation

Adding a workflow does **not** make its status mandatory. Activate it as follows:

1. Merge these files through the existing review process. Privileged evaluation
   uses default-branch code, so this initial PR cannot bootstrap its own check.
2. Open a small PR or manually run `review-policy-evaluator` to publish the first
   `review-policy` status. Confirm low and medium PRs from trusted authors pass,
   a high PR waits for one other person, and an agent PR waits for one.
3. Protect enforcement before reducing blanket review counts. Because the tier
   comes only from the title, a `[low]` PR could otherwise change this policy
   without review. `CODEOWNERS` assigns `@celestiaorg/protocol` to `.github/`,
   `.agents/`, `.claude/`, `.cursor/`, `docs/ai/`, agent instruction files, and
   the root `README.md` (which defines the tiers), so keep "Require review from
   Code Owners" enabled on `main`.
4. Require the **`review-policy` commit status**, selecting GitHub Actions as its
   expected source, and require `review-policy-tests`. Keep the existing build,
   lint, test, and security requirements.
5. Keep the GitHub merge queue required. The policy revalidates on the
   merge-group SHA; direct merging could use a status from before a review was
   dismissed. Keep conversation resolution enabled. Disable "Dismiss stale pull
   request approvals when new commits are pushed"; otherwise GitHub dismisses
   approvals on every push and they stop counting toward this policy.
6. Disable "Allow GitHub Actions to create and approve pull requests" so no
   workflow can supply an approval.
7. Set the global approval count to zero only after the above requirements work.
   Do not globally require approval of the latest push, because that also blocks
   the low-risk path. `CODEOWNERS` has no `*` entry, so code-owner approval
   applies only to the paths in step 3. Keep routine bypass disabled for people
   and agents.

This implementation governs `main` and PRs stacked on it only. Do not relax release-branch protections
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
