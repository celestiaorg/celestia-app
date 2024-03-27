# ADR 005: QGB Reduce State Usage

## Status

Deprecated in favor of [orchestrator-relayer#65](https://github.com/celestiaorg/orchestrator-relayer/pull/66)

## Context

The first design for the QGB was to use the state extensively to store all the QGB-related data: Attestations, `Valset Confirms` and `DataCommitment Confirms`.
As a direct consequence of this, we needed to add thorough checks on the state machine level to be sure that any proposed attestation is correct and will eventually be relayed to the target EVM chain.
The following issue lists these checks: [QGB data commitments/valsets state machine checks #631](https://github.com/celestiaorg/celestia-app/issues/631) and here is their [implementation](https://github.com/celestiaorg/celestia-app/blob/d63b99891023d153ea5937e4f3c1907a784654d8/x/qgb/keeper/msg_server.go#L28-L262).
Also, the state machine for the QGB module became complex and more prone to bugs that might end up halting/forking the chain.

In addition to this, with the gas leak issue discussed in this [comment](https://github.com/celestiaorg/celestia-app/issues/631#issuecomment-1220848130), we ended up removing the sanitizing checks we used to run on the submitted `Valset Confirms` and `DataCommitment Confirms`.
This was done with the goal of not charging orchestrators increasing gas fees for every posted attestation.
A simple benchmark showed that the gas usage multiplied 2 times from `~50 000` to `100 000` after submitting 16 attestations.
Also, even if removing the checks was the most practical solution, it ended up opening new attack vectors on the QGB module state, such as flooding the network with incorrect attestations from users who are not even validators. Which would increase the burden on validators to handle all of that state.
Furthermore, it put the responsibility on the relayer to cherry-pick the right confirms from the invalid ones.

We propose to keep the `Valset Confirms` and `DataCommitment Confirms` transaction types, but not handle/save them on the state. Instead, keep them as transactions in the blocks. And, delegate the task of parsing to the Relayer. This would allow us to achieve the following:

- Reduce the state machine complexity.
- Reduce the amount of state used by the QGB.
- Prepare for a more Rollup oriented QGB design (TBD).

## Alternative Approaches

### Keep the existing design

Keeping the current design would entail using the state extensively.
This proves bad when the state grows after a few hundred attestations, and performing checks on the `Valset Confirms` and `DataCommitment Confirms`, which run queries on the state, becomes too expensive.
An example of such an issue is here: [QGB data commitments/valsets state machine checks #631](https://github.com/celestiaorg/celestia-app/issues/631) and [Investigate the QGB transactions cost #603](https://github.com/celestiaorg/celestia-app/issues/603).

The approach that we were planning to take is to prune the state after the unbonding period.
This way, we will always have a fixed-sized state, issue defining this: [Prune the QGB state after the unbonding period ends #309](https://github.com/celestiaorg/celestia-app/issues/309).

### Separate P2P network

This would mean gossiping about the confirms and attestations in a separate P2P network.
The pros of this approach are that it will be cheaper and wouldn't involve any state changes.
However, slashing will be very difficult, especially for liveness, i.e. an orchestrator not signing an attestation and then slashing them after a certain period.

### Dump the QGB state in a namespace

Remove the `MsgValsetConfirm` defined in [here](https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/proto/qgb/msgs.proto#L24-L49)
And also, the `MsgDataCommitmentConfirm` defined in [here](
<https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/proto/qgb/msgs.proto#L55-L76>).
Which were the way orchestrators were able to post confirms to the QGB module.
Then, keep only the state that is created in [EndBlocker](https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/x/qgb/abci.go#L12-L16).
Which are `Attestations`, i.e. `Valset`s and `DataCommitmentRequest`s.

### QGB Rollup

Deploy the QGB as a Rollup that posts its data to Celestia, and, uses a separate settlement layer for slashing.
This might be the end goal of the QGB, but it will be very involved to build at this stage.

Also, if this ADR got accepted, it will be an important stepping stone in the Rollup direction.

## Decision

We will need to decide on two things:

- [ ] Should we go for this approach?
- [ ] Should this change be part of QGB 1.0?
- [ ] When do we want to implement slashing?

## Detailed Design

The proposed design consists of keeping the same transaction types we currently have : the `MsgValsetConfirm` defined in [here](https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/proto/qgb/msgs.proto#L24-L49), and the `MsgDataCommitmentConfirm` defined in [here](
<https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/proto/qgb/msgs.proto#L55-L76>). However, remove  all the message server checks defined in the [msg_server.go](https://github.com/celestiaorg/celestia-app/blob/9867b653b2a253ba01cb7889e2dbfa6c9ff67909/x/qgb/keeper/msg_server.go) :

```go
// ValsetConfirm handles MsgValsetConfirm.
func (k msgServer) ValsetConfirm(
    c context.Context,
    msg *types.MsgValsetConfirm,
) (*types.MsgValsetConfirmResponse, error) {
    // <delete_all_this>
}

// DataCommitmentConfirm handles MsgDataCommitmentConfirm.
func (k msgServer) DataCommitmentConfirm(
    c context.Context,
    msg *types.MsgDataCommitmentConfirm,
) (*types.MsgDataCommitmentConfirmResponse, error) {
    // <delete_all_this>
}
```

This would reduce significantly the QGB module state usage, reduce the complexity of the state machine and give us the same benefits as the current design.

As a direct consequence of this, the relayer will need to adapt to this change and start getting the transactions straight from the blocks, parse them and sanitize the commits. Thus, making the relayer implementation more complex.

However, we can assume that the relayer will have enough computing power to do the latter. Also, only one relayer is necessary to have a working QGB contract. So, the relayer cost is justified.

For the orchestrators, they will also need to parse the history to keep track of any missed signatures. But, same as with the relayers, the cost is justified.

For posting transactions, we will rely on gas fees as a mechanism to limit malicious parties to flood the network with invalid transactions. Then, eventually, slash malicious behavior. However, since posting confirms will be possible for any user of the network. It won't be possible to slash ordinary users, who are not running validators if they post invalid confirms.

When it comes to slashing, we can add the `dataRoot` of the blocks to the state during `ProcessProposal`,  `FinalizeCommit`, or in some other way to be defined. Then, we will have a way to slash orchestrators after a certain period of time if they didn't post any confirms. The exact details of this will be left for another ADR.

## Consequences

### Positive

- Reduce significantly the gas fees paid by orchestrators.
- Reduce significantly the use of Celestia state.

### Negative

- Reducing the checks applied on the confirms: everyone will be able to post commitments, and we will have no way to sanitize them.
- Potentially harder slashing as fraud proofs will need data from blocks and also state, compared to depending only on the state for verification.

### Neutral

Credits to @evan-forbes.
