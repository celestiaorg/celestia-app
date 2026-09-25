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

For test-related tasks: 1) write the test, 2) run it to verify it passes, 3) for tests involving timing, concurrency, or networking, check for flakiness with `-count=N`.

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
- **`/x`** - Custom modules: `blob` (MsgPayForBlobs), `signal` (upgrades), `minfee` (gas price governance), `mint` (inflation), `fibre`, `valaddr`, `forwarding` (Hyperlane forwarding), `zkism`
- **`/pkg`** - Reusable packages: `appconsts`, `da`, `wrapper` (NMT), `user` (tx APIs), `inclusion`, `proof`
- **`/multiplexer`** - Multi-version upgrade system embedding v3-v9 binaries
- **`/cmd/celestia-appd`** - Binary entry point
- **`/test/util`** - Test utilities: `testnode`, `blobfactory`, `testfactory`

### Multiplexer vs Standalone

- **`make build`** (default): Multiplexer build embeds v3-v9 binaries, enables syncing from genesis through all upgrades. Build tag: `ledger,multiplexer`
- **`make build-standalone`**: v10-only, lighter. Build tag: `ledger`

The fibre and valaddr modules are compiled into every build by default. The module code lives under `x/fibre/` and `x/valaddr/`, wired into the app via `app/modules.go` and `app/app.go`.

### Dependency Forks

All branches use forked cosmos-sdk and celestia-core. The exact versions are pinned in the `replace` block of each branch's `go.mod`.

## Development Workflow

1. **Multi-module repo**: Copy `go.work.example` to `go.work` and run `go work sync`
2. **Conventional commits**: PR titles must follow [conventionalcommits.org](https://www.conventionalcommits.org/) (e.g., `feat:`, `fix:`, `chore:`, `feat!:` for breaking changes). Any consensus-breaking change (one that alters deterministic state-machine behavior and requires a coordinated network upgrade) must include a `!` in the PR title, e.g. `fix!:`.
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

- Answer questions from the codebase first. Ask the engineer only when different readings of the request would lead to materially different code. Give a recommended answer with each question.
- For risky-tier work, get the engineer's confirmation on the plan before writing code. A wrong assumption in consensus code can halt the chain.
- If a task is complex enough, write an implementation plan first and discuss it with the engineer before implementing.
- If a fix is very complex, split the implementation into self-contained phases, each with a clear goal and tests to verify it, that can ideally run in parallel across multiple agents.

### Searching

- Cite the file path or URL behind factual claims. If you can't find a definitive answer, say so.
- For internet searches, actually search and show the links used to find the information.
