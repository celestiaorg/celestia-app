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

The submission phase handles transaction creation, job queuing, and worker assignment. The TxClient provides five main submission APIs with different execution paths:

#### SubmitTx

```go
func (client *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Direct processing, no worker queue
- **Input**: Cosmos SDK messages
- **Process**: Transaction building -> Broadcast -> Confirmation
- **Usage**: Should only work with sequential submission

#### SubmitPayForBlob

```go
func (client *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Direct processing with default account, no worker queue
- **Input**: Celestia blobs
- **Process**: PayForBlob creation -> Broadcast -> Confirmation
- **Usage**: Only for sequential submissions using the default account

#### SubmitPayForBlobToQueue

```go
func (client *TxClient) SubmitPayForBlobToQueue(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Uses worker queue for parallel processing
- **Input**: Celestia blobs
- **Process**: Job queued -> Worker assignment -> PayForBlob creation -> Broadcast -> Confirmation
- **Usage**: Works with sequential processing with a single worker or parallel processing with multiple. If you only create a single worker it will be sequential processing and ordered. If there are multiple workers then it will spin up as many accounts as the workers and submits in parallel.

#### SubmitPayForBlobWithAccount

```go
func (client *TxClient) SubmitPayForBlobWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
```

- **Path**: Bypasses worker queue, uses specified account directly
- **Input**: Celestia blobs + account name
- **Process**: Direct account usage -> PayForBlob creation -> Broadcast -> Confirmation
- **Usage**: This bypasses having to spin up workers at all and can be used with a signle account for sequential submissions

#### QueueBlob

```go
func (client *TxClient) QueueBlob(ctx context.Context, resultC chan SubmissionResult, blobs []*share.Blob, opts ...TxOption)
```

- **Path**: Direct job submission to worker queue
- **Input**: Context, result channel, Celestia blobs, and transaction options
- **Process**: Job queued directly -> Worker assignment -> Processing
- **Usage**: This method adds a job into the queue directly, bypassing the normal submission flow

### Submission API Summary

All submission APIs ultimately delegate into the same broadcast and confirmation methods, they differ only in account selection, tx types and whether jobs go through the worker queue.

## Broadcast

Broadcasting steps are universal across SDK transactions and blob transactions.

**Process:**

1. **Build & Sign**: Transaction is built and signed using the Signer
2. **Gas Estimation**:
   - If no gas provided, simulate with minimal fee
   - Call gas estimation service to determine gas usage and price
   - If estimation fails due to a sequence mismatch, update local sequence to expected and retry
3. **Fee Setting**: Fee is computed as `ceil(minGasPrice * gasLimit)` unless explicitly set by the user
4. **Broadcast**: Transaction is encoded and submitted via gRPC:
   - **Single-endpoint mode**: Retries on mismatch until success or failure
   - **Multi-endpoint mode**: Broadcasts concurrently to multiple endpoints and accepts the first successful response
5. **Tracker Entry**: On successful broadcast, a record is added to the transaction tracker containing (signer, sequence, txBytes, timestamp) so the transaction can be resubmitted or rolled back if needed
6. **Sequence Increment**: After a successful broadcast, the local sequence for that account is incremented

⚠️ **Note**: This approach assumes the TxClient is connected to a trusted consensus node in order to maintain replay protection.

## Confirmation

After broadcast, the TxClient continuously polls the chain for transaction status until resolution. This is the tx confrimation life cycle:

- **Pending**: Transaction in the mempool but not yet included -> continue polling
- **Committed**: Transaction included in a block
  - If execution succeeded (code = OK): return success
  - If execution failed: return an execution error and remove from tx tracker
- **Rejected**:
  - Transaction explicitly refused by the node during `ReCheck()`(This happens in the mempool after a block is committed).
  - The client rolls back the sequence to the rejected transaction's sequence
  - Removes the `TxTracker` entry
  - Returns error
- **Evicted**: Transaction dropped from mempool (e.g. low fees).
  - The client resubmits using txBytes stored in the tracker
  - If resubmission fails again (e.g. mismatch), an eviction timeout window (1 minute) begins before reporting failure
- **Unknown/Not Found**: Treated as failure and error gets returned to the user.

Once transactions are concluded they are automatically pruned from the `TxTracker`. Additionally, the tracker periodically prunes entries older than 10 minutes to prevent unbounded growth.

## Message Structure/Communication Format

The client communicates with Celestia full nodes through gRPC services for transaction broadcasting, status checking, and gas estimation.

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

- Trusted Node: The client depends on a trusted consensus node for account state, sequence numbers, and gas estimation (no proof verification).

- Sequence Consistency: Currently we rely on consensus node to provide us with the correct sequence in case of sequence mismatch errors. If we are connected to a malicious node this could break replay protection.

- Eviction Behavior: Evictions are local to a node. A transaction evicted by one node may still be proposed by another, so users must not re-sign the transaction immediately to avoid double-spending.

- Submission Modes: Only sequential (single account) or parallel (worker accounts with fee grants) are safe; other patterns cause sequence mismatches.

- Resilience: At least one gRPC endpoint must remain live and connected for reliable broadcast and confirmation.

## Implementation

The TxClient implementation can be found in the `celestia-app` repo in the `pkg/user` directory:

**Core TxClient code is located in:**

- [`pkg/user/tx_client.go`](https://github.com/celestiaorg/celestia-app/blob/main/pkg/user/tx_client.go) - Transaction submission, broadcasting, and confirmation logic
- [`pkg/user/parallel_tx_submission.go`](https://github.com/celestiaorg/celestia-app/blob/main/pkg/user/parallel_tx_submission.go) - Parallel worker management and job queuing
- [`pkg/user/signer.go`](https://github.com/celestiaorg/celestia-app/blob/main/pkg/user/signer.go) - Transaction signing and account management
- [`pkg/user/account.go`](https://github.com/celestiaorg/celestia-app/blob/main/pkg/user/account.go) - Account state management
- [`pkg/user/tx_options.go`](https://github.com/celestiaorg/celestia-app/blob/main/pkg/user/tx_options.go) - Transaction configuration options
