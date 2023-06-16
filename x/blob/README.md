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

After the `BlobTx` is submitted to the network, a block producer separates the transaction from the blob. Both components get included in the data square in different namespaces: the BlobTx gets included in the PayForBlobNamespace and the associated blob gets included in the namespace the user specified in the original `BlobTx`. Further reading: [Data Square Layout](../../specs/src/specs/data_square_layout.md)

After a block has been created, the user can verify that their data was included in a block via a blob inclusion proof. A blob inclusion proof uses the `ShareCommitment` in the original transaction and subtree roots of the block's data square to prove to the user that the shares that compose their original data do in fact exist in a particular block.

## State

The blob module doesn't maintain it's own state.

When a `MsgPayForBlob` is processed, it consumes gas based on the blob size.

## Messages

- [`MsgPayForBlob`](https://github.com/celestiaorg/celestia-app/blob/8b9c4c9d13fe0ccb6ea936cc26dee3f52b6f6129/proto/blob/tx.proto#L39-L44) pays for the blob to be included in the block.

## PrepareProposal

When a block producer is preparing a block, they must perform an extra step for `BlobTx`s so that end-users can find the blob shares relevant to their submitted `BlobTx`. In particular, block proposers wrap the `BlobTx` in the PayForBlobs namespace with the index of the first share of the blob in the data square. See [Blob share commitment rules](../../specs/src/specs/data_square_layout.md#blob-share-commitment-rules) for more details.

Since `BlobTx`s can contain multiple blobs, the `BlobTx` is wrapped with one share index per blob in the transaction. The index wrapped transaction is called an [IndexWrapper](https://github.com/celestiaorg/celestia-core/blob/2d2a65f59eabf1993804168414b86d758f30c383/proto/tendermint/types/types.proto#L192-L198) and this is the type that gets marshalled and written to the PayForBlobNamespace.

## Events

The blob module emits the following events:

### Blob Events

#### EventPayForBlob

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

### How is the `ShareCommitment` of a blob generated?

See [`CreateCommitment`](https://github.com/celestiaorg/celestia-app/blob/ead76d2bb607ac8a2deaba552de86d4df74a116b/x/blob/types/payforblob.go#L133).

1. Split the blob into shares of size `appconsts.ShareSize`
1. Determine the `BlobMinSquareSize` (the minimum square size the blob can fit into). This is done by taking the number of shares from the previous step and rounding up to the next perfect square that is a power of two.
1. Arrange the shares into a Merkle mountain range where each tree in the mountain range has a maximum size of the `BlobMinSquareSize`. Note: each share is prefixed with a duplicate copy of the namespace so that the roots in the next step match up with the subtree roots of the NMT of the data square.
1. Take the roots of the trees in the Merkle mountain range and create a new Merkle tree.
1. The share commitment of this blob is the Merkle root of the Merkle tree from the previous step.
