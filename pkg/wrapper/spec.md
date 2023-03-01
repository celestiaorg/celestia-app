# Introduction
In Celestia, transactions are grouped into identical-size shares and arranged in a `k` by `k` matrix, called [original data square](https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/data_structures.md#arranging-available-data-into-shares).
The rows and columns of the original data square is then extended using Reed-Solomon erasure coding.
The extended version is called [extended data square]() which consists of 4 quadrants namely, `Q0`, `Q1`, `Q2`, and `Q3`.
Figure 1 demonstrates a sample data square and its extended version.
`Q0` corresponds to the original data square.
`Q1` is the row-wise erasure coded data of `Q0`.
`Q2` is the column-wise erasure coded data of `Q0`.
`Q3` is the erasure coded data of `Q2`.
The extended version is a `2k` by `2k` square.
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


The rows and columns of the extended data square, each, is modeled by a [Namespace Merkle tree (ENMT)]().
NMTs require data items they represent to be namespaced.
To this end, the shares within each row or column of the extended data square needs to be namespaced before being added to the NMT.
This is where the [Erasured Namespaced MerkleTree]() comes in.
It wraps around the [Namespaced MerkleTree]() data structure and takes care of appending proper namespaces to the shares prior to being added to their respective row or column NMT.
In this specification, we elaborate on the design and implementation of the Erasured Namespaced MerkleTree wrapper.

## Namespace ID assignment to the shares of an extended data square
Before presenting the ENMT data structure, we shall understand how the shares are namespaced in Celestia.
Shares that belong to the original data square i.e., `Q0` already carry their namespace IDs, i.e., the first `NAMESPACE_ID_BYTES` of each share represents its namespace ID.
However, the shares in `Q1`, `Q2`, and `Q3`, are the erasure coded version of `Q0` and by default do not carry any namespace IDs.
Such shares are called [Parity shares](), and MUST be assigned a predefined namespace ID namely, [`ParitySharesNamespaceID`]().
At the time of the writeup of this specification, the namespace ID size in Celestia is `8` bytes. 
In this setting, the `ParitySharesNamespaceID` is an 8 byte slice of `0xFF` values.

The NMT wrapper performs namespace ID assignment to the shares according to the predefined rules.

Each row/column of the data square is represented by an Erasured Namespaced Merkle Tree.
By construction,  some of the rows may overlap with the erasure coded data i.e., with `Q1`, `Q2`, and `Q3`.
The extended data square has 4 quadrants, namely, `Q0`, `Q1`, `Q2`, and `Q3`.
As mentioned in the introduction, the original data is stored in `Q0`.




## Erasured Namespaced Merkle Tree
Erasured Namespaced Merkle Tree is used to represent a row or column of Celestia extended data square.
At its core, it is an [NMT]() with the same exact functionalities???, and configuration options. 
It further extends the NMT data insertion behaviour (i.e., the [`Push` method]()) to prepend shares with proper namespace, according to the quadrants they belong to, before insertion.

To be able to properly determine and assign namespace IDs  to the inserted shares, ENMT is configured with the data square size, `k` in the above example, and the index of the row or column it represents `axis_index`.
Based on these two piece of information, together with the index of the inserted share in that respective row or column i.e., `share_index`, it determines whether the share is within the original data square or not.
A share is in `Q0` if and only if both of the following conditions are satisfied:
```
share_index < square_size && axis_index < square_size
```
Otherwise, the share is in `Q1`, `Q2`, or `Q3`.

If the added item falls in the original data square, it's first `NAMESPACE_ID_BYTES` byte is treated as its namespace ID (as explained in [Namespace ID assignment to the shares of an extended data square](#namespace-id-assignment-to-the-shares-of-an-extended-data-square), and the share is further prepended with the same nID.
If the added share is not within `Q0` i.e, it is a parity share, then it is prepended with the `ParitySharesNamespaceID` before getting pushed into the NMT.

This implies that in the ultimate ENMT, every leaf node has a size equal to the original data share ([`SHARE_SIZE`](https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/consensus.md#constants)) plus the size of the namespace ID ([`NAMESPACE_ID_BYTES`](https://github.com/celestiaorg/celestia-app/blob/specs-staging/specs/src/specs/consensus.md#constants)). 
In other words, the size of each leaf node is `SHARE_SIZE + NAMESPACE_ID_BYTES`.
Note that, leaves in the NMT are ordered by their namespace ID.

Some insightful observations can be made from the above description:
- For every row and column that has overlap with `Q0`, it is the case that the shares in the first half of the tree leaves  belong to `Q0`, whereas the second half of the leaves are the erasure coded version of the first half.
 This means, the second half of the tree leaves all have identical namespace IDs, i.e., `ParitySharesNamespaceID`.
- Leaves corresponding to shares of `Q0` are actually doubly namespaced: the first and the second `NAMESPACE_ID_BYTES` bytes of the leaf both contain the namespace ID of the share.


@TODO How about ignore max namespace ID? 
