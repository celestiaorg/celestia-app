# `x/blob`

## Abstract

The `x/blob` module enables users to pay for arbitrary data to be published to the Celestia blockchain. Users create a single `BlobTx` that is composed of:

1. Multiple `Blob`s (Binary Large OBjects): the data they wish to publish. A single `Blob` is composed of:
    1. `NamespaceId  []byte`: the namespace this blob should be published to.
    1. `Data         []byte`: the data to be published.
    1. `ShareVersion uint32`: the version of the share format used to encode this blob into a share.
1. A single `sdk.Tx` which is composed of:
    1. `Signer string`: the transaction signer
    1. `NamespaceIds []byte`: the namespaces they wish to publish each blob to. The namespaces here must match the namespaces in the `Blob`s.
    1. `ShareCommitment []byte`: a share commitment that is the root of a Merkle tree where the leaves are share commitments to each blob associated with this BlobTx.

After the `BlobTx` is submitted to the network, a block producer separates the transaction from the blob. Both components get included in the data square in different namespaces: the BlobTx gets included in the PayForBlobNamespace (one of the [reserved namespaces](../../specs/src/specs/consensus.md#reserved-namespaces)) and the associated blob gets included in the namespace the user specified in the original `BlobTx`. Further reading: [Message Block Layout](https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md)

After a block has been created, the user can verify that their data was included in a block via a blob inclusion proof. A blob inclusion proof uses the `ShareCommitment` in the original transaction and subtree roots of the block's data square to prove to the user that the shares that compose their original data do in fact exist in a particular block.

## State

The blob module doesn't maintain it's own state outside of a single param.

When a `MsgPayForBlob` is processed, it consumes gas based on the blob size.

## Messages

- [`MsgPayForBlobs`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/proto/celestia/blob/v1/tx.proto#L16-L31)
  pays for a set of blobs to be included in the block.

```proto
// MsgPayForBlobs pays for the inclusion of a blob in the block.
message MsgPayForBlobs {
  string signer = 1;
  // namespaces is a list of namespaces that the blobs are associated with. A
  // namespace is a byte slice of length 33 where the first byte is the
  // namespaceVersion and the subsequent 32 bytes are the namespaceId.
  repeated bytes namespaces = 2;
  repeated uint32 blob_sizes = 3;
  // share_commitments is a list of share commitments (one per blob).
  repeated bytes share_commitments = 4;
  // share_versions are the versions of the share format that the blobs
  // associated with this message should use when included in a block. The
  // share_versions specified must match the share_versions used to generate the
  // share_commitment in this message.
  repeated uint32 share_versions = 8;
}
```

## `PrepareProposal`

When a block producer is preparing a block, they must perform an extra step for `BlobTx`s so that end-users can find the blob shares relevant to their submitted `BlobTx`. In particular, block proposers wrap the `BlobTx` in the PFB namespace with the index of the first share of the blob in the data square. See [Non-interactive Default Rules](https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules) for more details.

Since `BlobTx`s can contain multiple blobs, the `sdk.Tx` portion of the `BlobTx` is wrapped with one share index per blob in the transaction. The index wrapped transaction is called an [IndexWrapper](https://github.com/celestiaorg/celestia-core/blob/2d2a65f59eabf1993804168414b86d758f30c383/proto/tendermint/types/types.proto#L192-L198) and this is the type that gets marshalled and written to the PayForBlobNamespace.

## Events

The blob module emits the following events:

### Blob Events

#### `EventPayForBlob`

| Attribute Key | Attribute Value                               |
|---------------|-----------------------------------------------|
| signer        | {bech32 encoded signer address}               |
| blob_size     | {size in bytes}                               |
| namespace_ids | {namespaces the blobs should be published to} |

## Parameters

| Key            | Type   | Default |
|----------------|--------|---------|
| GasPerBlobByte | uint32 | 8       |

### Usage

```shell
celestia-app tx blob PayForBlobs <hex encoded namespace> <hex encoded data> [flags]
```

For submitting PFB transaction via a light client's rpc, see [celestia-node's documention](https://docs.celestia.org/developers/rpc-tutorial/#submitpayforblob-arguments).

While not directly supported, the steps in the [`SubmitPayForBlob`](https://github.com/celestiaorg/celestia-app/blob/a82110a281bf9ee95a9bf9f0492e5d091371ff0b/x/blob/payforblob.go) function can be reverse engineered to submit blobs programmatically.

<!-- markdownlint-enable MD010 -->

### Generating the `ShareCommitment`

See [`CreateCommitment`](https://github.com/celestiaorg/celestia-app/blob/ead76d2bb607ac8a2deaba552de86d4df74a116b/x/blob/types/payforblob.go#L133), [data square layout](../../specs/src/specs/data_square_layout.md), and [ADR013](../../docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md) for rational on what the share commitment is created this way.

1. Split the blob into shares of size `appconsts.ShareSize`
1. Determine the `SubtreeWidth` by dividing the length in shares by the
   `SubtreeRootThreshold`.
1. Generate each subtree root by diving the blob shares into `SubtreeWidth`
   sized sets, then take the binary [namespaced merkle
   tree (NMT)](https://github.com/celestiaorg/nmt/blob/v0.16.0/docs/spec/nmt.md) root
   of each set of shares.
1. Calculate the final share commitment by taking the merkle root (note: not an
   NMT, just a normal binary merkle root) of the subtree roots from the previous step.
