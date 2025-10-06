# TxClient SubmitPayForBlob Flow

This document describes the transaction submission flow for the TxClient when submitting a PayForBlob transaction.

```mermaid
graph TD
    Start([SubmitPayForBlob]) --> BroadcastPFB[BroadcastPayForBlobWithAccount]
    BroadcastPFB --> CheckAccount{Account Loaded?}
    CheckAccount -->|No| LoadAccount[Load Account from Chain]
    CheckAccount -->|Yes| CreateMsg[Create MsgPayForBlobs]
    LoadAccount --> CreateMsg
    CreateMsg --> EstimateGas[Estimate Gas & Calculate Fee]
    EstimateGas --> SignTx[Sign Transaction]
    SignTx --> RouteDecision{Multiple Connections?}
    RouteDecision -->|No| SingleConn[submitToSingleConnection]
    RouteDecision -->|Yes| MultiConn[submitToMultipleConnections]
    SingleConn --> SendTx1[sendTxToConnection]
    SendTx1 --> BroadcastResp1{Broadcast Response}
    BroadcastResp1 -->|Success| TrackTx1[Track Transaction]
    BroadcastResp1 -->|Sequence Mismatch| ParseSeq1[Parse Expected Sequence]
    BroadcastResp1 -->|Other Error| Error1[Return Error]
    ParseSeq1 --> UpdateSeq1[Update Signer Sequence]
    UpdateSeq1 --> ResignTx1[resignTransactionWithNewSequence]
    ResignTx1 --> RetryBroadcast1[Retry submitToSingleConnection]
    RetryBroadcast1 --> SendTx1
    TrackTx1 --> IncrSeq1[Increment Sequence]
    IncrSeq1 --> ReturnResp1[Return TxResponse]
    MultiConn --> ParallelBroadcast[Broadcast to All Connections in Parallel]
    ParallelBroadcast --> FirstSuccess{First Success?}
    FirstSuccess -->|Yes| TrackTx2[Track Transaction]
    FirstSuccess -->|No| CollectErrors[Collect All Errors]
    CollectErrors --> Error2[Return Joined Errors]
    TrackTx2 --> IncrSeq2[Increment Sequence]
    IncrSeq2 --> ReturnResp2[Return TxResponse]
    ReturnResp1 --> ConfirmTx[ConfirmTx]
    ReturnResp2 --> ConfirmTx
    ConfirmTx --> PollStatus[Poll TxStatus]
    PollStatus --> CheckStatus{Transaction Status?}
    CheckStatus -->|Pending| WaitPoll1[Wait Poll Interval]
    WaitPoll1 --> PollStatus
    CheckStatus -->|Committed| CheckExecCode{Execution Code?}
    CheckExecCode -->|OK| DeleteTracker1[Delete from TxTracker]
    CheckExecCode -->|Error| ExecError[Return ExecutionError]
    DeleteTracker1 --> Success[Return TxResponse]
    ExecError --> DeleteTracker2[Delete from TxTracker]
    DeleteTracker2 --> End1([End with Error])
    CheckStatus -->|Evicted| CheckTracked{In TxTracker?}
    CheckTracked -->|No| ErrorNotTracked[Error: Not Found in TxTracker]
    CheckTracked -->|Yes| CheckTimer{Eviction Timer Running?}
    CheckTimer -->|Yes| CheckTimeout{Timeout Exceeded?}
    CheckTimer -->|No| ResubmitTx[Resubmit Transaction]
    CheckTimeout -->|Yes| TimeoutError[Return Eviction Timeout Error]
    CheckTimeout -->|No| WaitPoll2[Wait Poll Interval]
    WaitPoll2 --> PollStatus
    ResubmitTx --> ResubmitResp{Resubmit Response?}
    ResubmitResp -->|Success| WaitPoll3[Wait Poll Interval]
    ResubmitResp -->|Broadcast Error| StartTimer[Start Eviction Timer]
    ResubmitResp -->|Other Error| Error3[Return Error]
    StartTimer --> WaitPoll4[Wait Poll Interval]
    WaitPoll3 --> PollStatus
    WaitPoll4 --> PollStatus
    CheckStatus -->|Rejected| GetTxInfo{Get Tx from TxTracker?}
    GetTxInfo -->|Not Found| ErrorRejectedNotTracked[Error: Not Found in TxTracker]
    GetTxInfo -->|Found| ResetSeq[Reset Signer Sequence to Rejected Tx]
    ResetSeq --> DeleteTracker3[Delete from TxTracker]
    DeleteTracker3 --> ErrorRejected[Return Rejection Error]
    CheckStatus -->|Unknown| DeleteTracker4[Delete from TxTracker]
    DeleteTracker4 --> ErrorUnknown[Return Not Found Error]
    Success --> End2([End Success])
    Error1 --> End1
    Error2 --> End1
    Error3 --> End1
    ErrorNotTracked --> End1
    TimeoutError --> End1
    ErrorRejected --> End1
    ErrorRejectedNotTracked --> End1
    ErrorUnknown --> End1
    style Start fill:#e1f5e1
    style Success fill:#e1f5e1
    style End2 fill:#e1f5e1
    style End1 fill:#ffe1e1
    style Error1 fill:#ffe1e1
    style Error2 fill:#ffe1e1
    style Error3 fill:#ffe1e1
    style ExecError fill:#ffe1e1
    style ErrorNotTracked fill:#ffe1e1
    style TimeoutError fill:#ffe1e1
    style ErrorRejected fill:#ffe1e1
    style ErrorRejectedNotTracked fill:#ffe1e1
    style ErrorUnknown fill:#ffe1e1
    style ResignTx1 fill:#fff4e1
    style ResubmitTx fill:#fff4e1
```

