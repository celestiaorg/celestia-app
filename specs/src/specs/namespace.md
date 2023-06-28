# Namespace

<!-- toc -->

## Abstract

One of Celestia's core data structures is the namespace. When a user submits a blob to Celestia they MUST associate their blob with exactly one namespace. The namespace enables users to take an interest in a subset of all blobs published to Celestia by allowing user's to query for blobs by namespace.

In order to enable efficient retrieval of blobs by namespace, Celestia makes use of a [Namespaced Merkle Tree](https://github.com/celestiaorg/nmt). See section 5.2 of the [LazyLedger whitepaper](https://arxiv.org/pdf/1905.09274.pdf) for more details.

## Terms

### Version

The namespace version is an 8-bit unsigned integer that indicates the version of the namespace. The version is used to determine the format of the namespace id. The only supported namespace version is `0`. The version is encoded as a single byte.

### ID

The namespace ID is a 28 byte identifier that uniquely identifies a namespace. The ID is encoded as a byte slice of length 28.

## Overview

A namespace is composed of two fields: [version](#version) and [id](#id). The namespace is encoded as a byte slice of length 29.

### Reserved Namespaces

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

## Implementation

See [pkg/namespace](../../../pkg/namespace).

## References

1. [ADR-014](../../../docs/architecture/adr-014-versioned-namespaces.md)
1. [ADR-015](../../../docs/architecture/adr-015-namespace-id-size.md)
