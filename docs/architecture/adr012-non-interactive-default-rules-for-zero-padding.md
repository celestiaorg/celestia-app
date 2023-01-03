# ADR 012: Non-Interactive Default Rules for Zero Padding

## Changelog

- 03.01.12: initial draft

## Context

When laying out blobs in the square we create padding to follow the non-interactive default rules. With ADR 009 we decreased padding by aligning the blob in the square to the index of the `MinSquareSize` of the given blob. This is a good improvement but we can do better.

If you align the blob at an index of one you get zero padding as each blob can follow the next. This would make the hash of each square a subtree root. In a square with n shares, you would always get n subtree roots. But having a blob inclusion proof of size n for large blobs is too much and unfeasible.
Assuming there is a threshold, the number of subtree roots in a proof, where the proof size of a blob is acceptable we can use this threshold to determine the index of the blob in the square. This would give us zero padding for blobs that are smaller than the threshold and non-zero padding for blobs that are larger than the threshold but still smaller than before.

Let's assume a good threshold assumption is that the number of subtree roots in a blob inclusion proof is acceptable if it is smaller than the `MaxSquareSize`. This would mean that the blobs smaller than the square size can use the index of one to get zero padding. Blobs that are larger than `MaxSquareSize` but smaller than `MaxSquareSize * 2` can use the index of two to get a maximum of 1 padding square. Blobs that are larger than `MaxSquareSize * 2` but smaller than `MaxSquareSize * 4` can use the index of 4 to get a maximum of 2 padding squares and so on.

The new non-interactive default rules would be:

Blobs start at an index that is equal to the blob length/`MaxSquareSize` rounded up. If the blob length is smaller than `MaxSquareSize` then the blob starts at index 1.

`MaxSquareSize` can be changed to another threshold. The smaller the threshold the more padding we will have.

## Status

Proposed

## Consequences

### Light-Nodes

The Proof size is bounded by the number of subtree roots in the blob inclusion proof. If the new bound is the `MaxSquareSize` then the worst case for the number of subtree roots in a blob inclusion proof will be `MaxSquareSize`.

If Light-Nodes can process this proof size without a problem then we can use this bound. If not we can use a smaller bound. The smaller the bound the more padding we will have.

In addition, we could use PFB inclusion proofs (ADR 11) to reduce the proof size of the blob inclusion proof for Light-Nodes. This would make this change not noticeable to them as they are blob size independent until we need a fraud-proof for a malicious PFB inclusion. This fraud-proof would still be magnitudes smaller than a bad encoding fraud-proof.

Both cases require 2/3 of the Celestia validators to be malicious. In both cases, the chain would halt and fall back to social consensus. If a Light-Node can process the bad encoding fraud-proof then it can also process the PFB fraud-proof easily.

### Partial-Nodes

Partial nodes in this context are Celestia-node light nodes that may download all of the data in the reserved namespace. They check that the data behind the PFB was included in the `DataRoot`, via blob inclusion proofs.

The sum of the size of all blob inclusion proofs will be larger than the sum of all blob inclusion proofs with the previous non-interactive default rules.

In the current worst case a Celestia block is full of blobs of size 1, making the total amount of subtree roots that need to be downloaded O(n) where n is the number of shares in a block. With the new non-interactive default rules, the worst case stays the same, but the average case goes up. If the block is filled with blobs that are smaller than the threshold then the partial node will still need to download O(n) subtree roots.

### Positive

Most Blocks will have close to zero padding.

### Negative

The number of subtree roots to download for Partial-Nodes will increase in the average case.

### Neutral

The number of subtree roots to download for Light-Nodes will increase in the average case. This effect can be mitigated by using PFB inclusion proofs.
