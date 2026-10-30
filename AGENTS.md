# AGENTS.md

## Commands

### Building

When editing Go code, always run `make build` after changes to catch compilation errors immediately.

```bash
make build              # Build multiplexer version (embeds v3-v10 binaries) into ./build/
make build-standalone   # Build v11-only version (no embedded binaries)
make mod                # Update all go.mod files
```

### Testing

For tests involving timing, concurrency, or networking, check for flakiness with `-count=N`.

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

### Multiplexer vs Standalone

- **`make build`** (default): Multiplexer build embeds v3-v10 binaries, enables syncing from genesis through all upgrades. Build tag: `ledger,multiplexer`
- **`make build-standalone`**: v11-only, lighter. Build tag: `ledger`

### Dependency Forks

All branches use forked cosmos-sdk and celestia-core. The exact versions are pinned in the `replace` block of each branch's `go.mod`.

## Development Workflow

1. **Multi-module repo**: Copy `go.work.example` to `go.work` and run `go work sync`
2. **Conventional commits**: PR titles must follow [conventionalcommits.org](https://www.conventionalcommits.org/) and start the subject with exactly one review thoroughness tag: `[low]`, `[medium]`, or `[high]` (e.g., `docs: [low] clarify setup`, `fix(api): [medium] handle missing input`, `fix!: [high] correct gas accounting`). Any consensus-breaking change (one that alters deterministic state-machine behavior and requires a coordinated network upgrade) must include a `!` in the PR title, e.g. `fix!: [high] correct gas accounting`. Choose the tag using the PR rules below.
3. **Linking issues**: PR descriptions must start with a `Closes <link>` line when an issue exists, and the link must be clickable. Linear issues use `Closes [PROTOCO-1234](https://linear.app/celestia/issue/PROTOCO-1234)` — a bare `Closes PROTOCO-1234` is not acceptable because GitHub does not linkify it.
4. **Hacken bug bounty PRs**: When creating a PR that resolves a Hacken bug bounty report, do NOT include details about the bug in the PR description. Instead, link to a Linear issue (as a clickable link, per the previous item) that contains more details on the bug and the link to the Hacken bug bounty report.

## AI Safety Invariants

@docs/ai/invariants.md

Every code change must respect the invariants in [docs/ai/invariants.md](docs/ai/invariants.md), imported above. If your tool does not expand the import, read that file before changing `x/`, `app/`, `pkg/`, `proto/`, or `multiplexer/`. If a task cannot be done without violating one, stop and ask the engineer.

## AI Workflow

- **Risk tiers**: changes touching `x/`, `app/`, `pkg/`, `proto/`, or `multiplexer/` are risky; docs, test-only changes, scripts, tooling, and `.github/` are light; risky changes that modify a state transition reachable from ABCI are consensus-critical and also get an `adversarial-reviewer` deep review. When in doubt, treat as the higher tier.
- **Interactive gate**: before implementing or reviewing any change, triage the tier. For light changes, state the tier in one line and proceed. For risky and consensus-critical changes, ask the engineer whether to run the invariant workflow (the `/implement` skill, or the reviewer agents in `.claude/agents/` for a review). Give a 2–3 line overview — tier, paths touched, what the workflow would add — and recommend running it. Skip the question when the engineer invoked `/implement` directly. In non-interactive runs, run it.
- **Declined workflow on a risky change**: still state assumptions and get engineer confirmation before writing code; no reviewer agents unless the engineer asks.
- **Assumptions notes** live in `docs/plans/` and are never committed.
- **Working notes are never committed**: implementation plans, review or audit findings, investigation logs, and similar agent working documents (e.g. `docs/<topic>-review.md`) go in the gitignored `docs/plans/` or the PR description, not in the diff. Before opening or updating a PR, check the diff for such files and remove them.

## Pull Requests

- Follow [README.md#contributing](README.md#contributing) for review levels. Use the higher level when unsure and reassess if scope changes.
- Don't repeat the review tag in the PR description; the title already carries it.
- Keep PRs under 700 lines of code; propose independently reviewable splits for larger changes.
- When you open a PR, assign it to the engineer you are working for (their GitHub login, e.g. from the GitHub `get_me` tool).
