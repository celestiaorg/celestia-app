## Transaction Client

## Abstract

The Transaction Client (TxClient) provides a high-level abstraction for constructing, signing, broadcasting, and confirming transactions on Celestia chain. It handles sequence mismatches, mempool evictions, rejections and provides parallel transaction submission via worker accounts. The client is built around two reliable submission strategies: ordered (sequential) and unordered (parallel) processing.

**Important**: These are the only reliable ways to submit transactions. Other patterns (like multiple transactions from one account without confirmation) will cause sequence mismatches and failures.

### Usage Modes

1. **Ordered Submission:** (Low Throughput, Sequential Processing, Ordering guarantees):
    - **Single Account**: Uses one account for all transactions
    - **Sequential Processing**: One transaction per block, confirmed before next submission
    - **Ordering Guarantees**: Transactions are processed in exact submission order
    - **Use Case**: Applications requiring strict transaction ordering

2. **Unordered Submission:** (High Throughput, Parallel Processing, No Ordering)
    - **Worker Accounts**: Master account creates multiple worker accounts with fee grants
    - **Parallel Processing**: Multiple transactions submitted concurrently across workers
    - **No Ordering Guarantees**: Transactions will most likely not be ordered.
    - **Use Case**: Applications prioritizing throughput over ordering

## Protocol/Component Description

### Signer

The Signer is a core component that handles transaction creation, signing, and encoding. It manages multiple accounts and provides the cryptographic operations needed for transaction submission.

**Key Functionality**:

- **Transaction Building**: Creates transactions from messages and blobs
  - `CreateTx(msgs []sdktypes.Msg, opts ...TxOption) ([]byte, authsigning.Tx, error)`
  - `CreatePayForBlobs(accountName string, blobs []*share.Blob, opts ...TxOption) ([]byte, uint64, error)`
- **Signing**: Signs transactions using account private keys
  - `SignTx(msgs []sdktypes.Msg, opts ...TxOption) (authsigning.Tx, string, uint64, error)`
- **Encoding/Decoding**: Converts transactions to bytes and vice versa
  - `EncodeTx(tx sdktypes.Tx) ([]byte, error)`
  - `DecodeTx(txBytes []byte) (authsigning.Tx, error)`
- **Account Management**: Tracks account sequences and manages multiple signers
  - `IncrementSequence(accountName string) error`
  - `SetSequence(accountName string, seq uint64) error`

**How TxClient Uses Signer**: The TxClient delegates all transaction creation, signing, and encoding operations to the Signer, which handles sequence management, multi-account support, and blob transaction processing.

### Worker Queue / Parallel Submission

The worker queue is critical for unordered submission mode, enabling concurrent transaction processing without sequence contention.

**Key Functionality**:

- **Job Queue**: Holds submission jobs (`SubmissionJob`) containing blobs, options, and response channels
- **Workers**:
  - Worker 0 = primary account
  - Workers >0 = additional accounts created and fee-granted by primary
- **Parallelism**: Allows concurrent submission of multiple transactions without sequence contention
- **Fee Management**: Non-primary workers use fee grants so all transaction fees are paid by the primary account

### Gas Estimator

The gas estimation service is an external dependency that provides accurate gas and fee calculations for transaction submission.

**Key Functionality**:

TxClient calls the gas estimation service during broadcast to ensure transactions have appropriate gas limits and fees, handling sequence mismatches automatically through re-signing with corrected sequences.

**Gas Estimator APIs**:

- `EstimateGasPriceAndUsage(ctx context.Context, msgs []sdktypes.Msg, priority gasestimation.TxPriority, opts ...TxOption) (gasPrice float64, gasUsed uint64, err error)`
- `EstimateGasPrice(ctx context.Context, priority gasestimation.TxPriority) (float64, error)`

#### Confirmation Loop

Handles transaction resolution and recovery logic using tx data from `TxTracker`.

**Key Functionality**: The confirmation loop polls the chain until transactions are resolved as `Committed`, `Rejected`, `Evicted`, or `Unknown`.

#### Error Handling

The TxClient defines specific error types for different failure scenarios:

- **BroadcastTxError**: Returned when transaction fails to be accepted by the mempool

  ```go
  // BroadcastTxError occurs during transaction broadcasting
  type BroadcastTxError struct {
      TxHash   string // Transaction hash
      Code     uint32 // Error code from node
      ErrorLog string // Detailed error message
  }
  ```

- **ExecutionError**: Returned when transaction is included but execution fails

  ```go
  // ExecutionError occurs when transaction execution fails
  type ExecutionError struct {
      TxHash   string // Transaction hash
      Code     uint32 // Execution result code
      ErrorLog string // Execution error details
  }
  ```

#### Transaction Tracker

TxClient maintains a local transaction tracker (`txTracker`) that stores:

```go
type txInfo struct {
    sequence  uint64    // Account sequence at submission time
    signer    string    // Account name that signed the transaction
    timestamp time.Time // Submission timestamp
    txBytes   []byte    // Raw transaction bytes for resubmission
}
```

The transaction tracker is a critical component that enables the TxClient to handle network failures and mempool evictions. After successfully broadcasting a transaction, the client stores essential metadata locally to enable two key recovery mechanisms:

