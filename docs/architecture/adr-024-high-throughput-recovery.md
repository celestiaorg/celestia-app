# ADR 024: High Throughput Recovery

## Changelog

- 2025/01/29: Initial draft (@evan-forbes)

## Status

Proposed

## Context

The Celestia protocol will likely separate block propagation into two phases. "Preparation", for distributing data before the block is created, and "recovery" for distributing data after the block has been created. In order to utilize the data distributed before the block is created, the recovery phase must also be pull based. Therefore, the constraints for recovery are:

- 100% of the Block data MUST be delivered to >2/3 of the voting power before the ProposalTimeout is reached
- MUST use pull based gossip

## Decision

TBD

## Detailed Design

- [Messages](./assets/adr024/messages.md)
- [Handlers and State](./assets/adr024/handlers_and_state.md)
- [Connecting to Consensus](./assets/adr024/connecting_to_consensus.md)

## Alternative Approaches

### PBBT w/o erasure encoding

### No broadcast tree

## Consequences

### Positive

### Negative

### Neutral

## References
