# Release Notes

This guide provides notes for major version releases. These notes may be helpful for users when upgrading from previous major versions.

## Upcoming Major Release

## v6.0.0 (Unreleased)

This release targets throughput, blob size, inflation reduction, and fee changes.

### Config changes

It introduces a new block propagation reactor and configuration changes to accommodate the increased throughput. The relevant v6 configuration changes can be applied to existing config using the `celestia-appd update-config` command or by manually updating the config.toml and app.toml.

To modify your existing configs, the `celestia-appd update-configs` command can be used.

```
celestia-appd update-config
```

this uses version 6 and the default home (.celestia-app). Those can be changed or specified with flags as well.

```
celestia-appd update-config --version 6 --home ~/.celestia-app
```

To manually modify the configs, change the following values.

```toml
[rpc]
max_body_bytes = 436207616

[p2p]
send_rate = 25165824
recv_rate = 25165824

[mempool]
type = "cat"
max_tx_bytes = 8388608
ttl-duration = "0s"
ttl-num-blocks = 12
max-gossip-delay = "1m0s"
```

## v5.0.0

This major upgrade is an expedited patch release, fixing the problem with failed IBC transfers caused by the incorrectly configured capability module. There should be no additional API breaking changes.
This expedited release will have no upgrade delay. The moment 5/6ths signal and the `MsgTryUpgrade` is successful, the network will upgrade to v5.

## v4.0.0

### Node Operators (v4.0.0)

Node operators MUST upgrade their binary to this version prior to the v4 activation height. Node operators SHOULD NOT use cosmovisor to upgrade their binary.

#### Multiplexer

