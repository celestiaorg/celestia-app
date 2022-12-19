# `x/blob`

## Abstract

The `x/blob` module enables users to pay for arbitrary data to be published to the Celestia blockchain. Users create a single `BlobTx` that is composed of:

1. `Blob` (Binary Large OBject): the data they wish to publish
2. `NamespaceId`: the namespace they wish to publish to
3. `ShareCommitment`: a signature and a commitment over their data when encoded into shares
4. `MsgPayForBlobTx`: a sdk.Tx that contains a MsgPayForBlob that pays for the inclusion of the blob.

After the `BlobTx` is submitted to the network, a block producer separates their transaction into a `MsgPayForBlob` which doesn't include their data (a.k.a blob). Both components get included in the data square in different namespaces: the `MsgPayForBlob` gets included in the transaction namespace and the associated blob gets included in the namespace the user specified in the original `BlobTx`. Further reading: [Message Block Layout](https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md)

After a block has been created, the user can verify that their data was included in a block via a blob inclusion proof. A blob inclusion proof uses the `ShareCommitment` in the original `MsgPayForBlob` and subtree roots of the block's data square to prove to the user that the shares that compose their original data do in fact exist in a particular block.

## State

The blob module doesn't maintain it's own state.

When a `MsgPayForBlob` is processed, it consumes gas based on the blob size.

## Messages

- [`MsgPayForBlob`](https://github.com/celestiaorg/celestia-app/blob/8b9c4c9d13fe0ccb6ea936cc26dee3f52b6f6129/proto/blob/tx.proto#L39-L44) pays for the blob to be included in the block.

## PrepareProposal

The malleation process occurs during the PrepareProposal step.

## Events

The blob module emits the following events:

### Blob Events

#### EventPayForBlob

| Attribute Key | Attribute Value                 |
|---------------|---------------------------------|
| signer        | {bech32 encoded signer address} |
| blob_size     | {size in bytes}                 |

## Parameters

| Key           | Type   | Example |
|---------------|--------|---------|
| MinSquareSize | uint32 | 1       |
| MaxSquareSize | uint32 | 128     |

### Usage

```shell
celestia-app tx blob payForBlob <hex encoded namespace> <hex encoded data> [flags]
```

For submitting PFB transaction via a light client's rpc, see [celestia-node's documention](https://docs.celestia.org/developers/node-api/#post-submit_pfd).

While not directly supported, the steps in the [`SubmitPayForBlob`](https://github.com/celestiaorg/celestia-app/blob/a82110a281bf9ee95a9bf9f0492e5d091371ff0b/x/blob/payforblob.go) function can be reverse engineered to submit blobs programmatically.

<!-- markdownlint-enable MD010 -->

### How is the `MessageShareCommitment` generated?

1. Split the blob into shares of size `appconsts.ShareSize`
1. Determine the `msgMinSquareSize` (the minimum square size the blob can fit into). This is done by taking the number of shares from the previous step and rounding up to the next perfect square that is a power of two.
1. Arrange the shares into a Merkle mountain range where each tree in the mountain range has a maximum size of the `msgMinSquareSize`.
1. Take the roots of the trees in the Merkle mountain range and create a new Merkle tree.
1. The share commitment is the Merkle root of the Merkle tree from the previous step.
