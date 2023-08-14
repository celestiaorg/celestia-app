# Namespace

<!-- toc -->

## Abstract

One of Celestia's core data structures is the namespace. When a user submits a `MsgPayForBlobs` transaction to Celestia they MUST associate each blob with exactly one namespace. After their transaction has been included in a block, the namespace enables users to take an interest in a subset of the blobs published to Celestia by allowing the user to query for blobs by namespace.

In order to enable efficient retrieval of blobs by namespace, Celestia makes use of a [Namespaced Merkle Tree](https://github.com/celestiaorg/nmt). See section 5.2 of the [LazyLedger whitepaper](https://arxiv.org/pdf/1905.09274.pdf) for more details.

## Overview

A namespace is composed of two fields: [version](#version) and [id](#id). A namespace is encoded as a byte slice with the version and id concatenated. Each [share](./shares.md) is prefixed with exactly one namespace.

![namespace](./figures/namespace.svg)

### Version

The namespace version is an 8-bit unsigned integer that indicates the version of the namespace. The version is used to determine the format of the namespace. The only supported user-specifiable namespace version is `0`. The version is encoded as a single byte.

Note: The `PARITY_SHARE_NAMESPACE` uses the namespace version `255` so that it can be ignored via the `IgnoreMaxNamespace` feature from [nmt](https://github.com/celestiaorg/nmt). The `TAIL_PADDING_NAMESPACE` uses the namespace version `255` so that it remains ordered after all blob namespaces even in the case a new namespace version is introduced.

A namespace with version `0` must contain an id with a prefix of 18 leading `0` bytes. The remaining 10 bytes of the id are user-specified.

```go
// Valid encoded namespaces
0x0000000000000000000000000000000000000000000000000000000001 // transaction namespace
0x0000000000000000000000000000000000000001010101010101010101 // valid blob namespace
0x0000000000000000000000000000000000000011111111111111111111 // valid blob namespace

// Invalid encoded namespaces
0x0000000000000000000000000111111111111111111111111111111111 // invalid because it does not have 18 leading 0 bytes
0x1000000000000000000000000000000000000000000000000000000000 // invalid because it does not have version 0
0x1111111111111111111111111111111111111111111111111111111111 // invalid because it does not have version 0
```

A new namespace version MUST be introduced if the namespace format changes in a backwards incompatible way (i.e. the number of leading `0` bytes in the id prefix is reduced).

### ID

The namespace ID is a 28 byte identifier that uniquely identifies a namespace. The ID is encoded as a byte slice of length 28.

## Reserved Namespaces

<<<<<<< HEAD
| name                                | type        | value                                                          | description                                                                                          |
|-------------------------------------|-------------|----------------------------------------------------------------|------------------------------------------------------------------------------------------------------|
| `TRANSACTION_NAMESPACE`             | `Namespace` | `0x0000000000000000000000000000000000000000000000000000000001` | Transactions: requests that modify the state.                                                        |
| `INTERMEDIATE_STATE_ROOT_NAMESPACE` | `Namespace` | `0x0000000000000000000000000000000000000000000000000000000002` | Intermediate state roots, committed after every transaction.                                         |
| `PAY_FOR_BLOB_NAMESPACE`            | `Namespace` | `0x0000000000000000000000000000000000000000000000000000000004` | Namespace reserved for transactions that contain a PayForBlob.                                       |
| `RESERVED_PADDING_NAMESPACE`        | `Namespace` | `0x00000000000000000000000000000000000000000000000000000000FF` | Padding after all reserved namespaces but before blobs.                                              |
| `MAX_RESERVED_NAMESPACE`            | `Namespace` | `0x00000000000000000000000000000000000000000000000000000000FF` | Max reserved namespace is lexicographically the largest namespace that is reserved for protocol use. |
| `TAIL_PADDING_NAMESPACE`            | `Namespace` | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFE` | Tail padding for blobs: padding after all blobs to fill up the original data square.                 |
| `PARITY_SHARE_NAMESPACE`            | `Namespace` | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF` | Parity shares: extended shares in the available data matrix.                                         |
=======
Celestia reserves some namespaces for protocol use.
These namespaces are called "reserved namespaces".
Reserved namespaces are used to arrange the contents of the [data square](./data_square_layout.md).
Applications MUST NOT use reserved namespaces for their blob data.
Reserved namespaces fall into two categories: _Primary_ and _Secondary_.

- Primary: Namespaces with values less than or equal to `0x00000000000000000000000000000000000000000000000000000000FF`. Primary namespaces always have a version of `0`.
- Secondary: Namespaces with values greater than or equal to `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF00`.
Secondary namespaces always have a version of `255` (`0xFF`) so that they are placed after all user specifiable namespaces in a sorted data square.
The `PARITY_SHARE_NAMESPACE` uses version `255` (`0xFF`) to enable more efficient proof generation within the context of [nmt](https://github.com/celestiaorg/nmt), where it is used in conjunction with the `IgnoreMaxNamespace` feature.
The `TAIL_PADDING_NAMESPACE` uses the version `255` to ensure that padding shares are always placed at the end of the Celestia data square even if a new user-specifiable version is introduced.

Below is a list of the current reserved namespaces.
For additional information on the significance and application of the reserved namespaces, please refer to the [Data Square Layout](./data_square_layout.md) specifications.

| name                                 | type        | category  | value                                                          | description                                                                |
|--------------------------------------|-------------|-----------|----------------------------------------------------------------|----------------------------------------------------------------------------|
| `TRANSACTION_NAMESPACE`              | `Namespace` | Primary   | `0x0000000000000000000000000000000000000000000000000000000001` | Namespace for ordinary Cosmos SDK transactions.                            |
| `INTERMEDIATE_STATE_ROOT_NAMESPACE`  | `Namespace` | Primary   | `0x0000000000000000000000000000000000000000000000000000000002` | Namespace for intermediate state roots (not currently utilized).           |
| `PAY_FOR_BLOB_NAMESPACE`             | `Namespace` | Primary   | `0x0000000000000000000000000000000000000000000000000000000004` | Namespace for transactions that contain a PayForBlob.                      |
| `PRIMARY_RESERVED_PADDING_NAMESPACE` | `Namespace` | Primary   | `0x00000000000000000000000000000000000000000000000000000000FF` | Namespace for padding after all primary reserved namespaces.               |
| `MAX_PRIMARY_RESERVED_NAMESPACE`     | `Namespace` | Primary   | `0x00000000000000000000000000000000000000000000000000000000FF` | Namespace for the highest primary reserved namespace.                      |
| `MIN_SECONDARY_RESERVED_NAMESPACE`   | `Namespace` | Secondary | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF00` | Namespace for the lowest secondary reserved namespace.                     |
| `TAIL_PADDING_NAMESPACE`             | `Namespace` | Secondary | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFE` | Namespace for padding after all blobs to fill up the original data square. |
| `PARITY_SHARE_NAMESPACE`             | `Namespace` | Secondary | `0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF` | Namespace for parity shares.                                               |
>>>>>>> cb8247e (feat!: reserve the last 256 namespaces for protocol use (#2257))

## Assumptions and Considerations

## Implementation

See [pkg/namespace](../../../pkg/namespace).

## References

1. [ADR-014](../../../docs/architecture/adr-014-versioned-namespaces.md)
1. [ADR-015](../../../docs/architecture/adr-015-namespace-id-size.md)
