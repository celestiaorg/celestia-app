# Honest Block Proposer

<!-- toc -->

This document describes the tasks of an honest block proposer to assemble a new block. Performing these actions is not enforced by the [consensus rules](./consensus.md), so long as a valid block is produced.

## Constructing a Block

Before [arranging available data into shares](./data_structures.md#arranging-available-data-into-shares), the block proposer must select which transactions to include and determine the size of the original data square.

There are two restrictions on the original data's square size:

1. It must be at most [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`](./consensus.md#constants).
1. It must be a power of 2.

With these restrictions in mind, the block proposer performs the following actions:

1. Initialize a square builder with the maximum square size (the lesser of the governance parameter and [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`](./consensus.md#constants)).
1. Separate the available transactions from the mempool into normal transactions and blob transactions. Filter out any transactions that exceed the maximum transaction size.
1. Iterate through normal transactions and attempt to add each one to the square:
    1. If adding the transaction would cause the total share count to exceed the maximum square capacity, skip it.
    1. Validate the transaction (e.g. signature verification, fee checks). If validation fails, revert the addition and skip it.
1. Iterate through blob transactions and attempt to add each one to the square:
    1. If adding the blob transaction (including its blobs and [padding required by share commitment rules](./data_square_layout.md)) would cause the total share count to exceed the maximum square capacity, skip it.
    1. Validate the transaction. If validation fails, revert the addition and skip it.
1. Compute the smallest square size that is a power of 2 that can fit all the accepted transactions and blobs (including any [padding between blobs](./data_square_layout.md)).
1. Sort blobs by namespace (preserving the priority order of blobs within the same namespace).
1. Write out the final square: normal transaction shares, then pay-for-blob transaction shares, then reserved padding, then blob shares (with inter-blob padding as needed), then tail padding.
