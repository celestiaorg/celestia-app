# ADR 001: Adopt ABCI++ Early

## Changelog

- 2021-11-30: Initial Commit

## Context

We need validators to make some checks on the proposed block before they vote to ensure that messages that are paid for are actually included. Due to the current iteration of tendermint not passing block data to the application before committing to it, this additional step needs to be added. While we could add it in tendermint ourselves, this is already being done by the tendermint team as part of the migration to ABCI++, and cherry picking only the necessary components of that branch is increasingly looking like the best option.

It’s worth noting that eventually we will switch to using ABCI++ whenever tendermint includes it in an upcoming release. However, no firm date has been committed to, and we need to include a check for message inclusion before mainnet. Therefore, this ADR is proposing that we adopt portions of ABCI++ before they are officially added to a tendermint release. This will give us an opportunity to begin development in celestia-app and ensure that we have the necessary features tested in a live network before mainnet.

Not only is switching to ABCI++ inevitable, but it will also provide us with the tools to better implement future features and further reduce the necessary changes to our fork tendermint. For example, we will need to efficiently fill the data square so that it follows the non-interactive default rules and does not create a square larger than the max square size. While there are some hacky alternatives, being able to have access to the full block data during ABCI++’s `PrepareProposal` step will make this easier. Having access to the full block data while creating proposal blocks should also allow us to perform all erasure tasks in the app, instead of in our fork of tendermint. Eventually, it will even allow us to completely switch to immediate execution, instead of tendermint’s current deferred execution model.

The first downside of adopting ABCI++ early, would be that it requires that we don’t wait for the cosmos-sdk to incorporate it after being finished in tendermint. Fortunately, this should actually be doable, as we can take a similar approach to how we implemented `PreprocessTxs`. The second downside is that we will still probably have to wait until `ProcessProposal` is finished in the ABCI++ branch. Similarly, there might be unforseen side effects in consensus that could be tricky to handle, which could delay the tendermint team in quickly adding this new method.


## Alternative Approaches

The simplest, but still safe, alternative to ABCI++ for checking for message inclusion, would be to include the check in the `ValidateBasic` method of `Block`. Here we would iterate through all of the `PayForMessage` transactions, and then ensure that the message can be found in the block. This would ensure that all parties involved would deem a block with missing messages invalid. It would also mean that our fork of tendmerint would have to know how to parse `PayForMessage` transactions, `sdk.Txs`, and `sdk.Messages`. While doable by copying the necessary code to a different repo other than the sdk and celestia-app, this breaks the contract that tendermint was built around, where all application specific data is handled by the app. This approach would also complicate creating message inclusion fraud proofs, as `ValidateBasic` is called while unmarshalling blocks from their protobuf representation, and would therefore error instead of returning a block to create a fraud proof from.

We could also take an approach first mentioned in the draft PR for ADR008, where we can use tendermint’s current deferred execution model to our advantage, and not perform a consensus check at all. Instead of having a consensus check, we would perform a check on the original data square while `DeliverTx` processes a `PayForMessage`. If that check shows that the expected commitment for a given `PayForMessage` does not exist in the original data square, then we simply ignore that `PayForMessage`. It’s just wasted block space at that point, and has no effect on the state. While this approach should be easy to implement, it is not ideal, as it would make concise fraud proofs much more difficult in the future. This is because concise message inclusion fraud proofs rely on the assumption that if a `PayForMessage`s exists in the original data square, but it’s corresponding message does not, then the block is invalid. This would not be the case with this approach, we could have `PayForMessage`s without their corresponding message and the block would still be valid. Not only would we lose the ability to make concise message inclusion fraud proofs, but this approach would also need a way to pass the the block data to the application. This would require a substantial pivot from our strategy of treating tendermint like a black box.

## Decision



## Detailed Design

The first thing we would have to do, is to create a branch of celestia-core that incorporates the new changes for `ProcessProposal`. Then, we will need to modify our fork of the cosmos-sdk to include these new changes, not unlike what we did when implementing `PreProcessProposal`. Finally, we will be able to begin implementing the message inclusion checks in the app, along with the other previously mentioned features.

## Status

Proposed

## Consequences

### Positive

- Allows us to begin work on future features in a less hacky way, that we won't have to completely redo as soon as ABCI++ is released.
- Street cred for being the first ABCI++ app
- Contribute upstream by testing one of the most important upgrades in tendermint history

### Negative

- We will likely have to refactor slightly after the cosmos-sdk also adopts the cosmos-sdk
- Testnet implementation and mainnet implementaiton might differ provided we refactor to use the official cosmos-sdk's version of ABCI++ as soon as that is ready.
- The only feature that is technically needed is a message inclusion check, which we have a half decent alternative for.
- Almost guaranteed unforseen issues due to the scope of the changes.

## References

ABCI++ tracking issue
`ProcessProposal` PR
Message inclusion draft ADR
Testnet Tracking issue
