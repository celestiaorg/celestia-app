# ADR 017: Single binary upgrade

## Status

Proposed

## Changelog

- 2023/03/22: initial draft

## Context

It important to sync from genesis with one binary.
It is needed for wallets, block explorers, indexers, analytics (e.g. Nansen).
It makes it possible to sync headers from chain and download blobs from third party service.

## Alternative Approaches

Instead of having a single binary, we can have multiple binaries that are managed by another supervisory binary e.g. [Cosmovisor](https://docs.desmos.network/fullnode/cosmovisor/).
Osmosis does it through having a significant number of binaries gathered in one docker container and switch them in particular heights.
This approach has a lot of maintenance burden and is fragile to sync from genesis.

## Decision

TBD

## Detailed Design

- Cosmos blocks contain a version.app (version of state machine) that can be used to indicate which version of `celestia-app` should be used to process a block.
  - Example: Osmosis is currently on [version.app: 15](https://www.mintscan.io/osmosis/blocks/8804111) and release [v15.0.0](https://github.com/osmosis-labs/osmosis/releases/tag/v15.0.0).

  - Have to maintain old versions of binaries. Need to update dependencies for security issues.

- Genesis has an original version.app. Does celestia-app need to explicitly define this?

- Bitcoin / Ethereum leverage if statement upgrade logic. Downside to if statement logic in Cosmos ecosystem is if statements may not be relevant for new chains

- UX issue to improve experience for node operators to upgrade

- Maintenance issue for maintaining historical releases of binaries

- What granularity should conditional logic be?
  - App struct level
  - Module level
  - if/else on a line by line basis

- Why app version and not block height?
  - App version is bumped via upgrade module
  - Depending on app version is universal across multiple networks.
  - Block height will differ across testnets / chain-ids so depends on a `.toml` config file

- Are we planning on upstream this?
  - [CometBFT](https://github.com/cometbft/cometbft) is working on this for consensus.
  - We’re working on Cosmos SDK portion

- `celestia-node` light nodes need to version erasure coding block or celestia-node should import this from celestia-app

- Will celestia-node need to pass a block height to celestia-app? Header contains version.app (state machine version) and version.block (consensus version)

- Is it important to support rolling back versions? Involves upgrading (again) through the faulty version

- Since if conditional logic uses one Go version, all code will be compiled with this one Go version. If behavior changes across Go versions, we have to debug why.

- Option: depend on multiple major versions of a dependency. What is the relationship between if/else conditional statements and Cosmos SDK upstream changes?

- How to do this in CometBFT?
  - Always use latest p2p version
  - Version.block in header should be used to version changes to Tendermint / CometBFT

- Block sync needs to be aware of changes across block heights

## Consequences

> This section describes the consequences, after applying the decision. All consequences should be summarized here, not just the "positive" ones.

### Positive

### Negative

### Neutral

## References

[The meeting notes](https://docs.google.com/document/d/1UuiM9sKQ4g30OBoZLI5pwYBwoicvgC65xm9yzqmbh0g/edit#)

- {reference link}
