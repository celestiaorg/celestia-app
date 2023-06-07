# Block Validity Rules

Unlike most blockchains, Celestia derives most of its functionality from
stateless commitments to data rather than stateful transitions. This means that
the protocol relies heavily on block validity rules. These block validity rules
must be verifiable by light clients without making an honest majority
assumption, which has a significant impact on the design of those rules and how
they are enforced.

Before any Celestia specific validation is performed, all cometBFT (formerly
tendermint) [block validation
rules](https://github.com/cometbft/cometbft/blob/v0.34.28/spec/core/data_structures.md#block)
must be followed.

The only addition or modification of the standard cometBFT block validation
rules involve the block data. CometBFT creates a commitment to the block data
using a simple binary merkle tree, while Celestia uses a binary merkle root over
the row and column namespaced merkle tree roots of a erasure encoded square.

