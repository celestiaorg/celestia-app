# AGENTS.md

This file provides guidance to AI agents when working with code in this repository.

## Commands

### Building

When editing Go code, always run `make build` after changes to catch compilation errors immediately.

```bash
make build              # Build multiplexer version (embeds v3-v9 binaries) into ./build/
make build-standalone   # Build v10-only version (no embedded binaries)
make mod                # Update all go.mod files
```

### Testing

For test-related tasks: 1) write the test, 2) run it to verify it passes, 3) check for flaky behavior by running multiple times if relevant.

```bash
go test -v -run TestName ./path/to/package  # Run a single test
make test-short                              # Run tests in short mode (1 min timeout)
make test                                    # Run all tests (30 min timeout)
make test-race                               # Run tests with race detection
```

### Linting

Before opening PRs that modify Go code, run `make lint` and `make test-short`.

```bash
make lint       # Run all linters (golangci-lint, markdownlint, hadolint, yamllint)
make lint-fix   # Auto-fix linting issues
```

### Protobuf (requires Docker)

```bash
make proto-gen    # Generate protobuf files
make proto-lint   # Lint protobuf files
```

## Architecture

celestia-app is a Cosmos SDK-based blockchain implementing Celestia's data availability layer. It runs on celestia-core (a CometBFT fork) via ABCI.

### Directory Structure

- **`/app`** - Application core: state machine, ABCI handlers (`prepare_proposal.go`, `process_proposal.go`), ante decorators (`ante/`)
- **`/x`** - Custom modules: `blob` (MsgPayForBlobs), `signal` (upgrades), `minfee` (gas price governance), `mint` (inflation)
- **`/pkg`** - Reusable packages: `appconsts`, `da`, `wrapper` (NMT), `user` (tx APIs), `inclusion`, `proof`
- **`/multiplexer`** - Multi-version upgrade system embedding v3-v9 binaries
- **`/cmd/celestia-appd`** - Binary entry point
- **`/test/util`** - Test utilities: `testnode`, `blobfactory`, `testfactory`

### Multiplexer vs Standalone

- **`make build`** (default): Multiplexer build embeds v3-v9 binaries, enables syncing from genesis through all upgrades. Build tag: `ledger,multiplexer`
- **`make build-standalone`**: v10-only, lighter. Build tag: `ledger`

The fibre and valaddr modules are compiled into every build by default. The module code lives under `x/fibre/` and `x/valaddr/`, wired into the app via `app/fibre.go`.

### Dependency Forks

All branches use forked cosmos-sdk and celestia-core:

| celestia-app | celestia-core      | cosmos-sdk                 |
|--------------|--------------------|----------------------------|
| `main`       | `v0.40.x`          | `release/v0.52.x-celestia` |
| `v9.x`       | `v0.40.x`          | `release/v0.52.x-celestia` |
| `v8.x`       | `v0.39.x-celestia` | `release/v0.52.x-celestia` |
| `v7.x`       | `v0.39.x-celestia` | `release/v0.52.x-celestia` |
| `v6.x`       | `v0.39.x-celestia` | `release/v0.51.x-celestia` |
| `v5.x`       | `v0.38.x-celestia` | `release/v0.50.x-celestia` |
| `v4.x`       | `v0.38.x-celestia` | `release/v0.50.x-celestia` |
| `v3.x`       | `v0.34.x-celestia` | `release/v0.46.x-celestia` |

## Development Workflow