## Key Flow Components

### Broadcasting Phase
1. **Account Check**: Verifies the account is loaded, loads from chain if needed
2. **Transaction Preparation**: Creates the MsgPayForBlobs, estimates gas, calculates fee, and signs
3. **Routing**: Decides between single or multi-connection submission based on configuration
4. **Sequence Mismatch Handling**: On sequence mismatch during broadcast, parses expected sequence, updates signer, resigns transaction, and retries

### Confirmation Phase
1. **Polling**: Continuously polls the TxStatus endpoint at configured intervals
2. **Status Handling**:
   - **Pending**: Continues polling
   - **Committed**: Returns success if execution code is OK, otherwise returns ExecutionError
   - **Evicted**: Attempts resubmission; if resubmission fails with broadcast error, starts eviction timer (1 minute timeout)
   - **Rejected**: Resets signer sequence to the rejected transaction's sequence and returns error
   - **Unknown**: Returns not found error

### Error Recovery Mechanisms
1. **Sequence Mismatch**: Automatically resigns transaction with corrected sequence and retries broadcast
2. **Eviction**: Resubmits transaction once; if resubmission fails, waits up to 1 minute for transaction to be included before timing out
3. **Rejection**: Resets sequence number to enable resubmission of subsequent transactions

## Sequence Diagram

```mermaid
sequenceDiagram
    participant Client as TxClient
    participant Signer as Signer
    participant Node as Consensus Node

    Note over Client,Node: Broadcast Phase
    Client->>Client: Create MsgPayForBlobs
    Client->>Node: Estimate gas
    Node-->>Client: Gas estimate
    Client->>Signer: Sign transaction
    Signer-->>Client: Signed txBytes

    Client->>Node: BroadcastTx(txBytes)
    alt Success
        Node-->>Client: TxHash
        Client->>Signer: Increment sequence
    else Sequence Mismatch
        Node-->>Client: Expected sequence error
        Client->>Signer: Update sequence
        Client->>Signer: Resign transaction
        Signer-->>Client: New txBytes
        Client->>Node: BroadcastTx(newTxBytes) [RETRY]
        Node-->>Client: TxHash
        Client->>Signer: Increment sequence
    else Other Error
        Node-->>Client: Error
        Client-->>Client: EXIT with error
    end

    Note over Client,Node: Confirmation Phase
    loop Poll every 3s
        Client->>Node: TxStatus(txHash)
        alt Pending
            Node-->>Client: Status=Pending
        else Committed OK
            Node-->>Client: Status=Committed, Code=0
            Client-->>Client: EXIT success
        else Committed Error
            Node-->>Client: Status=Committed, Code!=0
            Client-->>Client: EXIT with execution error
        else Evicted
            Node-->>Client: Status=Evicted
            Client->>Node: BroadcastTx(txBytes) [RETRY]
            alt Retry Success
                Node-->>Client: Success
            else Retry Failed
                Node-->>Client: Error
                Note over Client: Start 1min timeout timer
            end
        else Rejected
            Node-->>Client: Status=Rejected
            Client->>Signer: Reset sequence to rejected tx
            Client-->>Client: EXIT with rejection error
        end
    end
```
