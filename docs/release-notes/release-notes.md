# Release Notes

This guide provides notes for major version releases. These notes may be helpful for users when upgrading from previous major versions.

## v4.0.0

### Node Operators (v4.0.0)

#### Multiplexer

Celestia-app v4.0.0 introduces support for a [multiplexer](https://github.com/celestiaorg/celestia-app/tree/e5d5ac6732c55150ea3573e17bec162fe836e0c6/multiplexer) that makes it easier for node operators to run a consensus node that can sync from genesis. The multiplexer contains an embedded celestia-app v3.x.x binary that will be used to sync the node from genesis. After the chain advances to app version v4, the multiplexer will stop routing requests to the embedded celestia-app v3.x.x binary and will instead route requests to the the v4.x.x state machine. Binaries that are installed from source (via `make install`) will include support for the multiplexer. To install Celestia without the multiplexer, you can use the `make install-standalone` target. Note that the standalone binary will only be able to run on networks that have already upgraded to app version v4.

#### `rpc.grpc_laddr`

The `rpc.grpc_laddr` config option is now required when running the celestia-app binary with the multiplexer. This option can be set via CLI flag `--rpc.grpc_laddr tcp://127.0.0.1:9098` or in the `config.toml`:

```toml
[rpc]

# TCP or UNIX socket address for the gRPC server to listen on
# NOTE: This server only supports /broadcast_tx_commit
grpc_laddr = "tcp://127.0.0.1:9098"
```

### State Machine Changes (v4.0.0)

Celestia-app v4.0.0 includes significant updates to the underlying state machine due to major dependency upgrades. These upgrades introduce updates to messages, data structures, field types, and the logic behind some calculations.

#### Dependency Upgrades

- **Cosmos SDK**: v0.46.x → v0.50.x
- **IBC**: v6 → v8

#### New Modules

The following modules were added in v4.0.0:

- **`x/circuit`**: Circuit breaker module for emergency halting of message processing
- **`x/consensus`**: Consensus parameters module (migrated from CometBFT core)
- **`hyperlane/core`**: Hyperlane interoperability protocol core module
- **`hyperlane/warp`**: Hyperlane token bridge module

#### Removed Modules

The following modules were removed in v4.0.0:

- **`x/capability`**: Removed from Cosmos SDK v0.50.x
- **`x/crisis`**: Removed from Cosmos SDK v0.50.x  
- **`x/paramfilter`**: Celestia-specific module removed in favor of circuit breaker

#### New Messages and Data Structures

**Circuit Breaker Module (`cosmossdk.io/x/circuit`)**:
- `MsgAuthorizeCircuitBreaker`: Authorize an account as a circuit breaker
- `MsgTripCircuitBreaker`: Trip (disable) a circuit breaker for specific message types  
- `MsgResetCircuitBreaker`: Reset (re-enable) a circuit breaker
- Circuit breaker state tracking and emergency halt capabilities

**Consensus Module (`x/consensus`)**:
- `MsgUpdateParams`: Update consensus parameters (block size, gas limits, timeouts) via governance
- `QueryParamsRequest`/`QueryParamsResponse`: Query current consensus parameters
- Replaces legacy parameter management from CometBFT core

**Hyperlane Core Module (`hyperlane/core`)**:
- Interchain Security Module (ISM) configuration messages
- Mailbox and validator set management messages  
- Message dispatch and delivery tracking
- Cross-chain communication protocol messages

**Hyperlane Warp Module (`hyperlane/warp`)**:
- Token bridging and routing messages for cross-chain transfers
- Wrapped token management and transfer protocols
- Cross-chain asset movement capabilities

#### Deprecated Properties

The following parameters and fields have been deprecated or removed:

**Legacy Parameter Management**:
- **`x/params` module**: Parameter management moved to individual modules with dedicated `MsgUpdateParams` messages
- **BaseApp parameters**: Moved from params module to `x/consensus` module
- **Module-specific parameters**: Each module now manages its own parameters independently

**Removed Crisis Module Features**:
- **Crisis invariants**: No longer supported, replaced by circuit breaker mechanisms
- **Emergency halt via crisis**: Now handled by circuit breaker module

**IBC Capability System**:
- **`x/capability` module**: Removed in favor of direct object-capability security model
- **Capability-based IBC authentication**: Replaced with enhanced authentication in IBC v8

**Paramfilter Module**:
- **`x/paramfilter`**: Entire module removed, parameter filtering now handled through governance and circuit breaker mechanisms

#### Updated Logic and Calculations

**Governance Parameter Updates**:
- Expedited minimum deposit increased to 50,000 TIA (from previous defaults)
- Module-specific governance: Each module now handles its own parameter updates via dedicated `MsgUpdateParams` messages
- Enhanced governance validation through circuit breaker integration

**Circuit Breaker Integration**:
- Automatic blocking of upgrade-related messages: `MsgSoftwareUpgrade`, `MsgCancelUpgrade`, `MsgIBCSoftwareUpgrade`
- Emergency halt capabilities for specific message types during security incidents
- Granular control over message processing during emergencies

**Consensus Parameter Migration and Management**:
- Consensus parameters migrated from legacy CometBFT core to dedicated `x/consensus` module
- Enhanced validation and governance control over block size, gas limits, and timeouts
- Version management now handled at application level with improved upgrade coordination

**IBC Protocol Enhancements (v6 → v8)**:
- **Packet Processing**: Improved packet validation, routing, and acknowledgment handling
- **Client Management**: Enhanced light client verification and state management
- **Connection Security**: Strengthened connection establishment and authentication protocols
- **Channel Management**: Improved channel lifecycle management and error handling

**Cosmos SDK Module Updates (v0.46.x → v0.50.x)**:
- **Authentication**: Enhanced signature verification and account management
- **Bank Module**: Improved multi-token support and transfer validation
- **Staking**: Updated validator selection and slashing mechanisms
- **Distribution**: Modified reward calculation and distribution logic
- **Gov Module**: Enhanced proposal validation and voting mechanisms

#### Gas and Fee Changes

**New Gas Calculations**:
- Circuit breaker operations: Gas costs for authorizing, tripping, and resetting circuit breakers
- Consensus parameter updates: Gas costs for governance-based consensus parameter modifications
- Hyperlane operations: Gas calculations for cross-chain messaging and token bridging

**Updated Fee Structures**:
- Module-specific parameter updates: Each module's `MsgUpdateParams` has dedicated gas costs
- Enhanced IBC fee handling: Updated fee calculations for IBC v8 packet processing and acknowledgments
- Cross-chain operations: New fee structures for Hyperlane-based interoperability features

**Gas Optimization**:
- Improved gas estimation for complex transactions involving multiple modules
- Optimized consensus parameter validation to reduce gas overhead
- Enhanced efficiency in IBC packet processing and state verification

### Library Consumers (v4.0.0)

If you are a library consumer, several Go APIs have changed due to the major dependency upgrades from v3 to v4:

#### Module Import Changes

- **Circuit Breaker**: New import `cosmossdk.io/x/circuit` for emergency halt capabilities
- **Consensus**: New import `cosmossdk.io/x/consensus` for consensus parameter management
- **Hyperlane**: New imports for cross-chain functionality:
  - `github.com/bcp-innovations/hyperlane-cosmos/x/core`
  - `github.com/bcp-innovations/hyperlane-cosmos/x/warp`

#### Removed Module Imports

- **`x/capability`**: No longer available, removed from Cosmos SDK v0.50.x
- **`x/crisis`**: No longer available, functionality replaced by circuit breaker
- **`x/paramfilter`**: Celestia-specific module removed in v4

#### API Changes

**Parameter Management**:
- Parameter updates now use module-specific `MsgUpdateParams` instead of generic param proposals
- `x/params` module deprecated in favor of individual module parameter management
- Consensus parameters moved from CometBFT to `x/consensus` module

**IBC Updates (v6 → v8)**:
- Updated import paths for IBC modules: `github.com/cosmos/ibc-go/v8`
- Modified packet and acknowledgment handling interfaces
- Enhanced client and connection management APIs

**Cosmos SDK Updates (v0.46.x → v0.50.x)**:
- Updated module interfaces and keeper patterns
- Modified query and transaction building APIs
- Enhanced error handling and validation patterns

## v3.0.0

### Node Operators (v3.0.0)

#### Enabling BBR and MCTCP

Consensus node operators must enable the BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm. See [#3774](https://github.com/celestiaorg/celestia-app/pull/3774).
If using Linux in Docker, Kubernetes, a VM or bare-metal, this can be done by calling

```sh
make enable-bbr
```

command on the host machine.

#### Configure Node for V3

Consensus node operators should update several configurations for v3. This can be done by calling:

```sh
make configure-v3
```

If the config file is not in the default spot, it can be provided using:

```sh
make configure-v3 CONFIG_FILE=path/to/other/config.toml
```

**Alternatively**, the configurations can be changed manually. This involves updating the mempool TTLs and the send and receive rates.

- Configuring Bandwidth Settings
  - Update `recv_rate` and `send_rate` in your TOML config file to 10MiB (10485760)
- Extend TTLs
  - Update `ttl-num-blocks` in your TOML config file to 12

#### Signaling Upgrades

- Upgrades now use the `x/signal` module to coordinate the network to an upgrade height.

The following command can be used, if you are a validator in the active set, to signal to upgrade to v3:

```bash
celestia-appd tx signal signal 3 <plus transaction flags>
```

You can track the tally of signalling by validators using the following query

```bash
celestia-appd query signal tally 3
```

Once 5/6+ of the voting power have signalled, the upgrade will be ready. There is a hard coded delay between confirmation of the upgrade and execution to the new state machine.

To view the upcoming upgrade height use the following query:

```bash
celestia-appd query signal upgrade
> An upgrade is pending to app version 3 at height 2348907.
```

For more information refer to the module [docs](../../x/signal/README.md)

### Library Consumers (v3.0.0)

- Namespace and share constants in the `appconsts` package were moved to [celestiaorg/go-square](https://github.com/celestiaorg/go-square). See [#3765](https://github.com/celestiaorg/celestia-app/pull/3765).

## [v2.0.0](https://github.com/celestiaorg/celestia-app/releases/tag/v2.0.0)

### Node Operators (v2.0.0)

If you are a consensus node operator, please follow the communication channels listed under [network upgrades](https://docs.celestia.org/how-to-guides/participate#network-upgrades) to learn when this release is recommended for each network (e.g. Mocha, Mainnet Beta).

Consensus node operators are expected to upgrade to this release _prior_ to the Lemongrass hardfork if they intend to continue participating in the network. The command used to start the [consensus node](https://docs.celestia.org/how-to-guides/consensus-node#start-the-consensus-node) or [validator node](https://docs.celestia.org/how-to-guides/validator-node#run-the-validator-node) will accept an additional `--v2-upgrade-height` flag. See [this table](https://docs.celestia.org/how-to-guides/network-upgrade-process#lemongrass-network-upgrade) for upgrade heights for each network.

Consensus node operators should enable the BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm. See [#3812](https://github.com/celestiaorg/celestia-app/pull/3812).

### Library Consumers (v2.0.0)

If you are a library consumer, a number of the Go APIs have changed since celestia-app v1.x.x. Some of the notable changes are:

- Code pertaining to the original data square was extracted to [celestiaorg/go-square](https://github.com/celestiaorg/go-square).
  - celestia-app v1.x had a shares package. celestia-app v2.x uses [go-square/shares](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/shares)
  - celestia-app v1.x had a blob.types package with `CreateCommitment` function. celestia-app v2.x uses `CreateCommitment` function from the [go-square/inclusion](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/inclusion).
- celestia-app v1.x had a lot of functionality included in the signer. celestia-app v2.x splits a txClient from the signer. See [#3433](https://github.com/celestiaorg/celestia-app/pull/3433).
