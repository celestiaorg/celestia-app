# `x/blob`

## Abstract

The `x/blob` module enables users to pay for arbitrary data to be published to
the Celestia blockchain. Users create a single `BlobTx` that is composed of:

1. Multiple `Blob`s (Binary Large OBjects): the data they wish to publish. A
   single `Blob` is composed of:
    1. `NamespaceId  []byte`: the namespace this blob should be published to.
    1. `Data         []byte`: the data to be published.
    1. `ShareVersion uint32`: the version of the share format used to encode
       this blob into a share.
1. A single [`sdk.Tx`](https://github.com/celestiaorg/cosmos-sdk/blob/v1.15.0-sdk-v0.46.13/docs/architecture/adr-020-protobuf-transaction-encoding.md) which encapsulates a `MsgPayForBlobs` message that is composed of:
    1. `Signer string`: the transaction signer
    1. `NamespaceIds []byte`: the namespaces they wish to publish each blob to.
       The namespaces here must match the namespaces in the `Blob`s.
    1. `ShareCommitments [][]byte`: a share commitment that is the root of a Merkle
       tree where the leaves are share commitments to each blob associated with
       this `BlobTx`.

After the `BlobTx` is submitted to the network, a block producer separates the
transaction i.e., `sdk.Tx` from the blob. Both components get included in the
data square in different namespaces: the `sdk.Tx` of the original `BlobTx`
together with some metadata about the separated blobs get included in the
PayForBlobNamespace (one of the [reserved
namespaces](../../specs/src/specs/namespace.md#reserved-namespaces)) and the
associated blob gets included in the namespace the user specified in the
original `BlobTx`. Further reading: [Data Square
Layout](../../specs/src/specs/data_square_layout.md)

After a block has been created, the user can verify that their data was included
in a block via a blob inclusion proof. A blob inclusion proof uses the
`ShareCommitment` in the original `sdk.Tx` transaction and subtree roots of the
block's data square to prove to the user that the shares that compose their
original data do in fact exist in a particular block.

> TODO: link to blob inclusion (and fraud) proof

## State

The blob module doesn't maintain it's own state outside of two params. Meaning
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

#### `GovMaxSquareSize`

`GovMaxSquareSize` is the maximum size of a data square that is considered valid
by the validator set. This value is superseded by the `MaxSquareSize`, which is
hardcoded and cannot change without hardforking the chain. See
[ADR021](../../docs/architecture/adr-021-restricted-block-size.md) for more
details.

## Messages

- [`MsgPayForBlobs`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/proto/celestia/blob/v1/tx.proto#L16-L31)
  pays for a set of blobs to be included in the block. Blob transactions that contain
  this `sdk.Msg` are also referred to as "PFBs".

```proto
message MsgPayForBlobs {
  string signer = 1;
  repeated bytes namespaces = 2;
  repeated uint32 blob_sizes = 3;
  repeated bytes share_commitments = 4;
  repeated uint32 share_versions = 8;
}
```

`MsgPayForBlobs` pays for the inclusion of blobs in the block and consists of the
following fields:

- signer: bech32 encoded signer address
- namespace: namespace is a byte slice of length 29 where the first byte is the
  namespaceVersion and the subsequent 28 bytes are the namespaceId.
- blob_sizes: sizes of each blob in bytes.
- share_commitments is a list of share commitments (one per blob).
- share_versions are the versions of the share format that the blobs associated
  with this message should use when included in a block. The share_versions
  specified must match the share_versions used to generate the share_commitment
  in this message. See
  [ADR007](../../docs/architecture/adr-007-universal-share-prefix.md) for more
  details on how this effects the share encoding and when it is updated.

Note that while the shares version in each protobuf encoded PFB are uint32s, the
internal represantation of shares versions is always uint8s. This is because
protobuf doesn't support uint8s.

### Generating the `ShareCommitment`

The share commitment is the commitment to share encoded blobs. It can be used
for cheap inclusion checks for some data by light clients. More information and
rational can be found in the [data square layout
specs](../../specs/src/specs/data_square_layout.md).

1. Split the blob into shares of size [`shareSize`](../../specs/src/specs/data_structures.md#consensus-parameters)
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
for an implementation. See [data square
layout](../../specs/src/specs/data_square_layout.md) and
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
    1. The namepsace of each blob must match the respective (same index)
       namespace in the `MsgPayForBlobs` `sdk.Msg` field `namespaces`.
    1. The namespace is lexicographically greater than the [MAX_RESERVED_NAMESPACE](../../specs/src/specs/consensus.md#constants).
    1. The namespace is not the
       [TAIL_PADDING_NAMESPACE](../../specs/src/specs/consensus.md#constants)
       or [PARITY_SHARE_NAMESPACE](../../specs/src/specs/consensus.md#constants).
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
with the index of the first share of the blob in the data square. See [Blob
share commitment
rules](../../specs/src/specs/data_square_layout.md#blob-share-commitment-rules)
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

#### `EventPayForBlob`

| Attribute Key | Attribute Value                               |
|---------------|-----------------------------------------------|
| signer        | {bech32 encoded signer address}               |
| blob_sizes    | {sizes of blobs in bytes}                     |
| namespace_ids | {namespaces the blobs should be published to} |

## Parameters

| Key            | Type   | Default |
|----------------|--------|---------|
| GasPerBlobByte | uint32 | 8       |

### Usage

```shell
celestia-app tx blob PayForBlobs <hex encoded namespace> <hex encoded data> [flags]
```

For submitting PFB transaction via a light client's rpc, see [celestia-node's
documention](https://docs.celestia.org/developers/rpc-tutorial/#submitpayforblob-arguments).

The steps in the
[`SubmitPayForBlobs`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/x/blob/payforblob.go#L15-L54)
function can be reverse engineered to submit blobs programmatically.

<!-- markdownlint-enable MD010 -->
