# ADR 010: Remove `WireMsgPayForBlob`

## Changelog

- 2022-11-14: Initial draft

## Context

### Cosmos SDK transactions vs messages

For the remainder of this document, sdk.Tx refers to a Cosmos SDK transaction and sdk.Msg refers to a Cosmos SDK message.

- A single sdk.Tx may contain one or many sdk.Msg
- A sdk.Tx's `ValidateBasic()` is distinct from a sdk.Msg's `ValidateBasic()`

### `WireMsgPayForBlob` vs `MsgPayForBlob`

Historically, `WireMsgPayForBlob` was needed so that a user could create, sign, and send multiple signatures per message share commitment (one per square size). The user would submit their `WireMsgPayForBlob` as the sole message in a transaction to the mempool where a block proposer could pick it, malleate it into a `MsgPayForBlob` that included the appropriate signature for the block being constructed and include it in a block.

With the introduction of [ADR 008: square size independent message commitments](./adr-008-square-size-independent-message-commitments.md), a user no longer needs to create, sign, and send multiple signatures. This enables us to reduce the complexity of the malleation process by removing `WireMsgPayForBlob` entirely. Instead, users will create and publish a `BlobTx` to the mempool. The `BlobTx` will include a `sdk.Tx` which will remain unmodified and end up on-chain.

// TODO: diagram the transaction flow for a BlobTx from user -> block proposer -> tx in a block

### `MalleatedTx`'s OriginalTxHash

celestia-core contains a patch to replace the tx hash of a tx containing a `MsgPayForBlob` with the tx hash from the original `WireMsgPayForBlob` it was derived from. This change was needed because the transaction the user creates (one containing a single `WireMsgPayForBlob`) will always be different from the transaction that is included in a block (one containing a single `MsgPayForBlob`). Since the transactions are different, the hashes will also be different. This means Tendermint's default transaction indexing can't confirm to a user that their transaction was included in a block.

See [celestia-core#607](https://github.com/celestiaorg/celestia-core/pull/607) and Tendermint docs on [indexing transactions](https://docs.tendermint.com/v0.34/app-dev/indexing-transactions.html).

## Alternative Approaches

Preserve existing `WireMsgPayForBlob` and `MsgPayForBlob`.

## Decision

Proposed

## Detailed Design

### Transaction Flow

This transaction flow only describes the support for one tx with one message with one blob.

Assume a user wants to publish the data "hello world" to the namespace: `11111111`.

1. User creates a `MsgPayForBlob`

    ```golang
    type MsgPayForBlob struct {
        Signer string
        NamespaceID []byte // 11111111
        BlobSize uint64 // len("hello world")
        ShareCommitment []byte // e945c0bd85c106990...
        ShareVersion uint32 // 0
    }
    ```

2. The user takes this `MsgPayForBlob` and includes it as the sole message in a transaction (henceforth known as `MsgPayForBlobTx`).
3. The user signs the `MsgPayForBlobTx`.
4. The user marshals the `MsgPayForBlobTx` to bytes and includes it as a field in a new transaction. The new transaction is a `BlobTx` which includes an additional field for the data they wish to publish.

    ```golang
    type BlobTx struct {
        Tx []byte // marshalled sdk.Tx that includes one MsgPayForBlob
        Blob []byte // []byte("hello world")
    }
    ```

5. The user signs the `BlobTx` and publishes it to a celestia-app consensus full node or validator and eventually lands in the mempool.
6. The `BlobTx` is checked for validity in the Tendermint mempool via `CheckTx`. `CheckTx` needs the ability to unmarshal `BlobTx` and extract the tx hash associated with the `MsgPayForBlobTx` so that it can use this hash for transaction indexing. Note that the `BlobTx` is still sent from Tendermint to celestia-app in `CheckTx` because celestia-app needs access to the Blobs field in order to validate the associated `MsgPayForBlobTx`.
7. Assuming that the `BlobTx` is valid, a block proposer will pick it up, unwrap the BlobTx into its component parts, write the blob to blob shares in the block's data square, and wrap the MsgPayForBlobTx into a new `BlockTx` that includes the share index of the first share of the blob.

    ```golang
    type BlockTx struct {
        Tx bytes // marshalled sdk.Tx that includes one MsgPayForBlob
        ShareIndex uint64
    }
    ```

8. Assuming the block reaches consensus and gets committed, the Tendermint mempool eventually gets notified of the new block and the transactions included in that block in `TxMempool.Update`. At that time, the mempool must unwrap the `BlockTx` that got included on-chain into its component parts. Then it can use the tx hash associated with the `MsgPayForBlobTx` to remove the `BlobTx` from the mempool.

### Implementation

1. In celestia-core, introduce a new Protobuf definition for `BlobTx`

    ```proto
    // BlobTx wraps an encoded sdk.Tx with a second field to contain blobs of
    // data to be published to the Celestia blockchain. The raw bytes of the
    // blobs are not signed over but they are verified using the tx
    // MsgPayForBlobs's share commitments which are signed over.
    message BlobTx {
        bytes tx = 1; // marshalled sdk.Tx of type MsgPayForBlobs
        repeated bytes blobs = 2;
    }
    ```

2. In celestia-core, remove the transaction hash tracking from [`MalleatedTx`](https://github.com/celestiaorg/celestia-core/blob/b7a7c1ab37fde91f9687b5c1c4766119e7b71db5/proto/tendermint/types/types.pb.go#L1468).

    ```diff
    // MalleatedTx wraps a transaction that was derived from a different original
    // transaction. This allows for tendermint to track malleated and original
    // transactions
    type MalleatedTx struct {
    -   OriginalTxHash []byte `protobuf:"bytes,1,opt,name=original_tx_hash,json=originalTxHash,proto3" json:"original_tx_hash,omitempty"`
        Tx             []byte `protobuf:"bytes,2,opt,name=tx,proto3" json:"tx,omitempty"`
        ShareIndex     uint32 `protobuf:"varint,3,opt,name=share_index,json=shareIndex,proto3" json:"share_index,omitempty"`
    }
    ```

    Note: at the same time, consider renaming `MalleatedTx` to `ShareIndexedTx`.

3. Define a new wrapped transaction type in celestia-app called `ProcessedBlobTx`.

    ```go
    // ProcessedBlobTx caches the unmarshalled result of the BlobTx received from Tendermint
    type ProcessedBlobTx struct {
        Tx    sdk.Tx // unmarshalled sdk.Tx of type MsgPayForBlobs from the original BlobTx.tx but remains unmodified and will be included on-chain
        Blobs []coretypes.Blob
        PFBs  []*MsgPayForBlob
    }
    ```

// TODO: describe the `ValidateBasic` for ProcessedBlobTx
// TODO: describe the `ValidateBasic` for BlobTx

## Status

Proposed

## Consequences

### Positive

- Simplifies the malleation process
- By removing `OriginalTxHash` tracking, reduces the code difference between celestiaorg/celestia-core and tendermint/tendermint

### Negative

### Neutral

Consider an incremental approach for this and related changes:

1. Support for one tx with one message with one blob
1. Support for one tx with one message with multiple blobs
1. Support for one tx with multiple message with multiple blobs

## References

- [ADR 080: square size independent message commitments](./adr-008-square-size-independent-message-commitments.md)
