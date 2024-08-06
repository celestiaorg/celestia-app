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

## Assumptions and Considerations

Applications MUST refrain from using the [reserved namespaces](#reserved-namespaces) for their blob data.

Celestia does not ensure the prevention of non-reserved namespace collisions.
Consequently, two distinct applications might use the same namespace.
It is the responsibility of these applications to be cautious and manage the implications and consequences arising from such namespace collisions.
Among the potential consequences is the _Woods Attack_, as elaborated in this forum post: [Woods Attack on Celestia](https://forum.celestia.org/t/woods-attack-on-celestia/59).

## Implementation

See the [namespace implementation in go-square](https://github.com/celestiaorg/go-square/v2/share/be3c2801e902a0f90f694c062b9c4e6a7e01154e/namespace/namespace.go).
For the most recent version, which may not reflect the current specifications, refer to [the latest namespace code](https://github.com/celestiaorg/go-square/blob/main/share/namespace.go).

## Go Definition

```go
type Namespace struct {
	Version uint8
	ID      []byte
}
```

## References

1. [ADR-014](../../../docs/architecture/adr-014-versioned-namespaces.md)
1. [ADR-015](../../../docs/architecture/adr-015-namespace-id-size.md)
1. [Namespaced Merkle Tree](https://github.com/celestiaorg/nmt)
1. [LazyLedger whitepaper](https://arxiv.org/pdf/1905.09274.pdf)
1. [Data Square Layout](./data_square_layout.md)
