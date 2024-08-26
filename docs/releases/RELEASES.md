# Releases

This guide provides instructions for upgrading celestia-app to breaking releases.

## [v2.0.0](https://github.com/celestiaorg/celestia-app/releases/tag/v2.0.0)

### Node Operators

If you are a consensus node operator, please follow the communication channels listed under [network upgrades](https://docs.celestia.org/nodes/participate#network-upgrades) to learn when this release is recommended for each network (e.g. Mocha, Mainnet Beta).

Consensus node operators are expected to upgrade to this release _prior_ to the Lemongrass hardfork if they intend to continue participating in the network. The command used to start the [consensus node](https://docs.celestia.org/nodes/consensus-node#start-the-consensus-node) or [validator node](https://docs.celestia.org/nodes/validator-node#run-the-validator-node) will accept an additional `--v2-upgrade-height` flag. See [this table](https://docs.celestia.org/nodes/hardfork-process#lemongrass-hardfork) for upgrade heights for each network.

Consensus node operators should enable the BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm. See [#3774](https://github.com/celestiaorg/celestia-app/pull/3774).

### Library Consumers

If you are a library consumer, a number of the Go APIs have changed since celestia-app v1.x.x. Some of the notable changes are:

- Code pertaining to the original data square was extracted to [celestiaorg/go-square](https://github.com/celestiaorg/go-square).
  - celestia-app v1.x had a shares package. celestia-app v2.x uses [go-square/shares](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/shares)
  - celestia-app v1.x had a blob.types package with `CreateCommitment` function. celestia-app v2.x uses `CreateCommitment` function from the [go-square/inclusion](https://github.com/celestiaorg/go-square/tree/c8242f96a844956f8d1c60e5511104deed8bc361/inclusion).
- celestia-app v1.x had a lot of functionality included in the signer. celestia-app v2.x splits a txClient from the signer. See [#3433](https://github.com/celestiaorg/celestia-app/pull/3433).
