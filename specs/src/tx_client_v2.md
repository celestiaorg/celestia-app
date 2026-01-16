# Tx Client v2 – Sequential Submission Queue

## Overview

Tx Client v2 enforces **per-account sequential transaction submission** to prevent sequence races and minimize transaction failures.

### Key properties

- Transactions are **signed and broadcast strictly in sequence order**
- A transaction is **never re-signed**, only **resubmitted** (which means resubmitted using the original signed bytes).

## Assumptions

- The transactions conform to minimum fee requirements, so a transaction will **eventually** be included in a block after a bounded number of retries, given that the signer has enough funds in its account.

## Error Handling

Errors are classified based on whether progress can be made without user intervention.

- **Retryable**: the client can resubmit the transaction.
- **Terminal**: the error is returned to the user and further progress requires user action.

### Non-sequence errors

Errors that are **not related to account sequence numbers**.

#### Retryable

- Network errors
- Application errors like `ErrMempoolIsFull`

##### Retryable Behavior

- Retry submission until accepted or a terminal error occurs.

#### Terminal

- Application errors like `ErrTxTooLarge`

##### Terminal Behavior

- Return the error to the user immediately.

### Sequence mismatch errors

Sequence mismatch errors occur when the node's expected account sequence differs from the client's sequence during submission.

#### Case 1: Expected sequence < client sequence

Indicates eviction or rejection of one or more previously submitted transactions that is not yet known to the client (e.g. eviction or rejection during `ReCheckTx`).

##### Case 1 Behavior

- Pause broadcasting.
- Query the status of transactions in the range
  `[expected_sequence, last_submitted_sequence]`.

For each transaction in order:

- **Evicted** (Retryable)
  - Resubmit the transaction using the original signed bytes.
- **Rejected** (Terminal)
  - Roll back the client sequence to the rejected transaction's sequence.
  - Return the rejection error to the user.

#### Case 2: Expected sequence > client sequence

Indicates that the node's CheckTx state has advanced beyond the client's state.

This can happen for one of the following reasons:

- The transaction was evicted on the current node (thus not in the mempool), but was included in a block on another node.
- A different transaction with the same sequence was included in a block, implying that a transaction for the signer was submitted outside of the tx client.

##### Case 2 Behavior

1. Pause broadcasting.
2. Query the status of the transaction with the conflicting sequence.
3. If the transaction is **Committed**:
   - Treat the mismatch as **Retryable**.
   - Advance client state accordingly.
   - Resume broadcasting.
4. If the transaction is **not committed**:
   - Treat the mismatch as **Terminal**.
   - Return an error indicating that the client state has conflicting transaction with same sequence number.

## Confirmation and Monitoring

Transaction statuses are obtained via **batched status queries** using the `tx_status_batch` RPC method over a bounded set of pending transactions.

### Transaction States

#### Pending

- No action required.

#### Committed

- The transaction was included in a block.
- Remove from the queue.
- Return success with execution metadata.

#### Committed with execution error

- The transaction was included in a block but failed during execution.
- Remove from the queue.
- Return execution error with metadata.

#### Evicted

- Broadcasting is paused.
- Resubmit the transaction.
- Resume broadcasting.

#### Rejected

- Broadcasting is paused.
- Roll back the sequence to the first rejected transaction.
- Return rejection error to the user.

#### Unknown

- The transaction is neither evicted, rejected, nor committed.
- Return an error indicating that the transaction status is unknown.

## Implementation Notes

- Each account has a **submission queue** that owns transaction sequencing, signing, and submission.
- Confirmations are processed by a separate **confirmation queue/worker** that polls transaction statuses.
- Confirmation results are fed back to the submission queue, which remains the single point of coordination per account.
