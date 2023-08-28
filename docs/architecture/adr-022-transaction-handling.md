# ADR 022: Reliable Transaction Handling

## Changelog

- 2023/08/28: Initial draft

## Status

Proposed

## Context

Nonces are a crucial component in transactions for replay protection; preventing users from having their transaction executed multiple times. Each account has a monotonically increasing number associated with it: the nonce (also referred as the `sequence` in the Cosmos SDK). If the last executed nonce is `n`, then the next transaction signed by that account must have a nonce of `n + 1`. This, however, comes with a few downsides. If a user submits 3 transactions in a row with nonces 6, 7, 8 and while propogating throughout the network they are received by the next proposer as 8, 7, 6. Transactions with nonces 7 and 8 will be rejected and only 6 will make it into the mempool to be proposed in the next block. Furthermore, a local hash-based cache may prevent 7 or 8 from ever making it into the mempool. Secondly, if the transaction with nonce 6 happened to have too low priority it would block all other transactions. A user would not be able to overwrite the transaction by submitting the same one yet with a higher fee.

## Decision

Break up the `SigVerifyDecorator` into two: one for checking the validity of the signature and the second the validity of the nonce (sequence number). For the latter, there will be two versions of the decorator: 1) `CheckTx` and `ReCheckTx` will simply check that the provided sequence number is greater than the last executed one. 2) `DeliverTx` will check that the sequence number is one more than the previously executed one.

`CheckTx` will thus also no longer use `NewIncrementSequenceDecorator` on its branch of state.

The second part of the solution is to introduce an ordering mechanism in `PrepareProposal` which will take the priority ordered list of transactions and shuffle any transactions which have a greater nonce than the last executed one.

Specifically, it will loop through all transactions. Any transaction that has a a nonce below the last executed for that account will be dropped. If the transaction has a nonce more than one greater than the last executed it will be put in a temporary map (`map[string]map[uint64]sdk.Tx` where the map represents address => nonce => tx). If the transaction with the correct nonce appears later in the list, the protocol will check the map, and append the saved transaction directly after it (we already know it has a greater priority).

> NOTE: In future SDK versions, it is likely that priority will be managed on the application side (instead of on the consensus side).

This change is not consensus/state machine breaking as it does not modify the behavior of `DeliverTx` or `ProcessProposal` (methods that must be deterministic).  It can be rolled out as soon as v1.1.0

## Alternative Approaches

An alternative approach would be to use nonce lanes. This works by having multiple sequence numbers that a user can choose to increment any one of. The problem with this is that it heavily breaks the current account system. The `AccountI` interface defines a `SetSequence` and `GetSequence` which use single `uint64` values (as opposed to a key value style). It would also require a state migration.

## Consequences


### Positive

- Client libraries no longer need to handle situations where the network shuffles the order that the client signed the transactions forcing them to become invalid
- Users can bump the gas price on a transaction that was failing to make it in a block

### Negative

- Increased `ProcessProposal` complexity

### Neutral

## References

- [Consider being able to replace one transaction with a higher fee](https://github.com/celestiaorg/celestia-app/issues/2334)
