# Tx Client v2 – Sequential Submission Queue

## Abstract

The biggest pain point and bottleneck preventing 100% successful submissions is a race condition caused by eviction handling in tandem with our recovery mechanism in broadcast:

1. A transaction **Tx(3)** is **evicted** from the mempool.
2. The **Tx client does not poll** quickly enough to detect the eviction.
3. A new transaction **Tx(4)** is submitted while the node still expects **sequence=3**.
4. Recovery logic **re-signs Tx(3)** and submits it.
5. Other nodes may still have the *original* Tx(3).

    This results in a **race**:

- Either the **evicted original Tx(3)** is included first,
- Or the **new re-signed Tx(3)** is included first.

**Both outcomes caused failures.** This recurring issue with transaction handling has sparked discussions around a fundamental redesign of our approach.

### Context & Motivation

To understand why we need a new design, let's first look at how TxClient v1 currently works. In v1, transactions are:

1. Submitted via explicit `Submit*` methods.
2. Confirmed in parallel by separate confirmation logic.

This is not ideal for various reasons:

- **Users had to coordinate everything themselves:** when to submit, at which cadence, when to confirm, how often to poll. The TxClient should handle that automatically.
- **Handling failures requires knowing what happened before.** For example, you shouldn't resign a tx if the previous one was evicted. Currently there's no previous state tracking.
- **Evictions, rejections, resubmissions weren't handled consistently or deterministically** because no single component owned the account's entire transaction flow causing races.

This prompted us to re-design **Tx Client v2** around a **sequential per account submission queue**, where one component owns:

- nonce/sequence management
- submission cadence
- TxStatus polling(confirmations)
- engine algorithm: when to resubmit, when to resign, etc.

Additionally I propose that we:

Drop mempool enforced TTLs and move TTL responsibility entirely to the client/user. The node no longer expires transactions; instead, callers control TTL via setting it as tx option.

No API breakage: TxClient V2 still wraps the v1 client maintaining full backwards compatibility as well as the parallel submission flow introduced in v1. It solely focuses on introducing new API which should be a sequential tx queue.

## Protocol/Component Description

### Per account sequential queue

The basic idea is that every account gets its own **transaction lane**.

This lane is in charge of everything for that account: taking new transactions, broadcasting them in the right order, and keeping track of what happens to them afterwards.

Inside this lane we keep two kinds of transactions:

- **Queued** - submitted but not broadcast yet
- **Pending** - already broadcast and waiting for a result

We're not committed to exact data structures yet. Maps, lists whatever ends up being cleanest way for this FIFO.

Along the way, it's becoming pretty clear that we'll also need **a separate map from signer + sequence → tx status** (likely its hash and last known status). Even if we poll statuses in batches we still need to know which hash belongs to which sequence.

---

### Internal loops (still flexible)

Each lane will probably run two background loops:

1. **A submitter** - picks the next queued tx, signs it, broadcasts it to the node, and moves it into pending.
    1. can also be triggered by channels receiving jobs (ig this defeats the purpose of the queue though)
2. **A monitor** - checks the status of everything pending and knows how to manage evictions/rejections.

These don't need to be perfect yet; timing and exact behavior can evolve. We just need something simple that continuously submits and knows how to handle different scenarios without stalling the client.

---

### Submission flow

When a new transaction is submitted we will:

- Add blob with signer to the submission queue.
- Once the transaction is in the queue the submitter loop automatically picks it up, builds, signs and broadcasts it, and moves it into the pending set.
- The caller waits until the transaction reaches some final outcome:

    **confirmed**/**rejected**.

Most of the time that's all that happens. But if we hit a **sequence mismatch** during broadcast, we need to slow down and figure out what's going on before we directly resign.

#### Handling sequence mismatches in CheckTx

Sequence mismatches come in two flavours, depending on whether the chain expects a *lower* or *higher* sequence than the one we tried to use.

---

#### **1. The chain expects a lower sequence**

This usually means:

- we thought a previous tx was confirmed when it wasn’t(was probs evicted)

In this case we:

1. Pause signing/broadcasting
2. Let the confirmation loop figure out whether that previous tx was evicted(this should be the only case).
3. Wait for evicted tx to be pending again
4. Try resume signing

---

#### **2. The chain expects a higher sequence**

This usually means we rolled back too far due to multiple rejections. **(this should no longer happen with new confirmation flow)**

If this happens:

- Pause signing
- Check if we're in recovery mode (the confirmation loop gets in recovery mode when transactions get a sequence mismatch in ReCheck)
- Wait for recovery mode to complete
- Try continue signing taking txs from the start of the submission queue

If things get stuck, the escape hatch is to resign the stalled tx with the chain’s expected sequence as last resort.

**Question:** Is it safe to re-use the nonce of the transaction that was rejected but can be valid again at some point

---

### What the pending monitor(confirmation loop) does

The monitoring loop periodically checks on pending txs and sees how the chain responded.

Depending on the status:

- **Still pending** → keep waiting
- **Committed** → great, return success
- **Committed with execution error** → return error
- **Evicted** → put it back in the queue to try again/directly resubmit since it's already signed
- **Rejected:**
  - If previous tx was confirmed, roll back the sequence
  - What this means for subsequent pending txs:

    **Handling rejections (including when later txs are already pending)**

    If a transaction in the middle of the sequence gets rejected (say tx 3), we can't just keep going. Everything after it (4, 5, 6…) is now basically rejected as well. They will all be rejected with sequence mismatch as part of the same set.

    So when a rejection happens, the lane goes into "recovery mode":

    1. **Freeze signing and broadcasting**

        Stop signing and sending anything new until the client recovers.

        Still accept new submissions, but leave them unsigned for now.

    2. **Query all the subsequent txs with a multi tx status check**

        Make sure the whole tail (4, 5, 6…) actually got rejected.

        Remove them from the pending set so the confirmation loop doesn't keep re-triggering rejection logic.

        A `recoveryMode` flag can be used to stop us from processing the same rejection over and over again while we wait.

    3. **Flag all later txs as `needsResign`**

        Since the sequence rolled back, everything after the rejected tx needs to be resigned with the right sequence.

    4. **Only retry txs that were rejected due to sequence mismatch**

        Anything that failed for any other error won't be retried.

        Only the tail that failed *because of it* get resigned and resubmitted.

    Once the whole tail is cleared out, we unfreeze the lane, resign everything in order, and keep going like normal.

## Message Structure/Communication Format

TBD

## Assumptions and Considerations

### TTL & expiration (client/user-driven)

One of the bigger shifts is that we'd be moving away from mempool TTLs.

Instead:

- We will no longer have TTL related evictions but rejections that are final. Expired txs will never be valid again.
- The user gets to decide how long they care about the tx.

### Cleanup

We'll need some kind of cleanup process so we don't store states forever.

This could be based on:

- time,
- block height,
- how many old txs we keep around,
- or something else entirely.

Haven't settled on a strategy yet.

## Implementation

Not yet implemented.

## References

None.
