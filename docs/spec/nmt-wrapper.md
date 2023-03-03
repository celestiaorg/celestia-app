# Namespaced Merkle Tree Wrapper
## Abstract
In Celestia, block transactions are grouped into identical-size shares and arranged in a `k` by `k` matrix, called [original data square](https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/data_structures.md#arranging-available-data-into-shares).
The rows and columns of the original data square is then extended using 2D Reed-Solomon coding scheme.
The extended version is called extended data square, that is a `2k` by `2k` square consisting of `4` quadrants namely, `Q0`, `Q1`, `Q2`, and `Q3`.
Figure 1 provides an illustration of a sample data square and its extended version.
`Q0` corresponds to the original data square.
`Q1` is the row-wise erasure coded data of `Q0`.
`Q2` is the column-wise erasure coded data of `Q0`.
`Q3` is the erasure coded data of `Q2`.
```markdown
  k         k
 -------   -------
|        |        |
|   Q0 → |   Q1   | k
|   ↓    |        |
-------  -------
|        |        |
|   Q2 → |   Q3   | k
|        |        |
 -------   -------
```
Figure 1.

Each row and column of the extended data square is modeled by a [Namespace Merkle Tree](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md).
NMTs require the data items they represent to be namespaced, which means that the shares within each row or column of the extended data square must be namespaced before being added to the NMT.
This is where the [Namespaced MerkleTree Wrapper](https://github.com/celestiaorg/celestia-app/blob/main/pkg/wrapper/nmt_wrapper.go)  comes into play.
It is a data structure that wraps around the [Namespaced Merkle Tree](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md) that ensures the proper namespaces are prepended to the shares  before they are added to their respective row or column NMT.
In this specification, we elaborate on the design and structure of the Namespace Merkle Tree wrapper.



## Namespaced Merkle Tree Wrapper
Namespaced Merkle Tree Wrapper is used to represent a row or column of Celestia extended data square.
At its core, it is an [NMT](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md) and is defined by a similar set of parameters.
Namely, the [namespace ID size `NamespaceIDSize`](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md#nmt-data-structure), 
the underlying hash function for the digest calculation of the [namespaced hashes](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md#namespaced-hash), 
and the [`IgnoreMaxNamespace` flag](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md#ignore-max-namespace) which dictates how namespace range of non-leaf nodes are calculated.
In addition, the NMT wrapper is configured with the data square size `SqaureSize` (`k` in the above example), and the index of the row or column it represents `AxisIndex` which is a value in `[0, SquareSize)`.
These additional configurations are used to determine the namespace ID of the shares that the NMT wrapper represents based on the quadrants to which they belong.

NMT wrapper supports [Merkle inclusion proof](#link-to-the-nmt-spec-for-the-inclusion-proof) for the given share index and [Merkle range proof](#link-to-the-nmt-spec-for-the-range-proof) for a range of share indices.
It extends the NMT data insertion behaviour (i.e., the [`Push` method](https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md#add-leaves)) to prepend shares with proper namespace before inclusion in the tree.

### Namespace ID assignment to the shares of an extended data square
To understand the NMT wrapper data structure, it's important to first understand how shares are namespaced in Celestia.
The namespace ID of a share depends on the quadrant it belongs to.

Shares in the original data square `Q0` already carry their namespace IDs, which are located in the first `NamespaceIDSize` bytes of the share. 
Thus, the namespace ID of a share in `Q0` can be extracted from the first `NamespaceIDSize` bytes of the share.

However, shares in `Q1`, `Q2`, and `Q3` (that are erasure-coded versions of `Q0` and known as parity shares) do not have any namespace IDs by default. 
These shares must be assigned a reserved namespace ID, which is called `ParitySharesNamespaceID`.
`ParitySharesNamespaceID` corresponds to the maximum value representable by `NamespaceIDSize` bytes.

In Celestia, as of the writing of this specification, the `NamespaceIDSize` (which maps to the [`NAMESPACE_ID_BYTES`](https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/consensus.md#constants) constant) is `8` bytes, and therefore, [`ParitySharesNamespaceID`](https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/consensus.md#reserved-namespace-ids)  is represented by `8` bytes of `0xFF`.


### NMT Wrapper Data Insertion
The NMT wrapper insertion logic is governed by the same rules as the NMT insertion logic.
However, it takes care of namespace ID assignment to the shares before inserting them into the tree.
During the insertion, the wrapper first checks whether the share is within the original data square or not.
A share with the index `ShareIndex` where `ShareIndex` is a value between `[0, SquareSize)`, belongs to the original data square if and only if both of the following conditions are satisfied:
```
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
- Namespaced Merkle tree specifications: https://github.com/celestiaorg/nmt/blob/master/spec/nmt.md
- Celestia original data square specification: https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/data_structures.md#arranging-available-data-into-shares
- Celestia constants: https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/consensus.md#constants
- Celestia reserved namespace IDs: https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/consensus.md#reserved-namespace-ids