# Tx Client v2 – Sequential Submission Queue

### Context & Motivation

Tx Client v2 enforces a per-account sequential submission queue that serializes signing, submission, and recovery, avoiding sequence races and minimizing transaction failures.

## Protocol/Component Description

### Per account sequential queue

Every account gets its own **transaction queue**.

This queue is in charge of everything for that account: taking new transactions, broadcasting them in submission order, reporting outcomes.

Inside the queue we keep two kinds of transactions:

- **Queued** - submitted but not broadcast yet
- **Pending** - already broadcast and waiting for confirmation

---

### Broadcast and Confirmation

Each queue handles two main responsibilities:

1. **Submission**: Transactions are signed and broadcast in order. Evicted transactions are resubmitted.
2. **Monitoring**: Transaction statuses are polled and processed, evictions are reported for resubmission.

---

### Submission flow

**Accepting transactions**

When a user submits a transaction:

- The transaction is queued with its options and context
- Memory based backpressure is enforced:
  - If memory is available, transaction is accepted
  - If memory limit is exceeded, the submission will block until memory is freed or a timeout expires
  - On timeout an error will be returned
- Transactions are not signed or broadcast at submission time
- Results are delivered asynchronously

**Broadcasting transactions**

Transactions have to be broadcast strictly in submission order:

- Broadcasting is blocked when the queue is in recovery mode
- Each transaction is signed with the next sequence number before broadcast
- On successful broadcast, the transaction becomes pending

**Handling broadcast failures**

Sequence mismatch:

Sequence mismatches during broadcast are treated as coordination signals not terminal errors. We should wait for the confirmation loop to recover.

- Further submissions must be paused
- The transaction remains queued
- Broadcasting resumes after recovery completes
- Sequence mismatches are treated as temporary coordination signals, not terminal errors **UNLESS**
  - Expected sequence is higher than expected which suggests that a user submitted a tx for the signer outside of tx client.
  - Parse the expected sequence and bubble it up as an error to the user indicating that node needs to be restarted.

Other failures:

- The transaction must be removed from the queue
- An error must be returned to the user

**Transaction statuses**

In order to confirm transactions, it is required to submit **batch queries** for the status of a bounded number of pending transactions using the `tx_status_batch` RPC method. This returns all transaction statuses in a single call, allowing the client to easily determine which transactions need to be resubmitted and in what order, without dealing with partial or stale data.

**Pending**: No action required.

**Committed**:

- The transaction was included in a block
- User receives a `TxResponse` with height, hash, code, gas usage, and signers
- Transaction is removed from the queue

**Committed with execution error**:

- Transaction is removed from the queue
- User receives an error with the execution code and log

**Evicted**:

- Queue enters recovery mode to block new broadcasts
- Transaction must be resubmitted using the original signed bytes (never resigned)
- Transaction remains pending after resubmission
- Recovery mode must be exited after resubmission
- Broadcasting resumes

**Rejected**:

- Queue must enter recovery mode
- Sequence number must be rolled back to the rejected transaction's sequence
- If multiple consecutive rejections occur, rollback must occur only once to the first rejected sequence
- Transaction is removed from the queue
- User receives a rejection error with code and log
- After sequence rollback, recovery mode exits and broadcasting resumes

**Unknown**

Tx is neither evicted nor rejected or confirmed. Return error to the user that tx is unknown.

### Backward compatibility

Tx Client v2 wrapps Tx Client v1 maintaining full API compatibility.

## Assumptions and Considerations

The client assumes the network isn’t heavily congested and that nodes don’t often bump their local minimum fees. That said, there’s an edge case where a transaction can pass CheckTx, get evicted from the mempool, and then fail on resubmission if the node has since increased its local fee.

## Message Structure/Communication Format

TBD

## Implementation

Not yet implemented.
