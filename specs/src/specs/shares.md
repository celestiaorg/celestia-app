# Shares

<!-- toc -->

## Abstract

All available data in a Celestia [block](./data_structures.md#block) is split into fixed-size data chunks known as "shares". Shares are the atomic unit of the Celestia data square. The shares in a Celestia block are eventually [erasure-coded](./data_structures.md#erasure-coding) and committed to in [Namespace Merkle trees](./data_structures.md#namespace-merkle-tree) (also see [NMT spec](https://github.com/celestiaorg/nmt/blob/master/docs/spec/nmt.md)).

## Terms

- **Blob**: User specified data (e.g. a roll-up block) that is associated with exactly one namespace. Blob data are opaque bytes of data that are included in the block but do not impact Celestia's state.
- **Share**: A fixed-size data chunk that is associated with exactly one namespace.
- **Share sequence**: A share sequence is a contiguous set of shares that contain semantically relevant data. A share sequence MUST contain one or more shares. When a [blob](../../../x/blob/README.md) is split into shares, it is written to one share sequence. As a result, all shares in a share sequence are typically parsed together because the original blob data may have been split across share boundaries. All transactions in the [`TRANSACTION_NAMESPACE`](./consensus.md#reserved-namespaces) are contained in one share sequence. All transactions in the [`PAY_FOR_BLOB_NAMESPACE`](./consensus.md#reserved-namespaces) are contained in one share sequence.

## Overview

User submitted [blob](../../../x/blob/README.md) data is split into shares (see [share splitting](#share-splitting)) and arranged in a `k * k` matrix (see [arranging available data into shares](./data_structures.md#arranging-available-data-into-shares)) prior to the erasure coding step. Shares in the `k * k` matrix are ordered by namespace and have a common [share format](#share-format).

[Padding](#padding) shares are added to the `k * k` matrix to ensure:

1. Blob sequences start on an index that conforms to [non-interactive default rules](../rationale/data_square_layout.md#non-interactive-default-rules) (see [namespace padding share](#namespace-padding-share) and [reserved padding share](#reserved-padding-share))
1. The number of shares in the matrix is a perfect square (see [tail padding share](#tail-padding-share))

## Share Format

The share format below is consistent for all blob shares. In other words, the share format below applies to shares with a namespace above [`MAX_RESERVED_NAMESPACE`](./consensus.md#reserved-namespaces) but below [`PARITY_SHARE_NAMESPACE`](./consensus.md#reserved-namespaces):

- The first [`NAMESPACE_SIZE`](./consensus.md#constants) of a share's raw data is the namespace of that share.
- The next [`SHARE_INFO_BYTES`](./consensus.md#constants) bytes are for share information with the following structure:
  - The first 7 bits represent the share version in big endian form (initially, this will be `0000000` for version `0`);
  - The last bit is a sequence start indicator. The indicator is `1` if the share is the first share of a sequence or `0` if the share is a continuation share of a sequence.
- If this is the first share of a sequence the next [`SEQUENCE_BYTES`](./consensus.md#constants) contain a big endian `uint32` that represents the length of the sequence that follows in bytes.
- The remaining [`SHARE_SIZE`](./consensus.md#constants)`-`[`NAMESPACE_SIZE`](./consensus.md#constants)`-`[`SHARE_INFO_BYTES`](./consensus.md#constants)`-`[`SEQUENCE_BYTES`](./consensus.md#constants) bytes (if first share) or [`SHARE_SIZE`](./consensus.md#constants)`-`[`NAMESPACE_SIZE`](./consensus.md#constants)`-`[`SHARE_INFO_BYTES`](./consensus.md#constants) bytes (if continuation share) are blob data. Note that blob data refers to the payload that user's submit in a [BlobTx](../../../x/blob/README.md).
- If there is insufficient blob data to fill the share, the remaining bytes are filled with `0`.

First share in a sequence:

![fig: share start](./figures/share_start.svg)

Continuation share in a sequence:

![fig: share continuation](./figures/share_continuation.svg)

Since blob data that exceeds [`SHARE_SIZE`](./consensus.md#constants)`-`[`NAMESPACE_SIZE`](./consensus.md#constants)`-`[`SHARE_INFO_BYTES`](./consensus.md#constants) `-` [`SEQUENCE_BYTES`](./consensus.md#constants) bytes will span more than one share, developers MAY choose to encode additional metadata in their raw blob data prior to inclusion in a Celestia block.

## Transaction Shares

In order for clients to parse shares in the middle of a sequence without downloading antecedent shares, Celestia encodes additional metadata in the shares associated with reserved namespaces. At the time of writing this only applies to the [`TRANSACTION_NAMESPACE`](./consensus.md#reserved-namespaces) and [`PAY_FOR_BLOB_NAMESPACE`](./consensus.md#reserved-namespaces). This share structure is often referred to as "compact shares" to differentiate from the share structure defined above for blob shares (a.k.a "sparse shares"). In other words, the format below applies to shares with below [`NAMESPACE_ID_MAX_RESERVED`](./consensus.md#reserved-namespaces):

- The first [`NAMESPACE_SIZE`](./consensus.md#constants) of a share's raw data is the namespace of that share.
- The next [`SHARE_INFO_BYTES`](./consensus.md#constants) bytes are for share information with the following structure:
  - The first 7 bits represent the share version in big endian form (initially, this will be `0000000` for version `0`);
  - The last bit is a sequence start indicator. The indicator is `1` if the share is the first share of a sequence or `0` if the share is a continuation share of a sequence.
- If this is the first share of a sequence the next [`SEQUENCE_BYTES`](./consensus.md#constants) contain a big endian `uint32` that represents the length of the sequence that follows in bytes.
- The next [`SHARE_RESERVED_BYTES`](./consensus.md#constants) bytes are the starting byte of the length of the [canonically serialized](./consensus.md#serialization) first request that starts in the share, or `0` if there is none, as an unsigned [varint](https://developers.google.com/protocol-buffers/docs/encoding).
- The remaining [`SHARE_SIZE`](./consensus.md#constants)`-`[`NAMESPACE_SIZE`](./consensus.md#constants)`-`[`SHARE_INFO_BYTES`](./consensus.md#constants) `-` [`SEQUENCE_BYTES`](./consensus.md#constants) `-` [`SHARE_RESERVED_BYTES`](./consensus.md#constants) bytes (if first share) or [`SHARE_SIZE`](./consensus.md#constants)`-`[`NAMESPACE_SIZE`](./consensus.md#constants)`-`[`SHARE_INFO_BYTES`](./consensus.md#constants)`-`[`SHARE_RESERVED_BYTES`](./consensus.md#constants) bytes (if continuation share) are transaction or PayForBlob transaction data. Each transaction or PayForBlob transaction is prefixed with a [varint](https://developers.google.com/protocol-buffers/docs/encoding) of the length of that unit.
- If there is insufficient transaction or PayForBlob transaction data to fill the share, the remaining bytes are filled with `0`.

First share in a sequence:

![fig: transaction share start](./figures/transaction_share_start.svg)

where reserved bytes would be `38` as a binary big endian `uint32` (`[0b00000000, 0b00000000, 0b00000000, 0b00100110]`).

Continuation share in a sequence:

![fig: transaction share continuation](./figures/transaction_share_continuation.svg)

where reserved bytes would be `80` as a binary big endian `uint32` (`[0b00000000, 0b00000000, 0b00000000, 0b01010000]`).

## Padding

Padding shares vary based on namespace but share a common structure:

- The first [`NAMESPACE_SIZE`](./consensus.md#constants) of a share's raw data is the namespace of that share.
- The next [`SHARE_INFO_BYTES`](./consensus.md#constants) bytes are for share information.
  - The first 7 bits represent the share version in big endian form (initially, this will be `0000000` for version `0`);
  - The last bit is a sequence start indicator. The indicator is always `1`.
- The next [`SEQUENCE_BYTES`](./consensus.md#constants) contain a big endian `uint32` of value `0`.
- The remaining [`SHARE_SIZE`](./consensus.md#constants)`-`[`NAMESPACE_SIZE`](./consensus.md#constants)`-`[`SHARE_INFO_BYTES`](./consensus.md#constants)`-`[`SEQUENCE_BYTES`](./consensus.md#constants) bytes are filled with `0`.

### Namespace Padding Share

A namespace padding share uses the namespace of the blob that precedes it in the data square so that the data square can retain the property that all shares are ordered by namespace.
A namespace padding share acts as padding between blobs so that the subsequent blob begins at an index that conforms to the [non-interactive default rules](../rationale/data_square_layout.md#non-interactive-default-rules). Clients MAY ignore the contents of these shares because they don't contain any significant data.

### Reserved Padding Share

Reserved padding shares use the [`RESERVED_PADDING_NAMESPACE`](./consensus.md#constants). Reserved padding shares are placed after the last reserved namespace share in the data square so that the first blob can start at an index that conforms to non-interactive default rules. Clients MAY ignore the contents of these shares because they don't contain any significant data.

### Tail Padding Share

Tail padding shares use the [`TAIL_PADDING_NAMESPACE`](./consensus.md#constants). Tail padding shares are placed after the last blob in the data square so that the number of shares in the data square is a perfect square. Clients MAY ignore the contents of these shares because they don't contain any significant data.

## Parity Share

Parity shares use the [`PARITY_SHARE_NAMESPACE`](./consensus.md#constants). Parity shares are the output of the erasure coding step of the data square construction process. They occupy quadrants Q1, Q2, and Q3 of the extended data square and are used to reconstruct the original data square (Q0). Bytes carry no special meaning.

## Share Splitting

Share splitting is the process of converting a blob into a share sequence. The process is as follows:

1. Create a new share and populate the prefix of the share with the blob's namespace and share version. Set the sequence start indicator to `1`. Write the blob length as the sequence length. Write the blob's data into the share until the share is full.
1. If there is more data to write, create a new share (a.k.a continuation share) and populate the prefix of the share with the blob's namespace and share version. Set the sequence start indicator to `0`. Write the remaining blob data into the share until the share is full.
1. Repeat the previous step until all blob data has been written.
1. If the last share is not full, fill the remainder of the share with `0`.

## Assumptions and Considerations

- Shares are assumed to be 512 byte slices. Parsing shares of a different size WILL result in an error.

## Implementation

See [pkg/shares](../../../pkg/shares).

## References

1. [ADR-012](../../../docs/architecture/adr-012-sequence-length-encoding.md)
1. [ADR-014](../../../docs/architecture/adr-014-versioned-namespaces.md)
1. [ADR-015](../../../docs/architecture/adr-015-namespace-id-size.md)
