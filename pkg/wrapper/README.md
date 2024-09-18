# Namespaced Merkle Tree Wrapper

## Abstract

In Celestia, block transactions are grouped into identical-size shares and arranged in a `k` by `k` matrix called [original data square][originalds-link].
According to the Celestia consensus rules, `k` must be a power of `2`, and the share size is determined by the  [`SHARE_SIZE` parameter][celestia-constants-link].
The rows and columns of the original data square are then extended using a 2D Reed-Solomon coding scheme.
The [extended version][reedsolomon-link] is called extended data square, that is a `2k` by `2k` square consisting of `4` quadrants namely, `Q0`, `Q1`, `Q2`, and `Q3`.
Figure 1 provides an illustration of a sample data square and its extended version.
`Q0` corresponds to the original data square.
`Q1` and `Q2` represent the horizontal and vertical extensions of `Q0`, respectively.
`Q3` is the horizontal extension of `Q2` or alternatively, it can be considered as the vertical extension of `Q1`.
Additional information about the extension logic can be found in the specifications of the [2D Reed-Solomon encoding scheme][reedsolomon-link].
<img src="https://raw.githubusercontent.com/celestiaorg/celestia-app/c09843d07d4c3842753138de96b304b4866e8f5d/specs/src/specs/figures/rs2d_extending.svg" alt="Figure 1. Extended Data Square." style="max-width: 50%; height: auto;">

Figure 1. `r` and `c` stand for row and column, respectively.

Each row and column of the extended data square is modeled by a [Namespace Merkle Tree][nmtlink].
NMTs require the data items they represent to be namespaced, which means that the shares within each row or column of the extended data square must be namespaced before being added to the NMT.
This is where the Namespaced Merkle Tree Wrapper, more specifically, [`ErasuredNamespacedMerkleTree`][nmtwrapper-link] comes into play.
It is a data structure that wraps around the [Namespaced Merkle Tree][nmtlink] and ensures the proper namespaces are prepended to the shares  before they are added to their respective row or column NMT.
In this specification, we elaborate on the design and structure of the Namespace Merkle Tree wrapper.

## Namespaced Merkle Tree Wrapper Structure

Namespaced Merkle Tree Wrapper is used to represent a row or column of Celestia extended data square.
At its core, it is an [NMT][nmtlink] and is defined by a similar set of parameters.
Namely, the [namespace ID size `NamespaceIDSize`][nmt-ds-link],
the underlying hash function for the digest calculation of the [namespaced hashes][nmt-hash-link],
and the [`IgnoreMaxNamespace` flag][nmt-ignoremax-link] which dictates how namespace range of non-leaf nodes are calculated.
In addition, the NMT wrapper is configured with the original data square size `SquareSize` (`k` in the above example), and the index of the row or column it represents `AxisIndex` which is a value in `[0, 2*SquareSize)`.
These additional configurations are used to determine the namespace ID of the shares that the NMT wrapper represents based on the quadrants to which they belong.

NMT wrapper supports [Merkle inclusion proof][nmtlink] for the given share index and [Merkle range proof][nmtlink] for a range of share indices.
It extends the NMT data insertion behaviour (i.e., the [`Push` method][nmt-add-leaves-link]) to prepend shares with proper namespace before inclusion in the tree.

### Namespace ID assignment to the shares of an extended data square

To understand the NMT wrapper data structure, it's important to first understand how shares are namespaced in Celestia.
The namespace ID of a share depends on the quadrant it belongs to.

Shares in the original data square `Q0` already carry their namespace IDs, which are located in the first `NamespaceIDSize` bytes of the share.
Thus, the namespace ID of a share in `Q0` can be extracted from the first `NamespaceIDSize` bytes of the share.

