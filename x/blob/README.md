# `x/blob`

## Abstract

The `x/blob` module enables users to pay for arbitrary data to be published to
the Celestia blockchain. This module's name is derived from Binary Large Object
(blob).

To use the blob module, users create and submit a `BlobTx` that is composed of:

1. A single [`sdk.Tx`](https://github.com/celestiaorg/cosmos-sdk/blob/v1.15.0-sdk-v0.46.13/docs/architecture/adr-020-protobuf-transaction-encoding.md) which encapsulates a message of type `MsgPayForBlobs`.
1. Multiple `Blob`s: the data they wish to publish.

After the `BlobTx` is submitted to the network, a block producer separates
the `sdk.Tx` from the blob(s). Both components get included in the
[data square](../../specs/src/data_square_layout.md) in different namespaces:

1. The `sdk.Tx` and some metadata about the separated blobs gets included in the `PayForBlobNamespace` (one of the [reserved namespaces](../../specs/src/namespace.md#reserved-namespaces)).
1. The blob(s) get included in the namespace specified by each blob.

After a block has been created, the user can verify that their data was included
in a block via a blob inclusion proof. A blob inclusion proof uses the
`ShareCommitment` in the original `sdk.Tx` transaction and subtree roots of the
block's data square to prove to the user that the shares that compose their
original data do in fact exist in a particular block.

> TODO: link to blob inclusion (and fraud) proof

## State

The blob module doesn't maintain its own state outside of two params. Meaning
that the blob module only uses the params and auth module stores.

### Params

```proto
// Params defines the parameters for the module.
message Params {
  option (gogoproto.goproto_stringer) = false;
  uint32 gas_per_blob_byte = 1
      [ (gogoproto.moretags) = "yaml:\"gas_per_blob_byte\"" ];
  uint64 gov_max_square_size = 2
      [ (gogoproto.moretags) = "yaml:\"gov_max_square_size\"" ];
}
```

#### `GasPerBlobByte`

`GasPerBlobByte` is the amount of gas that is consumed per byte of blob data
when a `MsgPayForBlobs` is processed. Currently, the default value is 8. This
value is set below that of normal transaction gas consumption, which is 10.
`GasPerBlobByte` was a governance-modifiable parameter in v1 and v2. In app v3 and above, it is a versioned parameter, meaning it can only be changed through hard fork upgrades.

#### `GovMaxSquareSize`

`GovMaxSquareSize` is a governance modifiable parameter that is used to
determine the max effective square size. See
[ADR021](../../docs/architecture/adr-021-restricted-block-size.md) for more
details.

## Messages

`MsgPayForBlobs` pays for a set of blobs to be included in the block. Blob transactions that contain this `sdk.Msg` are also referred to as "PFBs".

```proto
// MsgPayForBlobs pays for the inclusion of a blob in the block.
message MsgPayForBlobs {
  // signer is the bech32 encoded signer address
  string signer = 1;
  // namespaces is a list of namespaces that the blobs are associated with. A
  // namespace is a byte slice of length 29 where the first byte is the
  // namespaceVersion and the subsequent 28 bytes are the namespaceId.
  repeated bytes namespaces = 2;
  // blob_sizes is a list of blob sizes (one per blob). Each size is in bytes.
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

> [!NOTE]
> The internal representation of share versions is always `uint8`. Since protobuf doesn't support the `uint8` type, they are encoded and decoded as `uint32`.

### Generating the `ShareCommitment`

The share commitment is the commitment to share encoded blobs. It can be used
for cheap inclusion checks for some data by light clients. More information and
rational can be found in the [data square layout](../../specs/src/data_square_layout.md) specification.

1. Split the blob into shares of size [`shareSize`](../../specs/src/data_structures.md#consensus-parameters)
1. Determine the
   [`SubtreeWidth`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/pkg/shares/non_interactive_defaults.go#L94-L116)
   by dividing the length in shares by the `SubtreeRootThreshold`.
1. Generate each subtree root by diving the blob shares into `SubtreeWidth`
   sized sets, then take the binary [namespaced merkle tree
   (NMT)](https://github.com/celestiaorg/nmt/blob/v0.16.0/docs/spec/nmt.md) root
   of each set of shares.
1. Calculate the final share commitment by taking the merkle root (note: not an
   NMT, just a normal binary merkle root) of the subtree roots from the previous
   step.

See
[`CreateCommitment`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/x/blob/types/payforblob.go#L169-L236)
for an implementation. See [data square layout](../../specs/src/data_square_layout.md) and
[ADR013](../../docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md)
for details on the rational of the square layout.

## Validity Rules

In order for a proposal block to be considered valid, each `BlobTx`, and thus
each PFB, to be included in a block must follow a set of validity rules.

1. Signatures: All blob transactions must have valid signatures. This is
   state-dependent because correct signatures require using the correct sequence
   number(aka nonce).
1. Single SDK.Msg: There must be only a single sdk.Msg encoded in the `sdk.Tx`
   field of the blob transaction `BlobTx`.
1. Namespace Validity: The namespace of each blob in a blob transaction `BlobTx`
   must be valid. This validity is determined by the following sub-rules:
    1. The namespace of each blob must match the respective (same index)
       namespace in the `MsgPayForBlobs` `sdk.Msg` field `namespaces`.
    1. The namespace is not reserved for protocol use.
1. Blob Size: No blob can have a size of 0.
1. Blob Count: There must be one or more blobs included in the transaction.
1. Share Commitment Validity: Each share commitment must be valid.
    1. The size of each of the share commitments must be equal to the digest of
       the hash function used (sha256 so 32 bytes).
    1. The share commitment must be calculated using the steps specified above
       in [Generating the Share
       Commitment](./README.md#generating-the-sharecommitment)
1. Share Versions: The versions of the shares must be supported.
1. Signer Address: The signer address must be a valid Celestia address.
1. Proper Encoding: The blob transactions must be properly encoded.
1. Size Consistency: The sizes included in the PFB field `blob_sizes`, and each
   must match the actual size of the respective (same index) blob in bytes.

## `IndexWrappedTx`

When a block producer is preparing a block, they must perform an extra step for
`BlobTx`s so that end-users can find the blob shares relevant to their submitted
`BlobTx`. In particular, block proposers wrap the `BlobTx` in the PFB namespace
with the index of the first share of the blob in the data square. See [Blob share commitment rules](../../specs/src/data_square_layout.md#blob-share-commitment-rules)
for more details.

Since `BlobTx`s can contain multiple blobs, the `sdk.Tx` portion of the `BlobTx`
is wrapped with one share index per blob in the transaction. The index wrapped
transaction is called an
[IndexWrapper](https://github.com/celestiaorg/celestia-core/blob/2d2a65f59eabf1993804168414b86d758f30c383/proto/tendermint/types/types.proto#L192-L198)
and this is the struct that gets marshalled and written to the
PayForBlobNamespace.

## Events

The blob module emits the following events:

### Blob Events

#### `EventPayForBlobs`

| Attribute Key | Attribute Value                               |
|---------------|-----------------------------------------------|
| signer        | {bech32 encoded signer address}               |
| blob_sizes    | {sizes of blobs in bytes}                     |
| namespaces    | {namespaces the blobs should be published to} |

## Parameters

| Key            | Type   | Default |
|----------------|--------|---------|
| GasPerBlobByte | uint32 | 8       |

### Usage

```shell
celestia-appd tx blob PayForBlobs <hex encoded namespace> <hex encoded data> [flags]
```

For submitting PFB transaction via a light client's rpc, see [celestia-node's
documentation](https://docs.celestia.org/developers/node-tutorial#submitting-data).

The steps in the
[`SubmitPayForBlobs`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/x/blob/payforblob.go#L15-L54)
function can be reverse engineered to submit blobs programmatically.

<!-- markdownlint-enable MD010 -->

## FAQ

Q: Why do the PFB transactions in the response from Comet BFT API endpoints fail to decode to valid transaction hashes?

The response of CometBFT API endpoints (e.g. `/cosmos/base/tendermint/v1beta1/blocks/{block_number}`) will contain a field called `txs` with base64 encoded transactions. In Celestia, transactions may have one of the two possible types of `sdk.Tx` or `BlobTx` (which wraps around a `sdk.Tx`). As such, each transaction should be first decoded and then gets unmarshalled according to its type, as explained below:

1. Base64 decode the transaction
1. Check to see if the transaction is a `BlobTx` by unmarshalling it into a `BlobTx` type.
   1. If it is a `BlobTx`, then unmarshal the `BlobTx`'s `Tx` field into a `sdk.Tx` type.
   1. If it is not a `BlobTx`, then unmarshal the transaction into a `sdk.Tx` type.

See [test/decode_blob_tx_test.go](./test/decode_blob_tx_test.go) for an example of how to do this.
