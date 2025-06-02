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

Celestia-app v4.0.0 includes significant updates to the underlying state machine due to major dependency upgrades: **Cosmos SDK** (v0.46.x → v0.50.x), **IBC** (v6 → v8), and **CometBFT** (v0.34 → v0.38).

#### Module Changes

**New Modules**:
- **`x/circuit`**: Circuit breaker module for emergency halting of message processing
  - `MsgAuthorizeCircuitBreaker`, `MsgTripCircuitBreaker`, `MsgResetCircuitBreaker`
  - Automatic blocking of upgrade-related messages during emergencies
- **`x/consensus`**: Consensus parameters module (migrated from CometBFT core)
  - `MsgUpdateParams`: Update consensus parameters via governance
  - `QueryParamsRequest`/`QueryParamsResponse`: Query consensus parameters
- **`hyperlane/core`**: Hyperlane interoperability protocol core
  - Interchain Security Module (ISM) configuration messages
  - Mailbox and validator set management, cross-chain communication
- **`hyperlane/warp`**: Hyperlane token bridge module
  - Token bridging and routing messages for cross-chain transfers

**Removed Modules**:
- **`x/capability`**: Removed from Cosmos SDK v0.50.x, replaced with enhanced authentication in IBC v8
- **`x/crisis`**: Removed from Cosmos SDK v0.50.x, functionality replaced by circuit breaker
- **`x/paramfilter`**: Celestia-specific module removed in favor of circuit breaker

#### Key Changes and Updates

**Parameter Management Migration**:
- **`x/params` module**: Deprecated in favor of module-specific `MsgUpdateParams` messages
- **Consensus parameters**: Migrated from CometBFT core to dedicated `x/consensus` module  
- **Governance**: Expedited minimum deposit increased to 50,000 TIA; enhanced validation through circuit breaker integration

**IBC Protocol Enhancements (v6 → v8)**:
- Enhanced packet validation, routing, and acknowledgment handling
- Improved light client verification and state management  
- Strengthened connection establishment and authentication protocols

### Library Consumers (v4.0.0)

Library consumers will need to update imports and APIs due to major dependency upgrades:

**New Module Imports**:
- `cosmossdk.io/x/circuit` (circuit breaker)
- `cosmossdk.io/x/consensus` (consensus parameter management)
- `github.com/bcp-innovations/hyperlane-cosmos/x/core` and `x/warp` (Hyperlane)

**Removed Module Imports**:
- `x/capability`, `x/crisis` (removed from Cosmos SDK v0.50.x)
- `x/paramfilter` (Celestia-specific module removed)

**API Changes**:
- Parameter updates now use module-specific `MsgUpdateParams` instead of generic param proposals
- IBC modules updated to `github.com/cosmos/ibc-go/v8` with modified interfaces
- Cosmos SDK v0.50.x includes updated module interfaces, query APIs, and transaction building patterns
- TxClient interface updates and user package modifications from dependency upgrades

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
