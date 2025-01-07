# ADR 010: Remove `WireMsgPayForBlob`

## Status

Implemented in <https://github.com/celestiaorg/celestia-core/pull/893> and <https://github.com/celestiaorg/celestia-app/pull/1089>

## Changelog

- 2022/11/14: Initial draft
- 2023/3/24: Update types
- 2023/5/30: Update status

## Context

### Cosmos SDK transactions vs messages

For the remainder of this document, sdk.Tx refers to a Cosmos SDK transaction and sdk.Msg refers to a Cosmos SDK message.

- A single sdk.Tx may contain one or many sdk.Msg
- A sdk.Tx's `ValidateBasic()` is distinct from a sdk.Msg's `ValidateBasic()`

### `WireMsgPayForBlob` vs `MsgPayForBlob`

Historically, `WireMsgPayForBlob` was needed so that a user could create, sign, and send multiple signatures per message share commitment (one per square size). The user would submit their `WireMsgPayForBlob` as the sole message in a transaction to the mempool where a block proposer could pick it, malleate it into a `MsgPayForBlob` that included the appropriate signature for the block being constructed and include it in a block.

With the introduction of [ADR 008: square size independent message commitments](./adr-008-square-size-independent-message-commitments.md), a user no longer needs to create, sign, and send multiple signatures. This enables us to reduce the complexity of the malleation process by removing `WireMsgPayForBlob` entirely. Instead, users will create and publish a `BlobTx` to the mempool. The `BlobTx` will include a `sdk.Tx` which will remain unmodified and end up on-chain.

### `MalleatedTx`'s OriginalTxHash

celestia-core contains a patch to replace the tx hash of a tx containing a `MsgPayForBlob` with the tx hash from the original `WireMsgPayForBlob` it was derived from. This change was needed because the transaction the user creates (one containing a single `WireMsgPayForBlob`) will always be different from the transaction that is included in a block (one containing a single `MsgPayForBlob`). Since the transactions are different, the hashes will also be different. This means Tendermint's default transaction indexing can't confirm to a user that their transaction was included in a block.

