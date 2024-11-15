# ADR 23: Multiplexed App

## Changelog

- 2024/10/14: Initial draft (@rootulp)

## Status

Draft

## Glossary

**Single binary syncs**: the ability for a single binary to sync the entire history of the chain from genesis to the most recent block.
**Donwtime-minimized upgrades**: the ability for the network to upgrade app versions within the expected block time (i.e. 12 or 6 seconds).

## Context

Celestia-app v2.x and v3.x binaries support multiple versions of the state machine. This capability is necessary for single binary syncs and downtime-minimized upgrades. The current implementation of this feature involves conditionals littered throughout the codebase to perform version-specific logic. This ADR proposes a new design that abstracts the version-specific logic into separate Go modules (i.e. `celestia-app/v2`, `celestia-app/v3`). Each module will contain an implementation of the state machine for a particular app version. In other words `celestia-app/v2` will not contain any conditional statements to implement features in v3 and vice versa. A multiplexer will be responsible for routing ABCI messages to the appropriate module based on the app version.

## Decision

TBD

## Detailed Design

![multiplexer-diagram](./assets/adr023/multiplexer-diagram.png)

As a prerequisite to this work, Go modules must be extracted for all state machine modules. This is necessary so that the types defined by one module do not conflict with the types defined by the same module imported via a different state machine version.

## Alternative Approaches

Continue adding conditional statements to the codebase to implement version-specific logic. Note: this approach may no longer be viable when two different state machine versions need to use different versions of the same dependency. For example, celestia-app v3.x uses `github.com/cosmos/ibc-go/v6 v6.2.2` but it isn't possible to update that dependency for future celestia-app versions without having the bump also impact the v3.x state machine.

## Consequences

### Positive

- Potentially simplifies the codebase by removing conditionals for version-specific logic.

### Negative

- Makes the release process more cumbersome because we must create new releases for state machine modules. Previously the state machine modules were included in the celestia-app go module and therefore releaseed every time we created a celestia-app release.

### Neutral

- Increases the number of exported Go modules exported by the celestia-app repo.

## References

- A prototype implementation of this design: <https://github.com/celestiaorg/celestia-app/pull/3729>