Celestia-app v4.0.0 introduces support for a [multiplexer](https://github.com/celestiaorg/celestia-app/tree/e5d5ac6732c55150ea3573e17bec162fe836e0c6/multiplexer) that makes it easier for node operators to run a consensus node that can sync from genesis. The multiplexer contains an embedded celestia-app v3.x.x binary that will be used to sync the node from genesis. After the chain advances to app version v4, the multiplexer will stop routing requests to the embedded celestia-app v3.x.x binary and will instead route requests to the v4.x.x state machine. Binaries that are installed from source (via `make install`) will include support for the multiplexer. To install Celestia without the multiplexer, you can use the `make install-standalone` target. Note that the standalone binary will only be able to run on networks that have already upgraded to app version v4.

#### `proxy_app` and `address`

- The default ABCI client address is now `tcp://127.0.0.1:36658` (configured via `--proxy_app` flag or `proxy_app` in config.toml).
- The default ABCI server address is now `tcp://127.0.0.1:36658` (configured via `--address` flag).

These two configs must match in order for the multiplexer to work correctly. Please update your config.toml to account for the new default

```diff
-proxy_app = "tcp://127.0.0.1:26658"
+proxy_app = "tcp://127.0.0.1:36658"
```

#### Custom build flags

`make install` currently downloads a v3.x binary with only one custom build flag, `ledger`. If you use any additional custom build flags (i.e. `pebbledb`, `rocksdb`, `badgerdb`, `cleveldb`, `boltdb`), you will need to build the v3.x binary from source (with custom build tags) and include it in the app's embedded binary directory (by default: `~/.celestia-app/bin/`). The embedded binary directory layout:

```bash
$ tree bin
bin
└── v3.10.2-mocha
    ├── celestia-appd
    ├── LICENSE
    └── README.md
```

#### `rpc.grpc_laddr`

The `rpc.grpc_laddr` config option is now required when running the celestia-app binary with the multiplexer. This option can be set via CLI flag `--rpc.grpc_laddr tcp://127.0.0.1:9098` or in the `config.toml`:

```toml
[rpc]

# TCP or UNIX socket address for the gRPC server to listen on
# NOTE: This server only supports /broadcast_tx_commit
grpc_laddr = "tcp://127.0.0.1:9098"
```

#### IAVL v1 Migration

Celestia-app v4 uses IAVL v1 for better performance. When upgrading to v4, the migration happens lazily over time. If you'd like to avoid the lazy migration, you can perform a fresh state sync so that your node uses IAVL v1 exclusively.

#### Cosmos SDK default addresses

The default addresses for the Cosmos SDK API server, GRPC server, and GRPC web server have changed from `0.0.0.0` to `localhost`. See [cosmos-sdk#13778](https://github.com/cosmos/cosmos-sdk/pull/13778).

### State Machine Changes (v4.0.0)

Celestia-app v4.0.0 includes significant state machine changes due to major dependency upgrades:

- Cosmos SDK v0.46.16 to v0.50.12
  - [API breaking changes](https://github.com/cosmos/cosmos-sdk/blob/release/v0.50.x/CHANGELOG.md#api-breaking-changes)
  - [Client breaking changes](https://github.com/cosmos/cosmos-sdk/blob/release/v0.50.x/CHANGELOG.md#client-breaking-changes)
  - [CLI breaking changes](https://github.com/cosmos/cosmos-sdk/blob/release/v0.50.x/CHANGELOG.md#cli-breaking-changes)
- IBC v6.2.2 to v8.7.0
  - [IBC v8.7.0 release notes](https://github.com/cosmos/ibc-go/releases/tag/v8.7.0)
- CometBFT v0.34.35 to v0.38.17
  - [CometBFT v0.38.17 release notes](https://github.com/cometbft/cometbft/releases/tag/v0.38.17)

#### New Messages (Added Modules)

**`x/circuit` Circuit Breaker Module** ([cosmos-sdk docs](https://docs.cosmos.network/v0.50/build/modules/circuit)):

- `MsgAuthorizeCircuitBreaker` - Grant circuit breaker permissions
- `MsgTripCircuitBreaker` - Disable message execution
- `MsgResetCircuitBreaker` - Re-enable message execution

**`x/consensus` Consensus Parameters Module** ([cosmos-sdk docs](https://docs.cosmos.network/v0.50/build/modules/consensus)):

- `MsgUpdateParams` - Update consensus parameters via governance (replaces CometBFT consensus param updates)

**Hyperlane**:

- `hyperlane/core` - Cross-chain messaging infrastructure
- `hyperlane/warp` - Token bridging and routing

#### Removed Messages (Deprecated Modules)

**`x/crisis` Module** - Removed in Cosmos SDK v0.50.x

**`x/capability` Module** - Removed in Cosmos SDK v0.50.x:

- IBC capability management integrated directly into IBC v8 modules

**`x/paramfilter` Module** - Celestia-specific module removed:

- Parameter filtering functionality replaced by circuit breaker

#### Changed Messages and Logic

**Parameter Management Migration**:

- **Generic parameter proposals** deprecated in favor of module-specific `MsgUpdateParams` messages
- **Consensus parameters** moved from CometBFT to dedicated `x/consensus` module
- **All modules** now use module-specific parameter update messages instead of legacy `x/params` proposals

**IBC v6 to v8 Protocol Changes** ([v6 to v7](https://ibc.cosmos.network/main/migrations/v6-to-v7), [v7 to v8](https://ibc.cosmos.network/main/migrations/v7-to-v8))

### Library Consumers (v4.0.0)

**Import Changes**:

- Add: `cosmossdk.io/x/circuit`, `cosmossdk.io/x/consensus`
- Remove: `x/capability`, `x/crisis`, `x/paramfilter`
- Update: `github.com/cosmos/ibc-go/v8` (from v6)

**API Breaking Changes** ([cosmos-sdk migration guide](https://docs.cosmos.network/v0.50/build/migrations/upgrading)):

- Module keepers now accept `context.Context` instead of `sdk.Context`
- `BeginBlock`/`EndBlock` signatures changed
- Parameter updates require module-specific `MsgUpdateParams` messages

## v3.0.0

### Node Operators (v3.0.0)

Node operators MUST upgrade their binary to this version prior to the v3 activation height. Node operators SHOULD NOT use cosmovisor to upgrade their binary.

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

Consensus node operators are expected to upgrade to this release _prior_ to the Lemongrass hardfork if they intend to continue participating in the network. The command used to start the [consensus node](https://docs.celestia.org/how-to-guides/consensus-node#start-the-consensus-node) or [validator node](https://docs.celestia.org/how-to-guides/validator-node#run-the-validator-node) will accept an additional `--v2-upgrade-height` flag. See [this table](https://docs.celestia.org/how-to-guides/network-upgrade-process#lemongrass-network-upgrade) for upgrade heights for each network. Node operators SHOULD NOT use cosmovisor to upgrade their binary.

Consensus node operators should enable the BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm. See [#3812](https://github.com/celestiaorg/celestia-app/pull/3812).

### Library Consumers (v2.0.0)

If you are a library consumer, a number of the Go APIs have changed since celestia-app v1.x.x. Some of the notable changes are:

- Code pertaining to the original data square was extracted to [celestiaorg/go-square](https://github.com/celestiaorg/go-square).
  - celestia-app v1.x had a shares package. celestia-app v2.x uses [go-square/shares](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/shares)
  - celestia-app v1.x had a blob.types package with `CreateCommitment` function. celestia-app v2.x uses `CreateCommitment` function from the [go-square/inclusion](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/inclusion).
- celestia-app v1.x had a lot of functionality included in the signer. celestia-app v2.x splits a txClient from the signer. See [#3433](https://github.com/celestiaorg/celestia-app/pull/3433).
