# ADR 5: Using block space instead of state to post attestations confirms

## Changelog

- {date}: {changelog}

## Context

Currently, the QGB is using the state solely to save all the attestations and confirms it needs.
We propose to post `Valset Confirms` and `DataCommitment Confirms` in a reserved namespace instead of adding them to the state to achieve the following:

- Reduce the state machine complexity
- Reduce the amount of state used by the QGB
- Prepare for a more Rollup oriented QGB design

## Alternative Approaches

### Keep the existing design

Keeping the current design would entail using the state extensively.
This proves bad when the state grows after a few hundred attestations, and performing checks on the `Valset Confirms` and `DataCommitment Confirms` becomes so expensive.
An example of such issue in here: [QGB data commitments/valsets state machine checks #631](https://github.com/celestiaorg/celestia-app/issues/631) and [Investigate the QGB transactions cost #603](https://github.com/celestiaorg/celestia-app/issues/603).

Also, we will be forced to prune the state after the unbonding period not to end up with a gigantic state, issue defining this: [Prune the QGB state after the unbonding period ends #309](https://github.com/celestiaorg/celestia-app/issues/309).
This would mean that the QGB contracts deployed after genesis will never have the whole history of attestations.

### Remove state definitively

This would mean gossiping the confirms and attestations in a separate P2P network.
The pros of this approach are that it will be cheaper and wouldn't involve any state changes.
However, slashing will not be possible.

### QGB Rollup

Deploy the QGB as a Rollup that posts its data to Celestia, and, uses a separate settlement layer for slashing.
This might be the end goal of the QGB, but it will be very involved to build at this stage.

Also, if this ADR got accepted, it will be an important stepping stone into the Rollup direction.

## Decision

We will need to decide on two things:

- [ ] Should we go for this approach?
- [ ] Should this change be part of QGB 1.0?

## Detailed Design

The first design for the QGB was to use the state extensively to store all the QGB related data: Attestations, `Valset Confirms` and `DataCommitment Confirms`.
As a direct consequence of this, we needed to add thorough checks on the state machine level to be sure that any proposed attestation is correct and will eventually be relayed to the target EVM chain.
The following issue lists these checks: [QGB data commitments/valsets state machine checks #631](https://github.com/celestiaorg/celestia-app/issues/631) and here is their [implementation](https://github.com/celestiaorg/celestia-app/blob/d63b99891023d153ea5937e4f3c1907a784654d8/x/qgb/keeper/msg_server.go#L28-L262).
Also, the state machine for the QGB module became complex and more prone to bugs that might end up halting/forking the chain.

In addition to this, with the gas leak issue discussed in this [comment](https://github.com/celestiaorg/celestia-app/issues/631#issuecomment-1220848130), we ended up removing the sanitizing checks we used to run on the submitted `Valset Confirms` and `DataCommitment Confirms`.
This was done in the goal of not charging orchestrators increasing gas fees with every posted attestations.
A simple benchmark showed that the gas usage multiplied 2 times from `~50 000` to `100 000` after submitting 16 attestation.
Also, even if removing the checks was the most practical solution, it ended up opening new attack vectors on the QGB module state, such as flooding the network with incorrect attestations from users who are not even validators.
Which would increase the burden on validators to handle all of that state.
Furthermore, it put the responsibility on the relayer to cherry-pick the right confirms from the invalid ones.

This led us to think that the direction we're taking makes more sense if it moves the confirms from the state to the block space.
This way, we can remove all the complex logic handling the Valset Confirms and DataCommitment Confirms to the orchestrator side, and also block any opportunity for any malicious party to post data to state.

In fact, we will be able to remove the `MsgValsetConfirm` defined in [here](https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/proto/qgb/msgs.proto#L24-L49)
And also, the `MsgDataCommitmentConfirm` defined in [here](
https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/proto/qgb/msgs.proto#L55-L76).
Which were the way orchestrators were able to post confirms to the QGB module.
Then, the only state that the QGB module will keep is the one that is created in [EndBlocker](https://github.com/celestiaorg/celestia-app/blob/a965914b8a467f0384b17d9a8a0bb1ac62f384db/x/qgb/abci.go#L12-L16).
Which are `Attestations`, i.e. `Valset`s and `DataCommitmentRequest`s.
We will need to investigate the state's growth if we keep in it only these latter.

In addition to this, we will need to reserve a certain namespace ID to only be reserved for Confirms.
This might also require a discussion on how we want to put the data in the block, and investigate the ability to batch confirms.

When it comes to slashing, we can add the `dataRoot` of the blocks to state during `ProcessProposal`,  `FinalizeCommit`, or in some other way to be defined. Then, we will have a way to slash orchestrators after a certain period of time if they didn't post any confirms. The exact details of this will be left for another ADR.

## Status

Proposed

## Consequences

### Positive

- Reduce significantly the gas fees paid by orchestrators.
- Reduce significantly the use of Celestia state.
- Keep the whole QGB confirms history via using block data, instead of being forced to delete it after the unbonding period not to end up with a super gigantic state. This would allow any QGB contract to have the whole history on any EVM chain.

### Negative

- Reducing the checks applied on the confirms: everyone will be able to post commitments, and we will have no way to sanitize them before adding them to a block. This would add an extra overhead for the relayer to choose the right commitments. However, it is alright, since we will not have a lot of Also, we will need to decide if this will be relayers, and we can give them enough computation power to compensate the overhead.
- Potentially harder slashing as fraud proofs will need data from blocks and also state, compared to depending only on state for verification.

### Neutral

Credits to @evan-forbes.