See [celestia-core#607](https://github.com/celestiaorg/celestia-core/pull/607) and Tendermint docs on [indexing transactions](https://docs.tendermint.com/v0.34/app-dev/indexing-transactions.html).

## Alternative Approaches

Preserve existing `WireMsgPayForBlob` and `MsgPayForBlob`.

## Detailed Design

### Transaction Flow

This transaction flow only describes the support for one tx with one message with one blob.

Assume a user wants to publish the data "hello world" to the namespace: `11111111`.

1. User creates a `MsgPayForBlobs`

    ```golang
    // MsgPayForBlobs pays for the inclusion of a blob in the block.
    type MsgPayForBlobs struct {
        Signer       string   `protobuf:"bytes,1,opt,name=signer,proto3" json:"signer,omitempty"`
        NamespaceIds [][]byte `protobuf:"bytes,2,rep,name=namespace_ids,json=namespaceIds,proto3" json:"namespace_ids,omitempty"`
        BlobSizes    []uint32 `protobuf:"varint,3,rep,packed,name=blob_sizes,json=blobSizes,proto3" json:"blob_sizes,omitempty"`
        // share_commitments is a list of share commitments (one per blob).
        ShareCommitments [][]byte `protobuf:"bytes,4,rep,name=share_commitments,json=shareCommitments,proto3" json:"share_commitments,omitempty"`
        // share_versions are the versions of the share format that the blobs
        // associated with this message should use when included in a block. The
        // share_versions specified must match the share_versions used to generate the
        // share_commitment in this message.
        ShareVersions []uint32 `protobuf:"varint,8,rep,packed,name=share_versions,json=shareVersions,proto3" json:"share_versions,omitempty"`
    }
    ```

2. The user takes this `MsgPayForBlobs` and includes it as the sole message in a transaction (henceforth known as `MsgPayForBlobTx`).
3. The user signs the `MsgPayForBlobTx`.
4. The user marshals the `MsgPayForBlobTx` to bytes and includes it as a field in a new transaction. The new transaction is a `BlobTx` which includes an additional field for the data they wish to publish.

    ```golang
    // BlobTx wraps an encoded sdk.Tx with a second field to contain blobs of data.
    // The raw bytes of the blobs are not signed over, instead we verify each blob
    // using the relevant MsgPayForBlobs that is signed over in the encoded sdk.Tx.
    type BlobTx struct {
        Tx     []byte  `protobuf:"bytes,1,opt,name=tx,proto3" json:"tx,omitempty"`
        Blobs  []*Blob `protobuf:"bytes,2,rep,name=blobs,proto3" json:"blobs,omitempty"`
        TypeId string  `protobuf:"bytes,3,opt,name=type_id,json=typeId,proto3" json:"type_id,omitempty"`
    }
    ```

5. The user signs the `BlobTx` and publishes it to a celestia-app consensus full node or validator and eventually lands in the mempool.
6. The `BlobTx` is checked for validity in the Tendermint mempool via `CheckTx`. `CheckTx` needs the ability to unmarshal `BlobTx` and extract the tx hash associated with the `MsgPayForBlobTx` so that it can use this hash for transaction indexing. Note that the `BlobTx` is still sent from Tendermint to celestia-app in `CheckTx` because celestia-app needs access to the Blobs field in order to validate the associated `MsgPayForBlobTx`.
7. Assuming that the `BlobTx` is valid, a block proposer will pick it up, unwrap the BlobTx into its component parts, write the blob to blob shares in the block's data square, and wrap the MsgPayForBlobTx into a new `IndexWrapper` that includes the share index of the first share of the blob.
8. Assuming the block reaches consensus and gets committed, the Tendermint mempool eventually gets notified of the new block and the transactions included in that block in `TxMempool.Update`. At that time, the mempool must unwrap the `IndexWrapper` that got included on-chain into its component parts. Then it can use the tx hash associated with the `MsgPayForBlobTx` to remove the `IndexWrapper` from the mempool.

### Implementation

1. In celestia-core, introduce new types for `BlobTx` and `IndexWrapper`. These will be defined in Protobuf but the Golang types are listed below.

    ```golang
    // BlobTx wraps an encoded sdk.Tx with a second field to contain blobs of data.
    // The raw bytes of the blobs are not signed over, instead we verify each blob
    // using the relevant MsgPayForBlobs that is signed over in the encoded sdk.Tx.
    type BlobTx struct {
        Tx     []byte  `protobuf:"bytes,1,opt,name=tx,proto3" json:"tx,omitempty"`
        Blobs  []*Blob `protobuf:"bytes,2,rep,name=blobs,proto3" json:"blobs,omitempty"`
        TypeId string  `protobuf:"bytes,3,opt,name=type_id,json=typeId,proto3" json:"type_id,omitempty"`
    }

    // IndexWrapper adds index metadata to a transaction. This is used to track
    // transactions that pay for blobs, and where the blobs start in the square.
    type IndexWrapper struct {
        Tx           []byte   `protobuf:"bytes,1,opt,name=tx,proto3" json:"tx,omitempty"`
        ShareIndexes []uint32 `protobuf:"varint,2,rep,packed,name=share_indexes,json=shareIndexes,proto3" json:"share_indexes,omitempty"`
        TypeId       string   `protobuf:"bytes,3,opt,name=type_id,json=typeId,proto3" json:"type_id,omitempty"`
    }
    ```

2. Implement a [`ValidateBlobTx`](https://github.com/celestiaorg/celestia-app/blob/74a3e4ba41c8137332ced5682508a89db64e99cb/x/blob/types/blob_tx.go#L37) that:
    1. Checks that the BlobTx contains a `MsgPayForBlobs` and invokes `ValidateBasic` on it
    2. Checks that the number of blobs attached to the BlobTx matches the number of blobs specified in the `MsgPayForBlobs`
    3. Checks that the namespaces of the blobs attached to the BlobTx match the namespaces specified in the `MsgPayForBlobs`
    4. Checks that the share commitments of the blobs attached to the BlobTx match the share commitments specified in the `MsgPayForBlobs`
3. In celestia-core, remove [`MalleatedTx`](https://github.com/celestiaorg/celestia-core/blob/b7a7c1ab37fde91f9687b5c1c4766119e7b71db5/proto/tendermint/types/types.pb.go#L1468).

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

- [ADR 008: square size independent message commitments](./adr-008-square-size-independent-message-commitments.md)
