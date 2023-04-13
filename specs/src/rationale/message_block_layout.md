# blob Layout

<!-- toc -->

## Preamble

Celestia uses [a data availability scheme](https://arxiv.org/abs/1809.09044) that allows nodes to determine whether a block's data was published without downloading the whole block. The core of this scheme is arranging data in a two-dimensional matrix then applying erasure coding to each row and column. This document describes the rationale for how data—transactions, blobs, and other data—[is actually arranged](../specs/data_structures.md#arranging-available-data-into-shares). Familiarity with the [originally proposed data layout format](https://arxiv.org/abs/1809.09044) is assumed.

## blob Layout Rationale

Block data consists of:

1. Cosmos SDK module transactions (e.g. [MsgSend](https://github.com/cosmos/cosmos-sdk/blob/f71df80e93bffbf7ce5fbd519c6154a2ee9f991b/proto/cosmos/bank/v1beta1/tx.proto#L21-L32)). These modify the Celestia chain's state.
1. Celestia-specific transactions (e.g. [PayForBlobs](../specs/data_structures.md#payforblobdata)). These modify the Celestia chain's state.
1. Intermediate state roots: required for fraud proofs of the aforementioned transactions.
1. blobs: binary blobs which do not modify the Celestia state, but which are intended for a Celestia application identified with a provided namespace ID.

We want to arrange this data into a `k * k` matrix of fixed-sized shares, which will later be committed to in [Namespace Merkle Trees (NMTs)](../specs/data_structures.md#namespace-merkle-tree).

The simplest way we can imagine arranging block data is to simply serialize it all in no particular order, split it into fixed-sized shares, then arrange those shares into the `k * k` matrix in row-major order. However, this naive scheme can be improved in a number of ways, described below.

First, we impose some ground rules:

1. Data must be ordered by namespace ID. This makes queries into a NMT commitment of that data more efficient.
1. Since non-blob data are not naturally intended for particular namespaces, we assign reserved namespaces for them. A range of namespaces is reserved for this purpose, starting from the lowest possible namespace ID.
1. By construction, the above two rules mean that non-blob data always precedes blob data in the row-major matrix, even when considering single rows or columns.
1. Data with different namespaces must not be in the same share. This might cause a small amount of wasted block space, but makes the NMT easier to reason about in general since leaves are guaranteed to belong to a single namespace.

Transactions can pay fees for a blob to be included in the same block as the transaction itself. However, we do not want serialized transactions to include the entire blob they pay for (which is the case in other blockchains with native execution, e.g. calldata in Ethereum transactions or OP_RETURN data in Bitcoin transactions), otherwise every node that validates the sanctity of the Celestia coin would need to download all blob data. Transactions must therefore only include a commitment to (i.e. some hash of) the blob they pay fees for. If implemented naively (e.g. with a simple hash of the blob, or a simple binary Merkle tree root of the blob), this can lead to a data availability problem, as there are no guarantees that the data behind these commitments is actually part of the block data.

To that end, we impose some additional rules onto _blobs only_: blobs must be placed is a way such that both the transaction sender and the block producer can be held accountable—a necessary property for e.g. fee burning. Accountable in this context means that

1. The transaction sender must pay sufficient fees for blob inclusion.
1. The block proposer cannot claim that a blob was included when it was not (which implies that a transaction and the blob it pays for must be included in the same block).

Specifically, blobs must begin at a new share, unlike non-blob data which can span multiple shares. We note a nice property from this rule: if the transaction sender knows 1) `k`, the size of the matrix, 2) the starting location of their blob in a row, and 3) the length of the blob (they know this since they are sending the blob), then they can actually compute a sequence of roots to _subtrees in the row NMTs_. More importantly, anyone can compute this, and can compute _the simple Merkle root of these subtree roots_.

This, however, requires the block producer to interact with the transaction sender to provide them the starting location of their blob. This can be done selectively, but is not ideal as a default for e.g. end-user wallets.

### Non-Interactive Rules

We can impose one additional rule on blob placement to make the starting locations of blobs predictable and constrained such that users can deterministically compute subtree roots without interaction:

> Blobs must start at an index that is a multiple of the subtree width of that blob.

Picking a subtree width is a tradeoff between the proof size and the amount of padding added to the square. If the subtree width is large, then there are fewer subtree roots whose inclusion needs to be proven. However, this also means that the blob will be padded with more empty shares because of the above constraint. To optimize this tradeoff, a constant threshold is used. Should the number of subtrees cross above that threshold, then the subtree size for a given blob will increase.

The subtree width is therefore computed as follows:

- Divide the length of the blob in shares by the SubtreeSizeThreshold.
- Use the Ceil of this value.
- Round up to the nearest power of 2.
- Use the minimum of this value and the blob minimum square size.
- Use the maximum of this value and the minimum square size.

As an example, using a `SubTreeSizeThreshold` of 8, a blob of length `11` has a blob sub tree width of `2` because, using a subtree width of 1 share, `11` will cross the subtree threshold of 8. Then we round op to the next power of 2 to get 2. Split the blob into chunks of length `2, 2, 2, 2, 2, 1`. The resulting slices are the leaves of subtrees whose roots can be computed. These subtree roots will be present as internal nodes in the NMT of _some_ row(s).

This is similar to [Merkle Mountain Ranges](https://www.usenix.org/legacy/event/sec09/tech/full_papers/crosby.pdf), though with the largest subtree bounded by the blob minimum square size rather than being unbounded.

The last piece of the puzzle is determining _which_ row the blob is placed at (or, more specifically, the starting location). This is needed to keep the block producer accountable. To this end, the block producer simply augments each fee-paying transaction with some metadata: the starting location of the blob the transaction pays for.

### Caveats

The blob placement rules described above conflict with the first rule that shares must be ordered by namespace ID, as shares between two blobs that are not placed adjacent to each other do not have a natural namespace they belong to. This is resolved by requiring that such shares have a value of zero and a namespace ID equal to the preceding blob's. Since their value is known, they can be omitted from NMT proofs of all shares of a given namespace ID.
