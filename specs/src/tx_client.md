## Transaction Client

## Abstract

The Transaction Client (TxClient) provides the infrastructure for constructing, signing, broadcasting, and confirming transactions on Celestia chain. It ensures recovery under failure conditions such as sequence mismatches and mempool evictions, scales throughput via worker accounts with fee grants being able to submit multiple txs in parallel, and guarantees that transactions either reach finality on-chain or fail with clear error reporting. It aims to abstract away the complexities of Cosmos sequence management and provide a consistent, reliable transaction submission pipeline.

## Protocol/Component Description

The TxClient handles the complete lifecycle of a transaction through three tightly connected phases: **submission**, **broadcast**, and **confirmation**.

## Submission

User-facing APIs (SubmitTx, SubmitPayForBlob, SubmitPayForBlobWithAccount.)

TODO: Make sure to explain difference between SubmitPayForBlob vs SubmitPayForBlobWithAccount and how they work with parallel/single blob submissions

 wrap user inputs (messages or blobs) into a submission job. Jobs are placed in a transaction queue, optionally processed in parallel by multiple workers. Each worker has a dedicated account, with the primary account funding and fee-granting secondary workers. This design prevents sequence contention and supports higher throughput. (in cosmos sdk single account can not submit multiple transactions in one block and always has to wait for confirmation subsequent)

## Broadcast

The client prepares a transaction by building, signing, and encoding it, and by assigning gas and fees. If gas is not set, it simulates the transaction to estimate usage. If simulation fails due to a sequence mismatch, the client updates the sequence locally with expected and retries. The signed transaction is then broadcast to the network using gRPC.

⚠️ note: this approach assumes that tx client is connected to a trusted consensus node in order to not break replay protection.

Single-endpoint mode retries on mismatch until success or failure.

Multi-endpoint mode broadcasts concurrently to all configured endpoints and accepts the first successful result.

On successful broadcast, the transaction is added to a local tracker(TODO: explain the role of tx tracker) containing (signer, sequence, txBytes, timestamp) to enable rollback or resubmission if required.

## Confirmation

After broadcast, the client continuously polls the chain for transaction status until resolution. Outcomes are:

- Pending: transaction observed but not yet included; continue polling.

- Committed: transaction included in a block. If execution succeeded (code = OK), return success. If failed, return an execution error and remove from tracker.

- Rejected: transaction definitively refused by the node. The client rolls back the account sequence to the rejected transaction’s sequence, removes it from the tracker, and reports error.

- Evicted: transaction dropped from mempool(probably low fee). The client resubmits using locally stored txBytes. If resubmission fails(seq mismatch, etc), an eviction timeout window (1 minute) begins before failure is reported.

Unknown/Not Found: treated as failure unless still tracked locally; in that case, polling continues until timeout.

## Message Structure/Communication Format

The TxClient communicates with Celestia full nodes through existing gRPC services:

Transaction Broadcast

```
Request: BroadcastTxRequest { TxBytes, Mode }
Response: BroadcastTxResponse { TxHash, Code, RawLog }
```

Transaction Status

```
Request: TxStatusRequest { TxId }

Response: TxStatusResponse { Status, Height, ExecutionCode, Error }
```

Gas Estimation

```
Request: EstimateGasPriceAndUsageRequest { TxBytes, TxPriority }

Response: EstimatedGasUsed, EstimatedGasPrice
```

## Assumptions and Considerations

Account Sequences must be consistent with the chain. The client assumes nodes provide reliable expected sequence numbers in error logs.

Eviction Handling requires storing raw txBytes locally for potential resubmission.

Fee Grants allow secondary worker accounts to submit transactions with fees covered by the primary account.

Resilience depends on at least one connected gRPC endpoint being live and connected to consensus.

## Security Considerations

Sequence numbers and balances are queried from full nodes without proof.

Fee grants concentrate fee authority in the primary account.

Gas estimation is based on node simulation and is trusted by the client.

## Implementation

The TxClient implementation can be found in the Celestia Go client libraries:

celestia-app:

Core TxClient code is located in:

user/tx_client.go (transaction submission and queue).

user/parallel_tx_submission.go (parallel worker management).
