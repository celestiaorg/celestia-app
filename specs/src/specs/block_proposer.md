# Honest Block Proposer

<!-- toc -->

This document describes the tasks of an honest block proposer to assemble a new block. Performing these actions is not enforced by the [consensus rules](./consensus.md), so long as a valid block is produced.

## Deciding on a Block Size

Before [arranging available data into shares](./data_structures.md#arranging-available-data-into-shares), the size of the original data's square must be determined.

There are two restrictions on the original data's square size:

1. It must be at most [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`](./consensus.md#constants).
1. It must be a power of 2.

With these restrictions in mind, the block proposer performs the following actions:

1. Collect as many transactions and messages from the mempool as possible, such that the total number of shares is at most [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`](./consensus.md#constants).
1. Compute the smallest square size that is a power of 2 that can fit the number of shares.
1. Attempt to [lay out the collected transactions and messages](#laying-out-transactions-and-messages) in the current square.
    1. If the square is too small to fit all transactions and messages (which may happen [due to needing to insert padding between messages](../rationale/message_block_layout.md)) and the square size is smaller than [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`](./consensus.md#constants), double the size of the square and repeat the above step.

Note: the maximum padding shares between messages should be at most twice the number of message shares. Doubling the square size (i.e. quadrupling the number of shares in the square) should thus only have to happen at most once.

## Laying out Transactions and Messages
