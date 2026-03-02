# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Building

When editing Go code, always run `make build` after changes to catch compilation errors immediately.

```bash
make build              # Build multiplexer version (embeds v3-v6 binaries) into ./build/
make build-standalone   # Build v7-only version (no embedded binaries)
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
- **`/multiplexer`** - Multi-version upgrade system embedding v3-v6 binaries
- **`/cmd/celestia-appd`** - Binary entry point
- **`/test/util`** - Test utilities: `testnode`, `blobfactory`, `testfactory`

### Multiplexer vs Standalone

- **`make build`** (default): Multiplexer build embeds v3-v6 binaries, enables syncing from genesis through all upgrades. Build tag: `ledger,multiplexer`
- **`make build-standalone`**: v7-only, lighter. Build tag: `ledger`

### Dependency Forks

All branches use forked cosmos-sdk and celestia-core:

| celestia-app | celestia-core      | cosmos-sdk                 |
|--------------|--------------------|----------------------------|
| `main`       | `main`             | `release/v0.51.x-celestia` |
| `v6.x`       | `v0.39.x-celestia` | `release/v0.51.x-celestia` |
| `v5.x`       | `v0.38.x-celestia` | `release/v0.50.x-celestia` |
| `v4.x`       | `v0.38.x-celestia` | `release/v0.50.x-celestia` |
| `v3.x`       | `v0.34.x-celestia` | `release/v0.46.x-celestia` |

## Development Workflow

1. **Multi-module repo**: Copy `go.work.example` to `go.work` and run `go work sync`
2. **Conventional commits**: PR titles must follow [conventionalcommits.org](https://www.conventionalcommits.org/) (e.g., `feat:`, `fix:`, `chore:`, `feat!:` for breaking changes)
3. **Validate inputs** in message handlers; be cautious with arithmetic overflow and gas consumption
