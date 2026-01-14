# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Building and Installation

```bash
# Build multiplexer version (includes embedded v3-v6 binaries) into ./build/
make build

# Build standalone v7-only version
make build-standalone

# Install multiplexer version to $GOPATH/bin
make install

# Install standalone version
make install-standalone

# Update all go.mod files
make mod
```

### Testing

```bash
# Run all tests (30 minute timeout)
make test

# Run tests for specific packages
make test PACKAGES="./app/..."

# Run tests in short mode (1 minute timeout)
make test-short

# Run tests with race detection (15 minute timeout)
make test-race

# Run benchmark tests
make test-bench

# Generate test coverage
make test-coverage

# Run single test
go test -v -run TestName ./path/to/package

# Run docker-based E2E tests (requires test variable)
make test-docker-e2e test=TestE2ESimple

# Run upgrade test for all app versions from v2
make test-docker-e2e-upgrade-all

# Run multiplexer tests (requires embedded binaries)
make test-multiplexer
```

### Linting and Formatting

```bash
# Run all linters (golangci-lint, markdownlint, hadolint, yamllint)
make lint

# Auto-fix linting issues
make lint-fix

# Check for modernize issues
make modernize-check

# Apply modernize fixes automatically
make modernize-fix
```

### Protobuf

```bash
# Generate Protobuf files (requires Docker)
make proto-gen

# Format Protobuf files
make proto-format

# Lint Protobuf files
make proto-lint

# Check for breaking Protobuf changes
make proto-check-breaking
```

### Docker

```bash
# Build celestia-appd Docker image with multiplexer support
make build-docker-multiplexer

# Build standalone Docker image
make build-docker-standalone
```

### Testing with Single Node

```bash
# Start a single node local testnet
./scripts/single-node.sh

# Publish blob data to local testnet
celestia-appd tx blob pay-for-blob 0x00010203040506070809 0x48656c6c6f2c20576f726c6421 \
  --chain-id test \
  --from validator \
  --keyring-backend test \
  --fees 21000utia \
  --yes
```

## High-Level Architecture

celestia-app is a Cosmos SDK-based blockchain application that implements Celestia's data availability layer. It runs on top of celestia-core (a fork of CometBFT).

### Key Components

```text
┌─────────────────────────────────────┐
│      celestia-app (v7)              │
│  Cosmos SDK Application             │
│  - State machine                    │
│  - Transaction handling             │
│  - Blob processing                  │
└──────────────┬──────────────────────┘
               │ ABCI Interface
┌──────────────▼──────────────────────┐
│  celestia-core (v0.39.22)           │
│  CometBFT fork                      │
│  - Consensus                        │
│  - P2P Networking                   │
│  - Block Production                 │
└─────────────────────────────────────┘
```

### Directory Structure

- **`/app`**: Application core (state machine logic)
  - `app.go`: Main application struct with keeper initialization
  - `prepare_proposal.go`: Block proposal creation with data square building
  - `process_proposal.go`: Block validation (verifies data root and blob validity)
  - `ante/`: Transaction validation decorators (15 files)
  - `encoding/`: Transaction codec configuration

- **`/x`**: Custom Cosmos SDK modules
  - `x/blob`: Blob submission and payment (MsgPayForBlobs)
  - `x/signal`: Network upgrade coordination
  - `x/minfee`: Minimum gas price governance
  - `x/mint`: Custom inflation module

- **`/pkg`**: Reusable protocol packages
  - `pkg/appconsts`: Application-wide constants and versioned parameters
  - `pkg/da`: Data availability header computation
  - `pkg/wrapper`: Namespace Merkle Tree (NMT) integration
  - `pkg/user`: User-facing transaction APIs and signing
  - `pkg/inclusion`: Blob inclusion proof generation
  - `pkg/proof`: Merkle proof structures