However, shares in `Q1`, `Q2`, and `Q3` (that are erasure-coded versions of `Q0` and known as parity shares) do not have any namespace IDs by default.
These shares must be assigned a reserved namespace ID, which is called `ParitySharesNamespaceID`.
The `ParitySharesNamespaceID` corresponds to the lexicographically last namespace ID that can be represented by `NamespaceIDSize` bytes.
If `NamespaceIDSize` is `8`, then the value of `ParitySharesNamespaceID` is `2^64-1`, which is equivalent to `0xFFFFFFFFFFFFFFFF`.
In Celestia, the values for `NamespaceIDSize` and `ParitySharesNamespaceID` can be found in [`NAMESPACE_SIZE`][celestia-constants-link] constant and the [`PARITY_SHARE_NAMESPACE`][celestia-consensus-link], respectively.

### NMT Wrapper Data Insertion

The NMT wrapper insertion logic is governed by the same rules as the NMT insertion logic.
However, it takes care of namespace ID assignment to the shares before inserting them into the tree.
During the insertion, the wrapper first checks whether the share is within the original data square or not.
A share with the index `ShareIndex` where `ShareIndex` is a value between `[0, SquareSize)`, belongs to the original data square if and only if both of the following conditions are satisfied:

```markdown
ShareIndex < SquareSize && AxisIndex < SquareSize
```

Otherwise, the share is in `Q1`, `Q2`, or `Q3`.

If the added item falls in the original data square, it's first `NamespaceIDSize` bytes are treated as its namespace ID (as explained in [Namespace ID assignment to the shares of an extended data square](#namespace-id-assignment-to-the-shares-of-an-extended-data-square), and the share is further prepended with the same namespace ID before getting pushed into the tree.
If the added share is not within `Q0` i.e, it is a parity share, then it is prepended with the `ParitySharesNamespaceID` before getting pushed into the tree.

**Some insightful observations can be made from the NMT wrapper description**:

- In the NMT wrapper, the shares are extended in size by `NamespaceIDSize` bytes before insertion to the tree.
- For every row and column that overlaps with `Q0`, it is the case that the shares in the first half of the tree leaves  belong to `Q0`, whereas the second half of the leaves are the erasure coded version of the first half.
 This means, the second half of the tree leaves all have identical namespace IDs, i.e., `ParitySharesNamespaceID`.
- Each leaf in the NMT wrapper that corresponds to shares in `Q0` has a doubly namespaced structure.
Specifically, the underlying data of the leaf contains the namespace ID of the share twice.
One namespace ID is located in the first `NamespaceIDSize` bytes, while the other is located in the second `NamespaceIDSize` bytes.

## References

- Namespaced Merkle tree specifications: <https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md>
- Celestia original data square specification: <https://github.com/celestiaorg/celestia-app/blob/main/specs/src/data_structures.md#arranging-available-data-into-shares>
- Celestia constants: <https://github.com/celestiaorg/celestia-app/blob/main/specs/src/consensus.md#constants>
- Celestia reserved namespace IDs: <https://github.com/celestiaorg/celestia-app/blob/main/specs/src/consensus.md#reserved-namespace-ids>

[nmtlink]: https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md
[nmtwrapper-link]: https://github.com/celestiaorg/celestia-app/blob/main/pkg/wrapper/nmt_wrapper.go
[nmt-ds-link]:  https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md#nmt-data-structure
[nmt-hash-link]: https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md#namespaced-hash
[nmt-ignoremax-link]: https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md#ignore-max-namespace
[nmt-add-leaves-link]: https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md#add-leaves
[celestia-constants-link]: https://github.com/celestiaorg/celestia-app/blob/c09843d07d4c3842753138de96b304b4866e8f5d/specs/src/specs/consensus.md#constants
[celestia-consensus-link]: https://github.com/celestiaorg/celestia-app/blob/c09843d07d4c3842753138de96b304b4866e8f5d/specs/src/specs/consensus.md#reserved-namespace-ids
[reedsolomon-link]: https://github.com/celestiaorg/celestia-app/blob/c09843d07d4c3842753138de96b304b4866e8f5d/specs/src/specs/data_structures.md#2d-reed-solomon-encoding-scheme
[originalds-link]: https://github.com/celestiaorg/celestia-app/blob/c09843d07d4c3842753138de96b304b4866e8f5d/specs/src/specs/data_structures.md?plain=1#L494
