# ADR 020: Deterministic Square Construction

## Status

Implemented in <https://github.com/celestiaorg/celestia-app/pull/1690>

## Changelog

- 2023/4/13: Initial draft
- 2023/5/30: Update status

## Context

The current protocol around the construction of an original data square (ODS) is based around a set of constraints that are enforced during consensus through validation (See `ProcessProposal`). Block proposers are at liberty to choose not only what transactions are included and in what order but can effectively decide on the amount of padding (i.e. where each blob is located in the square) and the size of the square. This degree of control leaks needless complexity to users with little upside and allows for adverse behaviour.

Earlier designs were incorporated around the notion of interaction between the block proposer and the transaction submitter. A user that wanted to submit a PFB would go to a potential block proposer, provide them with the transaction, the proposer would then reserve a position in the square for the transaction and finally the transaction submitter would sign the transaction with the provided share index. However, Celestia may have 100 potential block proposers which are often hidden from the network. Furthermore, transactions often reach a block proposer through a gossip network, severing the ability for the block proposer to directly communicate with the transaction submitter. Lastly, new transactions with greater fees might arrive causing the block proposer to want to shuffle the transactions around in the square. The response to these problems was to come up with "non-interactive defaults" (first mentioned in [ADR006](./adr-006-non-interactive-defaults.md)).

## Decision

Block proposers should only have the freedom to decide transaction inclusion and ordering (Note there are some existing constraints on ordering). Interaction is still permitted (in the sense that a transaction submitter can directly communicate with a block proposer) but not directly supported by the Celestia network. The non-interactive defaults become mandatory.

Square construction is thus to be reduced to the simple deterministic function:

```go
func ConstructSquare(txs []Tx) []Share
```

and its counterpart

```go
func DeconstructSquare(shares []Share) []Tx
```

Block proposers need not know nor care about the internals of square construction. With gradual improvements to construction techniques that best incorporate the tradeoffs between square size (impacting bandwidth and storage) with proof size (impacting light clients), the latest culmination in [ADR013](./adr-013-non-interactive-default-rules-for-zero-padding.md), there is little room for validators to implement optimizations. Such existing control to block proposers also means they are able to spam the network; building full size squares that include a single PFB. Lastly, a deterministic construction mechanism has the benefit of a more compact representation of the data that can freely move between different representations (this also supports a simpler compact blocks protocol):

```go
[]Tx <=> []Share (ODS) <=> EDS
```

Validation in `ProcessProposal` is simplified to reconstructing the square given the transaction set and then evaluating whether the computed data root matches the proposed one.

## Detailed Design

A new pkg `square` will be responsible for housing the square construction logic. Any modifications to the algorithm must come as a coordinated upgrade and thus be mapped to the `AppVersion`.

Squares are bound by an upper limit on the square size, ergo not all transactions can fit in a square. Given a prioritised ordering of transactions, each transaction is individually staged and if it fits is added to the pending square else it is dropped. Each addition of a transaction has the potential to shuffle up the order of the shares (since shares are ordered by namespace not order of priority). Therefore, the design conservatively estimates how much padding the blob could have and tracks the total offset to understand whether it fits in the square. This is done for both compact shares and sparse shares (which follow different rules):

For compact shares i.e. PFBs and other state transactions, we introduce a `CompactShareCounter` that tracks two fields (`shares` & `remainder`). It is centered around the `Add` method:

```go
func (c *CompactShareCounter) Add(dataLen int, shareCapacity int) bool
```

If there is sufficient capacity the `CompactShareCounter` will record the share and return true. The `remainder` field is used to record the remaining bytes in the ultimate share.
This emulates exactly how the encoding is done resulting in no error to the actual shares used.

For sparse shares i.e. the blobs themselves, the `SparseShareCounter` estimates the worst case intra-blob padding. This is according to the rules specified in [ADR013](./adr-013-non-interactive-default-rules-for-zero-padding.md). To paraphrase, the padding between blobs is dictated based on what would be the first subtree root in the merkle mountain range over the set of shares that correspond to that blob. The higher the first subtree root, the greater the worst case padding could possibly be. As an example, say a blob consisted of 11 shares, and that according to the specification in the construction of a `ShareCommitment`, the subtree roots (i.e. the merkle mountain range was): 4, 4, 2, 1. The worst case padding is 4 - 1 = 3 shares. Accordingly, as any blob with shares less than or equal to `SubtreeRootThreshold` has a subtree root height of 1, these blobs will all have zero padding. The `SpareShareCounter` is thus not optimal in it's estimation of the shares required as padding might be less than what was estimated, however further investigation is left for later discussion. Note that as we are taking the worst case padding, we don't need to worry about the order of the blobs. They will be ordered by namespace only at the end.

With these two structs, we can safely guarantee that all staged transactions can be included in the square and from the estimation can formulate the minimum square size that will be used to calculate the share index for all the blobs. The construction of the square itself remains the same. Additionally, as the share index and square size can be deterministically calculated it no longer needs to be gossiped to all nodes in the consensus network. However, for verifiability purposes the square size should still remain in the `Data` struct.

Both `PrepareProposal` and `ProcessProposal` will as a result, call much the same methods. Verification is thus reduced to: did I create the same square as you, rather than is your version of the square valid. The main difference is that `PrepareProposal` will handle overflow of transactions by discarding them, while `ProcessProposal` will handle overflow by rejecting the block.

The new algorithm will no longer need to check that the blobs are ordered by namespace and that the wrapped PFBs contain the correct share index. `ProcessProposal` will still need to verify the `BlobTx` format (i.e that each blob has a matching PFB and that the PFBs are correctly signed)

## Consequences

### Positive

- By staging transactions in order of priority, we prevent the removal of a higher priority transaction that had a higher namespace than a transaction with a lower namespace and lower priority. [#1519](https://github.com/celestiaorg/celestia-app/issues/1519)
- Block proposers aren't able to spam the network with unnecessarily large squares filled with padding as they no longer have control over the size of the square in so far as the amount of transactions included.
- Gossiping of data can be condensed to just an ordered list of transactions. This more easily enables a compact blocks style of consensus.

### Negative

- Block proposers can no longer run forks of celestia-app that make optimizations to the square layout. Optimizations have to be funneled through the canonical implementation.

### Neutral

- The concept of the square and its complexity becomes more hidden to users. Their mental model can be simplified to posting transactions and validating that they were published.

## References

- [ADR013](./adr-013-non-interactive-default-rules-for-zero-padding.md)
- [PoC: iterative and deterministic square construction](https://github.com/celestiaorg/celestia-app/pull/1301)
- [ADR006](./adr-006-non-interactive-defaults.md)
