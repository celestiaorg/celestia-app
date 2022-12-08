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

- [`NewPayForBlobEvent`](https://github.com/celestiaorg/celestia-app/pull/213/files#diff-1ce55bda42cf160deca2e5ea1f4382b65f3b689c7e00c88085d7ce219e77303dR17-R21) is emitted with the signer's address and size of the blob that is paid for.

## Parameters

Key | Type | Example
--- | --- | ---
MinSquareSize | uint32 | 1
MaxSquareSize | uint32 | 128

### Usage

```shell
celestia-app tx blob payForBlob <hex encoded namespace> <hex encoded data> [flags]
```

### Programmatic Usage

There are tools to programmatically create, sign, and broadcast `BlobTx`s

```go
blob := []byte{1}
namespace := []byte{1, 2, 3, 4, 5, 6, 7, 8}

// create the raw PayForBlob transaction
pfbMsg, err := apptypes.NewPayForBlob(address, namespace, blob)
if err != nil {
return err
}

// we create a `KeyringSigner` to sign messages programmatically
keyringSigner, err := NewKeyringSigner(keyring, "keyring account name", "chain-id-1")
if err != nil {
return err
}

// query for account information necessary to sign a valid tx
err = keyringSigner.QueryAccount(ctx, grpcClientConn)
if err != nil {
return err
}

// generate the signatures for each `MsgPayForBlob` using the `KeyringSigner`,
// then set the gas limit for the tx
gasLimOption := types.SetGasLimit(200000)

// Build and sign the `MsgPayForBlob` tx
signedTx, err := keyringSigner.BuildSignedTx(
signer.NewTxBuilder(gasLimOption),
pfbMsg,
)
if err != nil {
return err
}

rawTx, err := signer.EncodeTx(signedTx)
if err != nil {
return nil, err
}

blobTx, err := coretypes.MarshalBlobTx(rawTx, wblob)
if err != nil {
return nil, err
}

txResp, err := types.BroadcastTx(ctx, conn, sdk_tx.BroadcastMode_BROADCAST_MODE_BLOCK, blobTx)
if err != nil {
return nil, err
}
```

<!-- markdownlint-enable MD010 -->

### How is the `MessageShareCommitment` generated?

1. Split the blob into shares of size `appconsts.ShareSize`
1. Determine the `msgMinSquareSize` (the minimum square size the blob can fit into). This is done by taking the number of shares from the previous step and rounding up to the next perfect square that is a power of two.
1. Arrange the shares into a Merkle mountain range where each tree in the mountain range has a maximum size of the `msgMinSquareSize`.
1. Take the roots of the trees in the Merkle mountain range and create a new Merkle tree.
1. The share commitment is the Merkle root of the Merkle tree from the previous step.
