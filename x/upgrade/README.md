# `x/upgrade`

This upgrade module is a fork of the cosmos-sdk's [x/upgrade](https://github.com/cosmos/cosmos-sdk/tree/main/x/upgrade) module. The primary purpose of this module is to allow for rolling network upgrades as proposed in [ADR-018](../../docs/architecture/adr-018-network-upgrades.md) and [CIP-10](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-10.md).

Note: this module won't be used for upgrading from app version v1 to v2 but will be used for upgrading from v2 to v3 and onwards.

## Concepts

- Total voting power: The sum of voting power for all validators.
- Voting power threshold: The amount of voting power that needs to signal for a particular version for an upgrade to take place. This is a percentage of the total voting power (usually 5/6).

## State

This module persists a map in state from validator address to version that they are signalling for.

## State Transitions

The map from validator address to version is updated when a validator signals for a version (`SignalVersion`) and after an upgrade takes place (`ResetTally`).

## Messages

See [types/msgs.go](./types/msgs.go) for the message types.

## Client

### CLI

```shell
celestia-appd query upgrade tally
celestia-appd tx upgrade signal
celestia-appd tx upgrade try-upgrade
```

### gRPC

```api
celestia.upgrade.v1.Query/VersionTally
```

```shell
grpcurl -plaintext localhost:9090 celestia.upgrade.v1.Query/VersionTally
```

## Appendix

1. <https://github.com/celestiaorg/celestia-app/blob/main/docs/architecture/adr-018-network-upgrades.md>
1. <https://github.com/celestiaorg/CIPs/blob/main/cips/cip-10.md>
1. <https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/x/upgrade/README.md>
1. <https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/x/gov/README.md>