- **`/multiplexer`**: Multi-version upgrade system that embeds v3-v6 binaries
- **`/cmd/celestia-appd`**: Main binary entry point
- **`/test/util`**: Testing utilities (testnode, blobfactory, testfactory)
- **`/internal/embedding`**: Embedded v3-v6 binaries (tar.gz archives)

### BlobTx: Core Data Structure

A `BlobTx` is the primary transaction type for submitting data to Celestia:

```text
BlobTx
├─ Tx (sdk.Tx containing MsgPayForBlobs)
│  └─ MsgPayForBlobs
│     ├─ signer: Bech32 address
│     ├─ namespaces: []Namespace (29 bytes: 1 byte version + 28 bytes ID)
│     ├─ blob_sizes: []uint32
│     ├─ share_commitments: [][]byte (subtree root merkle proofs)
│     └─ share_versions: []uint32
└─ Blobs: []*Blob (actual data payloads)
```

When a `BlobTx` is processed:

1. Block proposer separates the `sdk.Tx` from the blob data
2. `MsgPayForBlobs` goes into `PayForBlobNamespace` (reserved)
3. Blob data goes into user-specified namespace in data square

### Data Square and Shares

**Shares**: Fixed 512-byte units that compose blocks

- First 29 bytes: Namespace (8-bit version + 28-bit ID)
- Remaining bytes: Data
- Multiple shares compose blobs; padding shares fill empty space

**Data Square**: 2D k×k matrix arranged in row-major order

1. Reserved namespaces contain system data (transactions, PFB, parity)
2. User blobs follow in namespace order
3. Extended via Reed-Solomon erasure coding to 2k×2k for data availability
4. Each row/column becomes an NMT (Namespaced Merkle Tree) leaf

**DataAvailabilityHeader (DAHeader)**:

- RowRoots: Merkle roots of each row in extended data square (EDS)
- ColumnRoots: Merkle roots of each column in EDS
- Hash: Merkle(RowRoots || ColumnRoots) = Data Root
- Enables light clients to verify data availability

### Block Lifecycle (ABCI)

**PrepareProposal** (`/app/prepare_proposal.go`):

1. Receive transactions from mempool
2. Separate BlobTxs from standard txs
3. Run ante handler validation on each tx
4. Build k×k data square (FilteredSquareBuilder)
5. Erasure code to 2k×2k extended data square
6. Compute DAHeader (row/column roots + data root)
7. Return: filtered_txs, square_size, data_root_hash

**ProcessProposal** (`/app/process_proposal.go`):

1. Validate each transaction (size, signatures, blob commitments)
2. Reconstruct extended data square from all txs
3. Verify: computed_square_size == proposed_square_size
4. Recompute DAHeader and verify: computed_data_root == proposed_data_root
5. Accept or reject block

### Ante Handler Chain

Every transaction passes through 19 decorator steps in `/app/ante/`:

1. **SetUpContext**: Initialize gas meter
2. **ValidateBasic**: Call Message ValidateBasic()
3. **ConsumeGasForTxSize**: Charge 10 gas per tx byte
4. **DeductFee**: Verify fees and check minimum gas price
5. **SigVerification**: Verify signatures and sequence numbers
6. **MinGasPFB**: For BlobTx, verify 8 gas per blob byte
7. **BlobShareDecorator**: Verify blob shares fit in square
8. **IncrementSequence**: Increment account nonce
9. (Plus 11 other decorators for various validations)

### Multiplexer vs Standalone Builds

**Standalone** (`make build-standalone`):

- Only v7 native application
- No embedded binaries
- Lighter deployment
- Cannot sync from genesis through multiple versions

**Multiplexer** (`make build` - default):

- Embeds v3.10.6, v4.1.0, v5.0.12, v6.4.4 binaries
- Single CometBFT instance handles all versions
- Automatically switches versions on upgrade
- Enables syncing from genesis without stopping node
- Defined in `/multiplexer/` and `/internal/embedding/`

Build tags:

