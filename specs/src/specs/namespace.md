# Namespace

<!-- toc -->

## Abstract

One of Celestia's core data structures is the namespace.
When a user submits a transaction encapsulating a `MsgPayForBlobs` message to Celestia, they MUST associate each blob with exactly one namespace.
After their transaction has been included in a block, the namespace enables users to take an interest in a subset of the blobs published to Celestia by allowing the user to query for blobs by namespace.

In order to enable efficient retrieval of blobs by namespace, Celestia makes use of a [Namespaced Merkle Tree](https://github.com/celestiaorg/nmt).
See section 5.2 of the [LazyLedger whitepaper](https://arxiv.org/pdf/1905.09274.pdf) for more details.

## Overview

A namespace is composed of two fields: [version](#version) and [id](#id).
A namespace is encoded as a byte slice with the version and id concatenated.

![namespace](./figures/namespace.svg)

### Version

The namespace version is an 8-bit unsigned integer that indicates the version of the namespace.
The version is used to determine the format of the namespace and
is encoded as a single byte.
A new namespace version MUST be introduced if the namespace format changes in a backwards incompatible way.

Below we explain supported user-specifiable namespace versions,
however, we note that Celestia MAY utilize other namespace versions for internal use.
For more details, see the [Reserved Namespaces](#reserved-namespaces) section.

#### Version 0

The only supported user-specifiable namespace version is `0`.
A namespace with version `0` MUST contain an id with a prefix of 18 leading `0` bytes.
The remaining 10 bytes of the id are user-specified.
Below, we provide examples of valid and invalid encoded user-supplied namespaces with version `0`.

```go
// Valid encoded namespaces
0x0000000000000000000000000000000000000001010101010101010101 // valid blob namespace
0x0000000000000000000000000000000000000011111111111111111111 // valid blob namespace

// Invalid encoded namespaces
0x0000000000000000000000000111111111111111111111111111111111 // invalid because it does not have 18 leading 0 bytes
0x1000000000000000000000000000000000000000000000000000000000 // invalid because it does not have version 0
0x1111111111111111111111111111111111111111111111111111111111 // invalid because it does not have version 0
```

Any change in the number of leading `0` bytes in the id of a namespace with version `0` is considered a backwards incompatible change and MUST be introduced as a new namespace version.

### ID

The namespace ID is a 28 byte identifier that uniquely identifies a namespace.
The ID is encoded as a byte slice of length 28.
<!-- It may be useful to indicate the endianness of the encoding) -->

## Reserved Namespaces

Celestia reserves certain namespaces with specific meanings.
Celestia makes use of the reserved namespaces to properly organize and order transactions and blobs inside the [data square](./data_square_layout.md).
Applications MUST NOT use these reserved namespaces for their blob data.

Below is a list of reserved namespaces, along with a brief description of each.
In addition to the items listed in this table, it should be noted that namespaces with values less than `0x00000000000000000000000000000000000000000000000000000000FF` are exclusively reserved for use within the Celestia protocols.
In the table, you will notice that the `PARITY_SHARE_NAMESPACE` and `TAIL_PADDING_NAMESPACE` utilize the namespace version `255`, which differs from the supported user-specified versions.
The reason for employing version `255` for the `PARITY_SHARE_NAMESPACE` is to enable more efficient proof generation within the context of [nmt](https://github.com/celestiaorg/nmt), where it is used in conjunction with the `IgnoreMaxNamespace` feature.
Similarly, the `TAIL_PADDING_NAMESPACE` utilizes the namespace version `255` to ensure that padding shares are always properly ordered and placed at the end of the Celestia data square even if a new namespace version is introduced.
For additional information on the significance and application of the reserved namespaces, please refer to the [Data Square Layout](./data_square_layout.md) specifications.

| name                                | type        | value                                                          | description                                                                                          |
|-------------------------------------|-------------|----------------------------------------------------------------|------------------------------------------------------------------------------------------------------|
| `TRANSACTION_NAMESPACE`             | `Namespace` | `0x0000000000000000000000000000000000000000000000000000000001` | Transactions: requests that modify the state.                                                        |
| `INTERMEDIATE_STATE_ROOT_NAMESPACE` | `Namespace` | `0x0000000000000000000000000000000000000000000000000000000002` | Intermediate state roots, committed after every transaction.                                         |
| `PAY_FOR_BLOB_NAMESPACE`            | `Namespace` | `0x0000000000000000000000000000000000000000000000000000000004` | Namespace reserved for transactions that contain a PayForBlob.                                       |
| `RESERVED_PADDING_NAMESPACE`        | `Namespace` | `0x00000000000000000000000000000000000000000000000000000000FF` | Padding after all reserved namespaces but before blobs.                                              |
| `MAX_RESERVED_NAMESPACE`            | `Namespace` | `0x00000000000000000000000000000000000000000000000000000000FF` | Max reserved namespace is lexicographically the largest namespace that is reserved for protocol use. |
| `TAIL_PADDING_NAMESPACE`            | `Namespace` | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFE` | Tail padding for blobs: padding after all blobs to fill up the original data square.                 |
| `PARITY_SHARE_NAMESPACE`            | `Namespace` | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF` | Parity shares: extended shares in the available data matrix.                                         |

## Assumptions and Considerations

Applications MUST refrain from using the [reserved namespaces](#reserved-namespaces) for their blob data.

## Implementation

See [pkg/namespace](../../../pkg/namespace).

## Protobuf Definition

<!-- TODO: Add protobuf definition for namespace -->

## References

1. [ADR-014](../../../docs/architecture/adr-014-versioned-namespaces.md)
1. [ADR-015](../../../docs/architecture/adr-015-namespace-id-size.md)
1. [Namespaced Merkle Tree](https://github.com/celestiaorg/nmt)
1. [LazyLedger whitepaper](https://arxiv.org/pdf/1905.09274.pdf)
1. [Data Square Layout](./data_square_layout.md)
