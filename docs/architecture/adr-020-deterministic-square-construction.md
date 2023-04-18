# ADR 020: Deterministic Square Construction

## Status

Proposed

## Changelog

- 2023/4/13: initial draft

## Context

The current protocol around the construction of a square (ODS) is based around a set of constraints that are enforced during consensus through validation (See `ProcessProposal`). Block proposers are at liberty to choosing not only what transactions are included and in what order but can effectively decide on the amount of padding (i.e. where each blob is located in the square) and the size of the square. This degree of control leaks needless complexity to users with little upside and allows for adverse behaviour.

Earlier designs were incorporated around the notion of interaction between the block proposer and the transaction submitter. A user that wanted to submit a PFB would go to a potential block proposer, provide them with the transaction, the proposer would then reserve a position in the square for the transaction and finally the transaction submitter would sign the transaction with the provided share index. However, Celestia may have 100 potential block proposers which are often hidden from the network. Furthermore, tranasctions often reach a block proposer through a gossip network, severing the ability for the block proposer to directly communicate with the transaction submitter. Lastly, new transactions with greater fees might arrive causing the block proposer to want to shuffle the transactions around in the square. The response to these problems was to come up with "non-interactive defaults" (first mentioned in [ADR006](./adr-006-non-interactive-defaults.md)).

## Decision

Block proposers should only have the freedom to decide transaction inclusion and ordering (Note there are some existing constraints on ordering). Interaction is still permitted (in the sense that a transaction submitter can directly communicate with a block proposer) but not directly supported by the Celestia network. The non-interactive defaults become mandatory.

Square construction is thus to be reduced to the simple deterministic function:

```go
func ConstructSquare(txs []Tx) []Share
```

and it's couterpart

```go
func DeconstructSquare(shares []Share) []Tx
```

Block proposers need not know nor care about the internals of square construction. With gradual improvements to construction techniques that best incorporate the tradeoffs between square size (impacting bandwidth and storage) with proof size (impacting light clients), the latest culmination in [ADR013](./adr-013-non-interactive-default-rules-for-zero-padding.md), there is little room for validators to implement optimizations. Such existing control to block proposers also means they are able to spam the network; building full size squares that include a single PFB. Lastly, a deterministic construction mechanism has the benefit of a more compact representation of the data that can freely move between different representations (this also supports a simpler compact blocks protocol):

```go
[]Tx <=> []Share <=> EDS
```

Validation in `ProcessProposal` is simplified to reconstructing the square given the transaction set and then evaluating whether the computed data root matches the proposed one. 

## Detailed Design

A new pkg `square` will be responsible for housing the square construction logic. Any modifications to the algorithm must come as a coordinated upgrade and thus be mapped to the `AppVersion`.

Squares are bound by an upper limit on the square size, ergo not all transactions can fit in a square. Given a prioritised ordering of transactions, each transaction is individually staged and if it fits is added to the pending square else it is dropped. Each addition of a transaction has the potential to shuffle up the order of the squares (since data is ordered by namespace not order of priority). Therefore, the design conservatively estimates how much padding the blob could have and tracks the total offset to understand whether it fits in the square. This is done for both compact shares and sparse shares (which follow different rules):

For compact shares i.e. PFBs and other state transactions, we introduce a `CompactShareCounter` that tracks two fields (`shares` & `remainder`). It is centered around the `Add` method:

```go
func (c *CompactShareCounter) Add(dataLen int, remainingShareCapacity int) bool
```

If there is sufficient remaining capacity the `CompactShareCounter` will record the share and return true. The `remainder` field is used to record the remaining bytes in the ultimate share.
This emulates exactly how the encoding is done resulting in no error to the actual shares used.

For sparse shares i.e. the blobs themselves, the `SparseShareCounter` estimates the worst case intra-blob padding. This is according to the rules specified in [ADR013](./adr-013-non-interactive-default-rules-for-zero-padding.md). If the amount of shares occupied by the blobs is less than the `

> This section does not need to be filled in at the start of the ADR but must be completed prior to the merging of the implementation.
>
> Here are some common questions that get answered as part of the detailed design:
>
> - What are the user requirements?
>
> - What systems will be affected?
>
> - What new data structures are needed, and what data structures will be changed?
>
> - What new APIs will be needed, and what APIs will be changed?
>
> - What are the efficiency considerations (time/space)?
>
> - What are the expected access patterns (load/throughput)?
>
> - Are there any logging, monitoring, or observability needs?
>
> - Are there any security considerations?
>
> - Are there any privacy considerations?
>
> - How will the changes be tested?
>
> - If the change is large, how will the changes be broken up for ease of review?
>
> - Will these changes require a breaking (major) release?
>
> - Does this change require coordination with the SDK or others?

## Consequences

> This section describes the consequences, after applying the decision. All consequences should be summarized here, not just the "positive" ones.

### Positive

### Negative

### Neutral

## References

> Are there any relevant PR comments, issues that led up to this, or articles referenced for why we made the given design choice? If so link them here!

- {reference link}
