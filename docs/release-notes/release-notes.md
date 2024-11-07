# Release Notes

This guide provides notes for major version releases. These notes may be helpful for users when upgrading from previous major versions.

## v3.0.0

### Node Operators (v3.0.0)

#### Enabling BBR and MCTCP

Consensus node operators must enable the BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm. See [#3774](https://github.com/celestiaorg/celestia-app/pull/3774).
if using linux in docker, kubernetes, a vm or baremetal, this can be done by calling 

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

**Alternatively**, the configurations can be changed manually. This involves updating the mempool TTLs and the send and the receive rates.

- Configuring Bandwidth Settings
  - update `recv_rate` and `send_rate` in your TOML config file to 10MiB (10485760).
- Extend TTLs
  - update `ttl-num-blocks` in your TOML config file to 12.

#### Signaling Upgrades

- Upgrades now use the `x/signal` module to coordinate the network to an upgrade height.

The following command can be used, if you are a validator in the active set, to signal to upgrade to v3

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

If you are a consensus node operator, please follow the communication channels listed under [network upgrades](https://docs.celestia.org/nodes/participate#network-upgrades) to learn when this release is recommended for each network (e.g. Mocha, Mainnet Beta).

Consensus node operators are expected to upgrade to this release _prior_ to the Lemongrass hardfork if they intend to continue participating in the network. The command used to start the [consensus node](https://docs.celestia.org/nodes/consensus-node#start-the-consensus-node) or [validator node](https://docs.celestia.org/nodes/validator-node#run-the-validator-node) will accept an additional `--v2-upgrade-height` flag. See [this table](https://docs.celestia.org/nodes/network-upgrade-process#lemongrass-network-upgrade) for upgrade heights for each network.

Consensus node operators should enable the BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm. See [#3812](https://github.com/celestiaorg/celestia-app/pull/3812).

### Library Consumers (v2.0.0)

If you are a library consumer, a number of the Go APIs have changed since celestia-app v1.x.x. Some of the notable changes are:

- Code pertaining to the original data square was extracted to [celestiaorg/go-square](https://github.com/celestiaorg/go-square).
  - celestia-app v1.x had a shares package. celestia-app v2.x uses [go-square/shares](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/shares)
  - celestia-app v1.x had a blob.types package with `CreateCommitment` function. celestia-app v2.x uses `CreateCommitment` function from the [go-square/inclusion](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/inclusion).
- celestia-app v1.x had a lot of functionality included in the signer. celestia-app v2.x splits a txClient from the signer. See [#3433](https://github.com/celestiaorg/celestia-app/pull/3433).
