---
name: implement
description: Implement a celestia-app code change with the safety workflow - triage, align, implement, invariant review loop, verify. Use when the engineer confirms the invariant workflow at the interactive gate or invokes /implement directly.
---

# /implement — safe code generation workflow

Implement the requested change by following these phases in order. Read `docs/ai/invariants.md` before anything else.

## Phase 0 — Triage

Determine the risk tier from the files the change will touch:

- **Risky**: anything under `x/`, `app/`, `pkg/`, `proto/`, or `multiplexer/`.
- **Light**: docs, test-only changes, scripts, tooling, `.github/`.
- **Consensus-critical**: risky, and the diff changes a state transition reachable from ABCI — message servers in `x/*/keeper`, ante decorators, `PrepareProposal`/`ProcessProposal`/`FinalizeBlock` logic, square construction, migrations, upgrade handling.

When in doubt, treat as the higher tier. Light tier: skip Phase 1 and Phase 3 entirely — implement, then run Phase 4. If the diff grows into a risky path mid-task, re-triage and ask the engineer again.

## Phase 1 — Align (risky only, hard gate)

1. Explore the code involved. Answer every question the codebase can answer before asking the engineer.
2. Interview the engineer about every unclear or under-defined aspect. Give a recommended answer for each question.
3. Write an assumptions note to `docs/plans/<task>.md`: goal, stated assumptions, invariants in play (by INV number), design decisions, open questions, and an implementation plan (ordered steps, files to touch, tests to add — as short as the task allows). Never commit this file.
4. Present the note and wait for explicit confirmation. Do not write code before the engineer confirms.

## Phase 2 — Implement

- Write the smallest diff that solves the problem. Follow existing patterns in the repo.
- Run `make build` after Go changes.
- For bug fixes: write the reproducing test first, watch it fail, then fix.

## Phase 3 — Review loop

1. Get the full diff (`git diff` plus untracked files) and spawn these agents from `.claude/agents/` in parallel on it: `determinism-reviewer`, `adversarial-input-reviewer`, `consensus-reviewer`, `compat-reviewer`, `simplicity-reviewer`. Give each the diff and the path to the assumptions note if one exists.
2. Consensus-critical tier: also spawn `adversarial-reviewer` in the first round. In later rounds, re-run it only on state transitions whose code changed during fixes. The engineer can request it for any change or skip it for a provably trivial one; record a skip in the report.
3. For every finding, spawn a `finding-verifier` agent to try to refute it. Discard refuted findings.
4. Fix all confirmed findings, then run the reviewers again.
5. Stop when two consecutive rounds produce zero confirmed findings, or after three rounds total. Escalate any still-open findings to the engineer — never silently drop them.

## Phase 4 — Verify

- `make lint` and `make test-short` must pass.
- Run the tests covering the changed behavior; add tests where behavior changed.

## Phase 5 — Report

Summarize for the engineer: what changed and why, assumptions honored, findings fixed vs escalated, and a one-line status per invariant (respected or not applicable).