- Standalone: `ledger`
- Multiplexer: `ledger,multiplexer`

### Testing Infrastructure

**testnode** (`/test/util/testnode/`):

- `NewNetwork()`: Starts single validator with RPC/gRPC
- Auto-configures ports, creates genesis, funds accounts
- Use in integration tests for full node setup

**blobfactory** (`/test/util/blobfactory/`):

- `RandBlobTxsWithAccounts()`: Generate random blob transactions
- `RandMsgPayForBlobs()`: Generate random MsgPayForBlobs
- Configurable namespace, blob sizes, signers

**Test patterns**:

```go
// Integration test example
func TestIntegration(t *testing.T) {
    app, _ := testnode.NewNetwork(t, testnode.DefaultConfig())
    // Use app.Accounts, app.NodeClient for testing
}
```

### Key Files for Common Tasks

**Understanding block production**:

- `/app/prepare_proposal.go`: How blocks are proposed
- `/app/filtered_square_builder.go`: How data square is constructed
- `/pkg/da/data_availability_header.go`: DAHeader computation

**Understanding blob validation**:

- `/x/blob/types/payforblob.go`: MsgPayForBlobs validation
- `/x/blob/ante/`: Blob-specific ante decorators
- `/app/process_proposal.go`: Block-level blob validation

**Understanding transaction flow**:

- `/app/ante/ante.go`: Complete ante handler chain
- `/pkg/user/tx_client.go`: User-facing transaction submission
- `/pkg/user/signer.go`: Transaction signing with sequence management

**Module initialization**:

- `/app/app.go`: Application setup and keeper initialization
- `/app/modules.go`: Module ordering (BeginBlock, EndBlock, InitGenesis)

### Important Constants

From `/pkg/appconsts/app_consts.go`:

- `Version = 7`: Current app version
- `MaxTxSize = 8388608`: 8 MiB maximum transaction size
- `SubtreeRootThreshold = 64`: Shares per subtree for share commitment
- `ShareSize = 512`: Bytes per share
- `NamespaceSize = 29`: Bytes per namespace (1 version + 28 ID)

### Dependency Branches

| celestia-app | celestia-core      | cosmos-sdk                 |
|--------------|--------------------|----------------------------|
| `main`       | `main`             | `release/v0.51.x-celestia` |
| `v6.x`       | `v0.39.x-celestia` | `release/v0.51.x-celestia` |
| `v5.x`       | `v0.38.x-celestia` | `release/v0.50.x-celestia` |
| `v4.x`       | `v0.38.x-celestia` | `release/v0.50.x-celestia` |
| `v3.x`       | `v0.34.x-celestia` | `release/v0.46.x-celestia` |

All use forked versions of cosmos-sdk and celestia-core with custom modifications.

### Development Workflow

1. **Multi-module repository**: Rename `go.work.example` to `go.work` and run `go work sync`
2. **Follow test-first development**: Write tests before implementation (see copilot instructions)
3. **Use conventional commits**: PR titles must start with `feat:`, `fix:`, `chore:`, etc.
4. **Breaking changes**: Include `!` in commit type (e.g., `feat!:`) for breaking changes
5. **Run linters before committing**: `make lint-fix`

### Security Considerations

From `.github/copilot-instructions.md`:

- Always validate user inputs, especially in message handlers
- Be cautious with arithmetic operations that could overflow
- Verify permissions and authority before state modifications
- Consider replay attacks and ensure proper nonce/sequence handling
- Be mindful of gas consumption and potential DoS vectors

### Conventional Commits

PR titles and commits should follow <https://www.conventionalcommits.org/>:

- `feat:` - New features
- `fix:` - Bug fixes
- `chore:` - Maintenance tasks
- `refactor:` - Code refactoring
- `test:` - Test additions/modifications
- `docs:` - Documentation changes
- `ci:` - CI/CD changes
- `perf:` - Performance improvements
- Use `!` for breaking changes (e.g., `feat!:`)
