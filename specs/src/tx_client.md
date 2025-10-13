## Transaction Client

## Abstract

The Transaction Client (TxClient) provides a high-level abstraction for constructing, signing, broadcasting, and confirming transactions on Celestia chain. It handles sequence mismatches, mempool evictions, rejections and provides parallel transaction submission via worker accounts. The client is built around two reliable submission strategies: ordered (sequential) and unordered (parallel) submissions.

## Protocol/Component Description

### Gas Estimator

The gas estimation service is an external dependency that provides accurate gas and fee calculations for transaction submission.

**Key Functionality**:

TxClient calls the gas estimation service during broadcast to ensure transactions have appropriate gas limits and fees, handling sequence mismatches automatically through re-signing with corrected sequences.

**Note**: Gas estimation is only necessary when the user did not set gas limit and gas price in tx options.

**Gas Estimator APIs**:

- `EstimateGasPriceAndUsage(ctx context.Context, msgs []sdktypes.Msg, priority gasestimation.TxPriority, opts ...TxOption) (gasPrice float64, gasUsed uint64, err error)`
- `EstimateGasPrice(ctx context.Context, priority gasestimation.TxPriority) (float64, error)`

## Tx Flow

Below will be described tx flow from submission to broadcasting it on celestia chain to confirmation and how transaction client handles it. It will be split into 3 sub-sections.

## Submission

**Important**: Currently, only the below submission patterns are reliable for submitting transactions. Other patterns like sending multiple transactions from one account without waiting for confirmation will likely cause sequence mismatches and failures.

### Ordered Submission

**Characteristics**: Low Throughput, Sequential Submission, Less error prone

- **Single Account**: All transactions signed by one account
- **Sequential**: Max one tx per block, each tx must be confirmed before subsequent can be resubmitted
- **Ordering**: Transactions will likely be processed in submission order
- **Low Throughput**: One tx per block per account
- **Use Case**:
  - Applications and users requiring all blobs to be signed by a single account and reliably executed.

**APIs**:

- **SubmitTx**: Sequential submission for SDK txs (no worker queue).
  - **Process**: Build transaction → Broadcast → Confirm

    ```go
    func (client *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*TxResponse, error)
    ```

- **SubmitPayForBlob**: Sequential submission with default account (no worker queue).
  - **Process**: Create PFB with default account → Broadcast → Confirm

    ```go
    func (client *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
    ```

- **SubmitPayForBlobWithAccount**: Sequential submission with a specified account (bypasses worker queue).
  - **Process**: Create PFB with specified account → Broadcast → Confirmation

    ```go
    func (client *TxClient) SubmitPayForBlobWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
    ```

### Unordered Submission

**Characteristics**: High Throughput, Parallel submission, No Ordering

- **Worker Accounts**: A pool of accounts is created by the primary account, each fee-granted with unlimited allowances.
- **Worker Queue**: Transactions are queued and dispatched to available workers.
- **Parallel Workers**:
  - Worker 0 = primary account
  - Workers >0 = additional fee-granted accounts
- **High Throughput**: Multiple transactions can be submitted and processed per block concurrently, without sequence contention.
- **No Ordering Guarantees**: Execution order is nondeterministic; workers process transactions as they become available.
- **Fee Grants**: All fees are covered by the primary account, so workers operate without individual balance management.
- **Account Reuse**: Worker accounts are persisted across runtimes when parallel submission is enabled.
- **Use Case**: Best suited for applications that prioritize throughput over ordering.

**Note:** If initialized with only one worker, this mode behaves identically to Sequential Submission (one tx per block).

**APIs**:

- **SubmitPayForBlobToQueue**: Uses worker queue for parallel or sequential (1 worker) submissions.
  - **Process**: Job queued → Worker assignment → PayForBlob creation → Broadcast → Confirmation

    ```go
    func (client *TxClient) SubmitPayForBlobToQueue(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error)
    ```

- **QueueBlob**: Direct job submission to worker queue.
  - **Process**: Job queued directly → Worker assignment → PayForBlob creation → Broadcast → Confirmation

    ```go
    func (client *TxClient) QueueBlob(ctx context.Context, resultC chan SubmissionResult, blobs []*share.Blob, opts ...TxOption)
    ```

All submission APIs ultimately delegate into the same broadcast and confirmation methods, they differ only in account selection, tx types and whether jobs go through the worker queue.

### Broadcast

Broadcasting steps are universal across SDK transactions and blob transactions.

**Process:**