- **Resubmission**: If a transaction gets evicted from the mempool (e.g., due to low fees), the client can resubmit using the stored `txBytes`.
- **Sequence Rollback**: If a transaction is rejected, the client can roll back the account sequence to the exact point before the failed transaction

## Tx Submission API

The submission phase handles transaction creation, job queuing, and worker assignment. The TxClient provides four main submission APIs with different execution paths:

#### SubmitTx

```go
func (client *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Direct processing, no worker queue
- **Input**: Cosmos SDK messages
- **Process**: Transaction building -> Broadcast -> Confirmation

note: should only work with sequential submission

#### SubmitPayForBlob

```go
func (client *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Uses worker queue for parallel processing
- **Input**: Celestia blobs
- **Process**: Job queued -> Worker assignment -> PayForBlob creation -> Broadcast -> Confirmation

note: works with sequential processing with a single worker or parallel processing with multiple.
if you only create a single worker it will be sequenial processing and ordered. if there are multiple workers then it will spin up as many accoounts as the workers and submits in parallel.

#### SubmitPayForBlobWithAccount

```go
func (client *TxClient) SubmitPayForBlobWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Bypasses worker queue, uses specified account directly
- **Input**: Celestia blobs + account name
- **Process**: Direct account usage -> PayForBlob creation -> Broadcast -> Confirmation

note: this bypasses having to spin up workers at all and can be used with a signle account for sequential submissions.

### Key Protocol Components

**Purpose**: Enables resubmission on eviction and sequence rollback on rejection.

#### Worker Queue Management

- **Job Distribution**: Round-robin assignment to available workers
- **Worker 0**: Uses the primary account (default)
- **Additional Workers**: Auto-generated accounts with fee grants from primary account
- **Fee Grant Setup**: Primary account pays fees for worker transactions

#### Gas Estimation and Nonce Management

**Gas Estimation Process**:

1. If gas limit not provided, simulate transaction with minimal fee (1 utia)
2. Call gas estimation service with transaction bytes
3. Handle sequence mismatch errors by updating local sequence and retrying
4. Set calculated gas limit and fee

**Nonce (Sequence) Management**:

- **Local Tracking**: Client maintains local sequence numbers for all accounts
- **Synchronization**: Query account info from chain on first use
- **Conflict Resolution**: Automatic sequence correction on mismatch errors
- **Increment**: Sequence incremented after successful broadcast
- **Rollback**: Sequence rolled back on transaction rejection

## Broadcast

Broadcasting steps are more or less universal for both SDK txs and blob txs.

The client prepares a transaction by building, signing, and encoding it, and by assigning gas and fees. If gas is not set, it simulates the transaction to estimate usage. If simulation fails due to a sequence mismatch, the client updates the sequence locally with expected and retries. The signed transaction is then broadcast to the network using gRPC.

⚠️ note: this approach assumes that tx client is connected to a trusted consensus node in order to not break replay protection.

Single-endpoint mode retries on mismatch until success or failure.

Multi-endpoint mode broadcasts concurrently to all configured endpoints and accepts the first successful result.

On successful broadcast, the transaction is added to a local tracker(TODO: explain the role of tx tracker) containing (signer, sequence, txBytes, timestamp) to enable rollback or resubmission if required.

And we increase the local sequence

## Confirmation

After broadcast, the client continuously polls the chain for transaction status until resolution. Outcomes are:

- Pending: transaction observed but not yet included; continue polling.

- Committed: transaction included in a block. If execution succeeded (code = OK), return success. If failed, return an execution error and remove from tracker.

- Rejected: transaction definitively refused by the node. The client rolls back the account sequence to the rejected transaction’s sequence, removes it from the tracker, and reports error.

- Evicted: transaction dropped from mempool(probably low fee). The client resubmits using locally stored txBytes. If resubmission fails(seq mismatch, etc), an eviction timeout window (1 minute) begins before failure is reported.

Unknown/Not Found: treated as failure unless still tracked locally; in that case, polling continues until timeout.

## Message Structure/Communication Format

The client communicates with Celestia nodes through gRPC services for transaction broadcasting, status checking, and gas estimation. It maintains persistent connections to multiple endpoints for redundancy and supports both synchronous and asynchronous transaction submission patterns.

The TxClient communicates with Celestia full nodes through existing gRPC services:

#### Transaction Broadcast

```go
Request: BroadcastTxRequest { TxBytes, Mode }
Response: BroadcastTxResponse { TxHash, Code, RawLog }
```

#### Transaction Status

```go
Request: TxStatusRequest { TxId }

Response: TxStatusResponse { Status, Height, ExecutionCode, Error }
```

#### Gas Estimation

```go
Request: EstimateGasPriceAndUsageRequest { TxBytes, TxPriority }

Response: EstimatedGasUsed, EstimatedGasPrice
```

## Assumptions and Considerations

Account Sequences must be consistent with the chain. The client assumes nodes provide reliable expected sequence numbers in error logs.

Eviction Handling requires storing raw txBytes locally for potential resubmission.

Fee Grants allow secondary worker accounts to submit transactions with fees covered by the primary account.

Resilience depends on at least one connected gRPC endpoint being live and connected to consensus.

## Implementation

The TxClient implementation can be found in the Celestia Go client libraries:

celestia-app:

Core TxClient code is located in:

user/tx_client.go (transaction submission and queue).

user/parallel_tx_submission.go (parallel worker management).
