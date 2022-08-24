# ADR 009: Universal Share Encoding
<!-- disable markdownlint MD010 because Go code snippet uses tabs -->
<!-- markdownlint-disable MD010 -->

## Changelog

- 2022/9/22: inital draft of InfoReservedByte
- 2022/9/24: update draft to Universal Share Encoding

## Context

The current contiguous (transaction, ISRs, evidence) share format is:

- First share of namespace: `nid (8 bytes) | reserved byte | share data`
- Contiguous share in namespace: `nid (8 bytes) | share data`

The current non-contigous (message) share format is:

- First share of message: `nid (8 bytes) | message length (varint) | share data`
- Contiguous share in message: `nid (8 bytes) | share data`

The current share format poses multiple challenges:

1. Clients must have two share parsing implementations (one for contiguous shares and one for non-contiguous shares).
1. It is difficult to make changes to the share format in a backwards compatible way because clients can't determine which version of the share format an individual share conforms to.
1. It is not possible for a client that samples a random share to determine if the share is the start of a namespace (for reserved namespaces) / message (for non-reserved namespaces) or a contiguous share for a multi-share namespace / message.

## Proposal

Introduce a universal share encoding that applies to both contiguous and non-contiguous share formats:

- First share of namespace (for reserved namespaces) or message (for non-reserved namespaces): `nid (8 bytes) | info (1 byte)| message length (varint) | data`
- Contiguous shares in namespace / message: `nid (8 bytes) | info (1 byte)| data`

The contiguous share format has the added constraint:

- First share of namespace: the first byte of `data` is a reserved byte so the format is: `nid (8 bytes) | info (1 byte) | message length (varint) | reserved (1 byte) | data`
- Contiguous shares in namespace: no additional constraint

Where info byte is a byte with the following structure:

- the first 7 bits are reserved for the version information in big endian form (initially, this will just be 0000000 until further notice);
- the last bit is a *message start indicator*, that is 1 if the share is at the start of a namespace (for reserved namespaces) / message (for non-reserved namespaces).

Rationale:

1. The first 9 bytes of a share are formatted in a consistent way regardless of the type of share (contiguous or non-contiguous). Clients can therefore parse shares into data via one mechanism rather than two.
1. The message start indicator allows clients to parse a whole message in the middle of a namespace, without needing to read the whole namespace.
1. The version bits allow us to upgrade the share format in the future, if we need to do so in such a way that different share formats can be mixed within a block.

## Questions

1. Does the info byte introduce any new attack vectors?
1. What happens if a block producer publishes a message with a version that isn't in the list of supported versions (initially only `0000000`)?

## Alternative Approaches

// TODO

## Decision

// TODO

## Implementation Details

### Protobuf

1. (Potentially) add `Version` to [`MsgPayForData`](https://github.com/celestiaorg/celestia-app/blob/main/proto/payment/tx.proto#L44)

**NOTE**: Protobuf does not support the byte type (see [Scalar Value Types](https://developers.google.com/protocol-buffers/docs/proto3#scalar)) so a `uint32` will be used for `Version`. Since `Version` is constrained to 2^7 bits (0 to 127 inclusive), a `Version` outside the supported range (i.e. 128) will seriealize / deserialize correctly but be considered invalid by the application. Adding this field increases the size of the message by one byte + protobuf overhead.

### Constants

1. Define a new constant for `InfoReservedBytes = 1`.
1. Update [`MsgShareSize`](https://github.com/celestiaorg/celestia-core/blob/v0.34.x-celestia/pkg/consts/consts.go#L26) to account for one less byte available
1. Update [`TxShareSize`](https://github.com/celestiaorg/celestia-core/blob/v0.34.x-celestia/pkg/consts/consts.go#L24) to account for one less byte available

**NOTE**: Currently constants are defined in celestia-core's [consts.go](https://github.com/celestiaorg/celestia-core/blob/master/pkg/consts/consts.go) but some will be moved to celestia-app's [appconsts.go](https://github.com/celestiaorg/celestia-app/tree/evan/non-interactive-defaults-feature/pkg/appconsts). See [celestia-core#841](https://github.com/celestiaorg/celestia-core/issues/841).

### Types

1. Introduce a new type `InfoReservedByte` to encapsulate the logic around getting the `Version()` or `IsMessageStart()` from a share.

```golang
// InfoReservedByte is a byte with the following structure: the first 7 bits are
// reserved for version information in big endian form (initially `0000000`).
// The last bit is a "message start indicator", that is `1` if the share is at
// the start of a message and `0` otherwise.
type InfoReservedByte byte

func NewInfoReservedByte(version uint8, isMessageStart bool) (InfoReservedByte, error) {
	if version > 127 {
		return 0, fmt.Errorf("version %d must be less than or equal to 127", version)
	}

	prefix := version << 1
	if isMessageStart {
		return InfoReservedByte(prefix + 1), nil
	}
	return InfoReservedByte(prefix), nil
}

// Version returns the version encoded in this InfoReservedByte.
// Version is expected to be between 0 and 127 (inclusive).
func (i InfoReservedByte) Version() uint8 {
	version := uint8(i) >> 1
	return version
}

// IsMessageStart returns whether this share is the start of a message.
func (i InfoReservedByte) IsMessageStart() bool {
	return uint(i)%2 == 1
}
```

### Logic

#### celestia-core

1. Account for the new `InfoReservedByte` in `./types/share_splitting.go` and `./types/share_merging.go`.
    - **NOTE**: These files are subject to be deleted soon. See <https://github.com/celestiaorg/celestia-core/issues/842>

#### celestia-app

1. Account for the new `InfoReservedByte` in all share splitting and merging code. There is an in-progress refactor of the relevant files. See <https://github.com/celestiaorg/celestia-app/pull/637>

## Status

Proposed

## Consequences

### Positive

This proposal resolves challenges posed above.

### Negative

This proposal reduces the number of bytes a message share can use for data by one byte.

### Neutral

If 127 versions is larger than required, the share format spec can be updated (in a subsequent version) to reserve fewer bits for the version in order to use some bits for other purposes.

If 127 versions is smaller than required, the share format spec can be updated (in a subsequent version) to occupy multiple bytes for the version. For example if the 7 bits are `1111111` then read an additional byte.

## References

- <https://github.com/celestiaorg/celestia-core/issues/839>
- <https://github.com/celestiaorg/celestia-core/issues/759>
- <https://github.com/celestiaorg/celestia-core/issues/757>
- <https://github.com/celestiaorg/celestia-app/issues/659>
