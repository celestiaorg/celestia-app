# ADR 017: Single binary sync

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

### Implementation proposal

There are multiple ways of doing it. The app version can be found in the SDK context.
So, in the `PrepareProposal` method, we can get the app version [from the context](https://github.com/celestiaorg/celestia-app/blob/main/app/prepare_proposal.go#L28) and use it as the version indicator.

#### Option 1: Passing the app version throughout all the required functions

In this case we need to pass either sdk context or the app version itself to all the functions and objects that we think they might be affected by a version change in future.
So for those functions that do not have the ctx already, we need to add an extra parameter to pass it.

#### Option 2: Define a version package and call a global function to get the version

We can define a package e.g. `version` that has a setter and getter. And the getter can be called by anyone throughout the application.

```go
var appVersion string

func SetVersion( newVer string) error {
  // Check if the caller is eligible to call this.
  appVersion = newVer
}

func GetVersion() string {
  return appVersion
}

```

Then we call the setter function in `ProcessProposal` where it reads it from the block header.
The getter function will be called everywhere e.g. [mergeMaps](https://github.com/celestiaorg/celestia-app/blob/f1dec1014a7159c0f0b213182aff4793163e9732/pkg/shares/share_splitting.go#L159)

```go
func mergeMaps(mapOne, mapTwo map[coretypes.TxKey]ShareRange) map[coretypes.TxKey]ShareRange {
	merged := make(map[coretypes.TxKey]ShareRange, len(mapOne)+len(mapTwo))

  switch version.GetVersion() {

    case version.V0:
      maps.Copy(merged, mapOne)
      maps.Copy(merged, mapTwo)

    case version.V1:
      maps.Copy(merged, mapTwo)
      maps.Copy(merged, mapOne)

    default:
        panic(version.ErrVersionNotFound)
  }
  return merged
}
```

This option has an advantage over the option #1 as we do not need to pass an extra parameter everywhere in the code and it has less maintenance burden to modify the version logic.
We can even add more structure to the version package in future if needed.

#### Initializing the app version

The app version can be configured in the genesis file as below:

```json
{
  ...
"consensus_params": {
    ...
    "version": {
      "app_version": "<a uint64 number>"
    }
  },
  ...
}
```

Then, it is read in the endblock of each module.

## Consequences

> This section describes the consequences, after applying the decision. All consequences should be summarized here, not just the "positive" ones.

### Positive

### Negative

### Neutral

## References

[The meeting notes](https://docs.google.com/document/d/1UuiM9sKQ4g30OBoZLI5pwYBwoicvgC65xm9yzqmbh0g/edit#)

- {reference link}
