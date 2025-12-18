# Tx Client v2 – Sequential Submission Queue

### Context & Motivation

Tx Client v2 enforces a per-account sequential submission queue that serializes signing, submission, and recovery, avoiding sequence races and minimizing transaction failures.

## Protocol/Component Description

### Per account sequential queue

Every account gets its own **transaction queue**.

This queue is in charge of everything for that account: taking new transactions(jobs), broadcasting them in submission order, reporting outcomes.

Inside the queue we keep two kinds of transactions:

- **Queued** - submitted but not broadcast yet
- **Pending** - already broadcast and waiting for confirmation

---

### Broadcast and Confirmation loops

Each lane runs two background loops:

1. **A submitter**
    - Picks the next queued tx, signs it, broadcasts it to the node.
    - Listens for evicted transactions reported by the monitor and resubmits them.
2. **A monitor**
    Checks the statuses of pending txs and knows how to manage evictions/rejections/confirmations.

**note** The number of submission and monitor workers should be bounded to a fixed number since the number or signers can potentially scale.

---

### Submission flow

1. Accepting a transaction(job)

When a user submits a transaction (e.g. via `SubmitPayForBlobToQueue`):

- Tx is wrapped in a job containing:
  - transaction data
  - user options
  - context
  - a result channel
- The queue enforces memory based backpressure:
  - If sufficient memory is available, the job is appended to the queue
  - If queue exceeds its memory limit, submission blocks until memory is freed or a timeout is reached. If the timeout expires, an error is returned to the user.
  - The client allows users to configure the queue size limit during initialization.
- Nothing gets signed yet.
- No network operations are performed.
- User will receive tx result asynchronously via the result channel.

1. Selecting the next transaction to broadcast

The submitter processes transactions strictly in submission order:

- Submission proceeds only if the queue is not in recovery mode.
- The next unbroadcast transaction is selected.
- The transaction is built and signed using the next sequence number.
- The transaction is broadcast to the node.

If broadcast succeeds:

- Tx hash, sequence, and signed bytes are recorded directly on the queued tx.
- Tx is now considered pending.

If broadcast fails due to a sequence mismatch:

- Submitter pauses further submissions.
- Job remains in the queue.
- Waits for confirmation/recovery logic to unblock submissions.

Sequence mismatches during broadcast are treated as coordination signals not terminal errors. If client remains blocked for longer than a minute, then it's possible to parse expected sequence from the error message and resign as recovery mech.

If broadcast fails for any other reason:

- Tx is removed from the queue.
- An error is returned to the user via the result channel.

### Phase 2: Confirmation

Confirmation runs asynchronously at a configurable polling interval.

The monitor periodically **batch queries** the status of a bounded number of pending transactions using the `tx_status_batch` RPC method. This returns all transaction statuses in one call, so the client can easily determine which transactions need to be resubmitted and in what order, without dealing with partial or stale data.

There are four distinct statuses:

1. Pending

- No action needed
- Move to the next

1. Committed

- The tx was included in a block.
- The user receives a `TxResponse` populated with execution and metadata returned by the chain (e.g. height, hash, execution code, gas usage, and signers).
- The tx is removed from the queue.

If execution failed:

- The tx is removed from the queue.
- The user receives a execution error containing the error code and log.

1. Evicted

- Queue enters recovery mode, preventing new transactions from being broadcast.
- Evicted tx is marked as being resubmitted to avoid duplicate attempts.
- Tx is sent to an internal resubmission channel, which is handled by the queue's coordinator that also handles submissions.

Resubmission behavior:

- Tx is re-broadcast using the same signed tx bytes.
- Tx is **never** resigned.
- Resubmitted tx remains as pending in queue.
- Recovery mode is exited.
- Broadcasting unblocks.

1. Rejected

Sequence recovery:
A rejected transaction means the node will be expecting a lower sequence for this account. To unblock submissions the client must update its local sequence to match the node.

- Queue enters recovery mode to prevent further submissions.
- The sequence number is rolled back to the sequence used by the rejected transaction **only if the tx before was not also rejected.**
- If multiple transactions are rejected consecutively, the sequence is rolled back exactly once, to the sequence of the first rejected transaction.
- Tx is removed from the queue.
- User receives a rejection error containing the execution code and error log.

After recovery:

- Queue exits recovery mode.
- Broadcasting may resume.

1. Unknown/Not Found

Tx is neither evicted nor rejected or confirmed. Return error to the user that tx is unknown.

### Backward compatibility

Tx Client v2 wrapps Tx Client v1 maintaining full API compatibility.

## Assumptions and Considerations

The client assumes the network isn’t heavily congested and that nodes don’t often bump their local minimum fees. That said, there’s an edge case where a transaction can pass CheckTx, get evicted from the mempool, and then fail on resubmission if the node has since increased its local fee.

## Message Structure/Communication Format

TBD

## Implementation

Not yet implemented.