1. **Multi-module repo**: Copy `go.work.example` to `go.work` and run `go work sync`
2. **Conventional commits**: PR titles must follow [conventionalcommits.org](https://www.conventionalcommits.org/) (e.g., `feat:`, `fix:`, `chore:`, `feat!:` for breaking changes). Any consensus-breaking change (one that alters deterministic state-machine behavior and requires a coordinated network upgrade) must include a `!` in the PR title, e.g. `fix!:`.
3. **Validate inputs** in message handlers; be cautious with arithmetic overflow and gas consumption
4. **Linking issues**: PR descriptions must start with a `Closes <link>` line when an issue exists, and the link must be clickable. Linear issues use `Closes [PROTOCO-1234](https://linear.app/celestia/issue/PROTOCO-1234)` — a bare `Closes PROTOCO-1234` is not acceptable because GitHub does not linkify it.
5. **Hacken bug bounty PRs**: When creating a PR that resolves a Hacken bug bounty report, do NOT include details about the bug in the PR description. Instead, link to a Linear issue (as a clickable link, per the previous item) that contains more details on the bug and the link to the Hacken bug bounty report.

## AI Safety Invariants

@docs/ai/invariants.md

Every code change must respect the invariants in [docs/ai/invariants.md](docs/ai/invariants.md), imported above. If a task cannot be done without violating one, stop and ask the engineer.

## AI Workflow

- **Risk tiers**: changes touching `x/`, `app/`, `pkg/`, `proto/`, or `multiplexer/` are risky; docs, test-only changes, scripts, tooling, and `.github/` are light; risky changes that modify a state transition reachable from ABCI are consensus-critical and also get an `adversarial-reviewer` deep review. When in doubt, treat as the higher tier.
- **Interactive gate**: before implementing or reviewing any change, triage the tier, then ask the engineer whether to run the invariant workflow (the `/implement` skill, or the reviewer agents in `.claude/agents/` for a review). Give a 2–3 line overview — tier, paths touched, what the workflow would add — and a recommendation: run it for risky and consensus-critical changes, skip it for light ones. Skip the question when the engineer invoked `/implement` directly. In non-interactive runs, follow the recommendation.
- **Declined workflow on a risky change**: still state assumptions and get engineer confirmation before writing code; no reviewer agents unless the engineer asks.
- **Assumptions notes** live in `docs/plans/` and are never committed.
- **Cheap models gather, frontier models think**: codebase exploration, log digging, and web research go to sub-agents on cheap models, which report conclusions — not transcripts — back to the frontier model; the frontier model orchestrates and keeps design, hard debugging, synthesis, and judgment calls (details in `.agents/skills/implement/SKILL.md`).
- **Persist findings to files**: on long tasks, write durable findings into the `docs/plans/` note as they are established instead of relying on chat history.
- **Context hygiene**: per-developer guidance in [docs/ai/context.md](docs/ai/context.md).

## Simplicity Rules

### PRs

- PR descriptions are as simple as possible. Omit unnecessary details.
- Keep PRs under 700 lines of code. If more is needed, propose a split into separate PRs that can each be implemented, tested, and reviewed independently.

### Issues

- Issues are simple, concise, and straight to the point. No unnecessary information.
- Every issue contains clear and simple acceptance criteria.

### Documentation

- Aim for simplicity. Prefer short sentences.
- Keep godoc comments short and easy to understand: one or two plain-language sentences saying what the thing does. Avoid big blocks of text — they are hard to read and reason about.
- No unnecessary calculations unless specified.
- No unnecessary explanations unless requested.

### Changing Files

- Touch the bare minimum of lines needed, in code or docs.
- Don't reformat files. Don't rewrite unrelated lines.
- Don't touch what you weren't asked to touch.
- Never include context specific to this conversation in comments.

### Implementing Code

- Look for similar code in the repo to reuse. Stay consistent with existing practices.
- Write the simplest human-readable implementation. Avoid premature optimizations.
- If something needs to be configurable, first ask whether it really does. No unnecessary configuration.

### Tests

- Make tests as simple and straightforward as possible.
- Initialization is usually hard — always check for existing abstractions built exactly for this before writing your own setup.

### Git

- Keep separate commits. Merge upstream, fix conflicts, then push. Never force push unless absolutely necessary.
- Commit messages are concise and straight to the point. No long verbose messages.

### Working on Tasks

- Never make assumptions and act on them. Interview the user relentlessly about every unclear or under-defined aspect until you reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one by one.
- If a question can be answered by exploring the codebase, explore the codebase instead.
- For each question, provide your recommended answer.
- If a task is complex enough, write an implementation plan first and discuss all aspects of the solution before implementing.
- If a fix is very complex: write an elaborate implementation plan, question the user relentlessly about every aspect of the design, then split the implementation into self-contained phases — each with a clear goal and tests to verify it — that can ideally run in parallel across multiple agents.

### Searching

- Never hallucinate an answer. Find the definitive answer or ask the user for more information.
- For internet searches, always actually search and show the links used to find the information.