1. **Build & Sign**: Transaction is built and signed using the Signer
2. **Gas Estimation**:
   - If no gas provided, simulate with minimal fee
   - Call gas estimation service to determine gas usage and price
   - If simulation fails due to a sequence mismatch:
        - Update local sequence to expected.
        - Resign and retry gas estimation.
3. **Fee Setting**: Fee is computed as `ceil(minGasPrice * gasLimit)` unless explicitly set by the user
4. **Broadcast**: Transaction is encoded and submitted via gRPC:
    - **Single-endpoint mode**: Send to the default RPC endpoint.
    - **Multi-endpoint mode**: Broadcast to all endpoints, accept the first success and cancel the rest; if all fail, return the first error.
5. **Broadcast Error Handling**: Common to both **single** and **multi-endpoint** modes.
    - Sequence mismatch during CheckTx (mempool validation):
        - Retry with corrected sequence until success or another failure.
        - Update the signer’s local sequence to the expected value returned by the node.
        - **Reasoning**: Without updating to the chain’s expected sequence, the client’s local signer falls behind and stalls. Once stalled, no new transactions can be submitted until the process is manually reset.
    - Other rejections:
       - Tendermint/CometBFT does not throw mempool errors for application rejections; they are set in the transaction response.
       - The client must parse the response code:
            - 0(`abci.CodeTypeOK`) - accepted into mempool.
            - Non-zero - tx was rejected; populate and return `BroadcastTxError`.

                 ```go
                  type BroadcastTxError struct {
                  TxHash   string // Transaction hash
                  Code     uint32 // Error code from node
                  ErrorLog string // Detailed error message
                }
                ```

6. **Transaction Entry**: On success, record (`signer`, `sequence`, `txBytes`).
7. **Sequence Increment**: After successful broadcast, increment the signer’s local sequence by calling **signer.SetSequence()**

### Confirmation

After broadcast, the TxClient continuously polls the chain for transaction status until resolution. This is the tx confirmation life cycle:

- **Pending**: Transaction in the mempool but not yet included, continue polling

- **Committed**: Transaction included in a block.
  - If execution succeeded (response code = `abci.CodeTypeOK`): return success
  - If execution failed (any other response code):
    - Populate `ExecutionError` and return it to the user

    ```go
    type ExecutionError struct {
        TxHash   string // Transaction hash
        Code     uint32 // Execution result code
        ErrorLog string // Execution error details
    }
    ```

- **Rejected**: Transaction was explicitly refused by the node during `ReCheck()` (after each block commit).
  - Retrieve tx sequence from the tracker.
  - Roll back the sequence to the retrieved tx sequence.
  - Construct and return a rejection error with the tx hash and response code. (no specific error type)

- **Evicted**: Transaction dropped from mempool (low fees, mempool full).
  - The client resubmits using txBytes stored in the tracker.
  - If resubmission fails (e.g. sequence mismatch):
    - **Do not resign with a new sequence**. Eviction isn't final, another node might still propose this tx and resigning risks duplicate inclusion.
    - Start a 1-minute eviction timeout window. If the tx is not confirmed within timeout period, construct and return an eviction error. (no specific error type)
  - If resubmission is successful:
    - The same tx is re-added to the node's mempool
    - Continue polling until the updated status is resolved

- **Unknown/Not Found**: Tx is neither evicted nor rejected or confirmed. Return error to the user that tx is unknown.

- Once transactions are concluded (rejected/evicted/confirmed) they should get pruned from the tracker before function returns.

## Message Structure/Communication Format

The client communicates with Celestia full nodes through gRPC services for transaction broadcasting, status checking, and gas estimation.

### Transaction Broadcast

```go
Request: BroadcastTxRequest { TxBytes, Mode }
Response: BroadcastTxResponse { TxHash, Code, RawLog }
```

### Transaction Status

```go
Request: TxStatusRequest { TxId }
Response: TxStatusResponse { Status, Height, ExecutionCode, Error }
```

### Gas Estimation

```go
Request: EstimateGasPriceAndUsageRequest { TxBytes, TxPriority }
Response: EstimatedGasUsed, EstimatedGasPrice
```

## Assumptions and Considerations

- Trusted Node: The client depends on a trusted consensus node for account state, sequence numbers, and gas estimation (no proof verification).

- Eviction Behavior: Evictions are local to a node. A transaction evicted by one node may still be proposed by another, so users must not re-sign the transaction immediately to avoid double-spending.

- Submission Modes: Only sequential (single account) or parallel (worker accounts with fee grants) are safe; other patterns may lead to sequence mismatches.

## Implementation

TxClient implementation can be found in the `celestia-app` repo in the `pkg/user` directory
