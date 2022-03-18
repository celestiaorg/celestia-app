# ADR 003: ValSet

## Changelog

- {date}: {changelog}

## Context
Various Rollups on EVM L1s may have a need for using Celestia as a DA layer, while keeping fraud/validity proof verification in the EVM.
To that end, two concrete possibilities exist: the Gravity bridge and a native EVM light client.

## Approaches
The following issue summarizes the two viable approaches to accommodate this: [#4](https://github.com/celestiaorg/quantum-gravity-bridge/issues/4)

## Decision
We decided to go for the gravity bridge approach and move the orchestrator/relayer logic to Celestia-app.

## Detailed Design
The QGB allows Celestia block header data roots to be relayed in one direction, from Celestia to an EVM chain. It does not support
bridging assets such as fungible or non-fungible tokens directly, and cannot send messages from the EVM chain back to Celestia.

Thus, choosing the gravity bridge approach makes more sense because:
- The QGB is a one way bridge which doesn't need to handle any state. This makes it significantly simpler to implement.
- The native EVM light client would take more time to build and wouldn't have any significant added value in our case.

Also, we decided to rewrite the relayer and the orchestrator because our relayer can be designed to be trust minimized,
where-as a two-way bridge would require each validator to run a trusted relayer.
And, since we had to rewrite them, and they were simplified, we decided to move things to the app.

For the state, it is currently changed by confirmation messages via mapping the orchestrator's address to the confirmation. This latter will
be used for slashing afterwards.

## Status
Accept

## Consequences of moving the logic to the app

### Positive
- Single binary containing the orchestrator and also Celestia-app.
- Ease of maintainability.

### Neutral
- Single binary will force validators who only want to join the Celestia chain, to also have the bridge logic. However, it should be alright
since it's only a small amount of code.

## References

- {reference link}
