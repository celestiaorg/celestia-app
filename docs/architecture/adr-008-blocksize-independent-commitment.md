# ADR 008: Blocksize independent Commitments for Messages

## Changelog

- 03.08.2022: Initial Draft

## Context

Currently, commitments in Celestia are dependent on the size of the block. The following diagram visualizes how this can happen. The visualizations in this ADR are without parity shares/erasure encoding.
The yellow block in the diagram is a message of length 11 shares. A square of 4x4 results in the following subtree roots: `A B C D`.
In [`CreateCommit`](https://github.com/celestiaorg/celestia-app/blob/0c81704939cd743937aac2859f3cb5ae6368f174/x/payment/types/payfordata.go#L112-166), we calculate the commitment of the subtree roots with [`HashFromByteSlices`](https://github.com/celestiaorg/celestia-core/blob/v0.34.x-celestia/crypto/merkle/tree.go#L7-L21). This resulting merkleroot is `Com`.
A square of 8x8 results in the following subtree roots: `H1 C D`
[`HashFromByteSlices`](https://github.com/celestiaorg/celestia-core/blob/v0.34.x-celestia/crypto/merkle/tree.go#L7-L21) results now in the merkleroot `Com'`, which is not equal to `Com`.

![Old Commitment](./assets/size-dependent-commitment.png)

To have a commitment independent of the block size, you would need to create a merkle tree over subtree roots that are included in every possible block size.
We agree on producing the commitment as if the message was put into the `minimalBlocksize`. Therefore the subtree roots will be the same and the resulting commitment as well, no matter the block size. *This is because all the subtree roots in the `minimalBlocksize` are also included in bigger block sizes*.
You can see this in the updated version of the diagram above. Now the merkle root will stay the same. This property results from the block width and length being a power of 2.  

![New Commitment](./assets/size-independent-commitment.png)

You can see in the diagramm below that no matter how the message is split up into rows, the hashes of the subtree roots stay the same.

![Row Size Comparison](./assets/row-size-comparison.png)

This scheme works for interactive commitments as well as long as the index of when a message starts is the same in every block size.
For example, in the following diagram, you see the message starting at the second share and still having the same commitment. I marked the skipped blocks pink to show that both messages have the same starting index.

![Interactive Commitment](./assets/interactive-commitment.png)

If the message starting point index is larger than the row of `minimalBlocksize`, you take the index mod `minimalBlocksize`. The `minimalBlocksize` is shown in green, and the skipped block is in pink. In both cases, the commitment stays the same.

![Interactive Commitment 2](./assets/interactive-commitment2.png)

## Alternative Approaches

TODO

## Decision

TODO

## Detailed Design

To implement this decision, you need to change [`CreateCommit`](https://github.com/celestiaorg/celestia-app/blob/0c81704939cd743937aac2859f3cb5ae6368f174/x/payment/types/payfordata.go#L112-166).
In Detail, [`powerOf2MountainRange`](https://github.com/celestiaorg/celestia-app/blob/0c81704939cd743937aac2859f3cb5ae6368f174/x/payment/types/payfordata.go#L142) should take `minimalBlocksize` as an argument instead of `squareSize`.

Following the non-interactive default rules, `minimalBlocksize` can be calculated like this:

```go
func minimalBlocksize(mLength uint64) uint64 {
    blocksize := NextHigherPowerOf2(mLength)
    //Check if message fits with non-interactive-deafult rules
    if mLength <= (blocksize * (blocksize -1)) {
        return blocksize
    } else {
        return blocksize << 1
    }
}
```

## Status

Proposed

## Consequences

### Negative

1. The amount of subtree roots per commitment is O(sqrt(n)), while n is the number of message shares. The worst case for the number of subtree roots is depicted in the diagram below - an entire block missing one share.  
  ![Interactive Commitment 2](./assets/complexity.png)  
The worst case for the current implementation depends on the block size. If it is the worst block size as in `minimalBlocksize`, it is O(sqrt(n)) as well. On the other hand, if the message is only in one row, then it is O(log(n)).
Therefore the height of the tree over the subtree roots is in this implementation O(log(sqrt(n))), while n is the number of message shares. In the current implementation, it varies from O(log(sqrt(n))) to O(log(log(n))) depending on the block size.

2. With more subtree roots, the amount of merkle proofs will increase. With deeper subtree roots, the size of the merkle proofs will increase. So instead of having a merkle proof from the `DataRoot` to the `originalSubRoot` you will need the merkle proof from `DataRoot` to `2^k` amount of `miniSubRoots` with `k` being the height difference of `originalSubRoot` and `miniSubRoots`. You can optimize this by having only **one** merkle proof from `DataRoot` to `originalSubRoot` and then calculate the `originalSubRoot` from the `k` `miniSubRoots`. Because the merkle proof is created after the block is published, we know the block size and, therefore, if this `originalSubRoot` exists or not.

### Positive

1. A Rollup can include the commitment in the block header *before* posting to Celestia because it is size-independent and does not have to wait for Celestia to confirm the block size. In general, the rollup needs access to this commitment in some form to verify a message inclusion proof guaranteeing data availability, which Rollmint currently does not have access to.
2. In turn, this would serve as an alternative to [ADR 007](https://github.com/celestiaorg/optimint/blob/main/docs/lazy-adr/adr-007-header-commit-to-shares.md)
3. Here is one scheme on how a Rollup might use this new commitment in the block header. Let's assume a Rollup that looks like this:  
  BH1 <-- BH2 <-- BH3 <-- BH4  
  The Messages that are submitted to Celestia could look like this:  
  Message 1: B1  
  The Commitment of B1 is saved into BH1.  
  Message 2: (BH1+B2)  
  The Commitment of BH1+B2 is saved into BH2.  
  Message 3: (BH2+B3)  
  The Commitment of (BH2+B3) is saved into BH3, and so on.  
4. Verifying a message inclusion proof could be done with merkle proofs of the subtree roots to the `DataRoot`, recalculating the commitment, and comparing to what's in the rollup block header. If you trust the validator set of Celestia/Rollup (aka Rollmint Light Client) it could be as simple as submitting a proof over the PFD transaction that included the message and then check if the commitment is the same as in the PFD transaction.
5. So far, a full node in Rollmint downloads new blocks from the DA Layer after each Celestia block, coupled tightly for syncing. With this approach, we can send blocks over the p2p Layer giving a soft-commit to full nodes. Then, they would receive the hard-commit after verifying a message inclusion proof without the need to download the blocks anymore. **P2P Blocksync**
    1. P2P Blocksync allows a Rollmint full node to run a Celestia light node and not a Celestia full node.
    2. It allows the Rollup node to continue running after Celestia halts, relying on soft commits with no data availability.
    3. It gives the Rollup the option to run asynchronously to Celestia because you don't have to wait for new Celestia blocks/commitments of the messages.
6. Combining P2P Blocksync and the scheme in 3, we could have multiple rollup blocks in one Celestia block. It could look like this:  
  ![multiple-blocks](./assets/multiple-blocks.png)
7. When submitting a message to Celestia, you only sign the message over one commitment and not all block sizes.

### Neutral
