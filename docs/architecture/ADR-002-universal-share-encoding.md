# ADR 009: Universal Share Encoding
<!-- disable markdownlint MD010 because Go code snippet uses tabs -->
<!-- markdownlint-disable MD010 -->

## Changelog

- 2022/8/22: inital draft of InfoReservedByte
- 2022/8/24: update draft to Universal Share Encoding
- 2022/8/31: switch from "reserved vs unreserved" to "compact vs sparse" when describing share format

## Terminology

- **nid** (8 bytes): namespace id
- **reserved** (1 byte): is the location of the first transaction, ISR, or evidence in this share if there is one and `0` if there isn't one
- **message length** (varint 1 to 10 bytes): is the length of the entire message in bytes
- **compact share**: a type of share that can accomodate multiple units. Currently, compact shares are used for transactions, ISRs, and evidence to efficiently pack this information into as few shares as possible.
- **sparse share**: a type of share that can accomodate zero or one unit. Currently, sparse shares are used for messages.

## Context

The current compact share format is:<br>`nid (8 bytes) | reserved (1 byte) | share data`

The current spare share format is:

- First share of message:<br>`nid (8 bytes) | message length (varint 1 to 10 bytes) | share data`
- Contiguous share in message:<br>`nid (8 bytes) | share data`

The current share format poses multiple challenges:

1. Clients must have two share parsing implementations (one for compact shares and one for spares shares).
1. It is difficult to make changes to the share format in a backwards compatible way because clients can't determine which version of the share format an individual share conforms to.
1. It is not possible for a client that samples a random share to determine if the share is the first share of that namespace or a contiguous share in the message.

## Proposal

Introduce a universal share encoding that applies to both compact and sparse shares:

- First share of namespace for compact shares or message for sprase shares:<br>`nid (8 bytes) | info (1 byte) | message length (varint 1 to 10 bytes) | data`
- Contiguous share in namespace for compact shares or message for sparse shares:<br>`nid (8 bytes) | info (1 byte) | data`

Shares in the reserved namespace have the added constraint: the first byte of `data` is a reserved byte so the format is:<br>`nid (8 bytes) | info (1 byte) | message length (varint 1 to 10 bytes) | reserved (1 byte) | data`

Where **info** (1 byte) is a byte with the following structure:

- the first 7 bits are reserved for the version information in big endian form (initially, this will be `0000000` for version 0);
- the last bit is a **message start indicator**, that is `1` if the share is at the start of a message or `0` if it is a contiguous share within a message.

Rationale:

1. The first 9 bytes of a share are formatted in a consistent way regardless of the type of share (compact or sparse). Clients can therefore parse shares into data via one mechanism rather than two.
1. The message start indicator allows clients to parse a whole message in the middle of a namespace, without needing to read the whole namespace.
1. The version bits allow us to upgrade the share format in the future, if we need to do so in a way that different share formats can be mixed within a block.

## Questions

1. Does the info byte introduce any new attack vectors?
1. Should one bit in the info byte be used to signify that a continuation share is expected after this share?
    - This **continuation share indicator** is inspired by [protocol buffer varints](https://developers.google.com/protocol-buffers/docs/encoding#varints) and [UTF-8](https://en.wikipedia.org/wiki/UTF-8).
    - The **continuation share indicator** is distinct from the **message start indicator**. Consider a message with 3 contiguous shares:

        indicator          | share 1 | share 2 | share 3
        ---                | ---     | ---     | ---
        message start      | `1`     | `0`     | `0`
        continuation share | `1`     | `1`     | `0` <- client stops requesting contiguous shares when they encounter `0`

    - This would enable clients to begin parsing a message by sampling a share in the middle of a message and proceed to parsing contiguous shares until the end without ever encountering the first share of the message which contains the message length. However, this use case seems contrived because a subset of the message shares may not be meaningful to the client.
    - Without the continuation share indicator, the client would have to request the first share of the message to parse the message length. If they don't request the first share, they can request contiguous shares until they reach the first share after their message ends to learn that they completed requesting the previous message.

1. What happens if a block producer publishes a message with a version that isn't in the list of supported versions?
    - This can be considered invalid via a `ProcessProposal` validity check. Validators already compute the shares in `ProcessProposal` [here](https://github.com/rootulp/celestia-app/blob/ad050e28678119adae02536db3ef5ce083ea1436/app/process_proposal.go#L104-L110) so we can add a check to verify that every share has a known valid version.
1. What happens if a block producer publishes a message where the message start indicator isn't set correctly?
    - Add a check similar to the one above.

## Alternative Approaches

We briefly considered adding the info byte to only sparse shares, see <https://github.com/celestiaorg/celestia-app/pull/651>. This approach was a miscommunication for an earlier proposal and was deprecated in favor of this ADR.

## Decision

// TODO

## Implementation Details

A share version is not set by a user who submits a `PayForData`. Instead, it is set by the block producer when data is split into shares.

### Constants

1. Define a new constant for `InfoReservedBytes = 1`.
1. Update [`MsgShareSize`](https://github.com/celestiaorg/celestia-core/blob/v0.34.x-celestia/pkg/consts/consts.go#L26) to account for one less byte available
1. Update [`TxShareSize`](https://github.com/celestiaorg/celestia-core/blob/v0.34.x-celestia/pkg/consts/consts.go#L24) to account for one less byte available

**NOTE**: Currently constants are defined in celestia-core's [consts.go](https://github.com/celestiaorg/celestia-core/blob/master/pkg/consts/consts.go) but some will be moved to celestia-app's [appconsts.go](https://github.com/celestiaorg/celestia-app/tree/evan/non-interactive-defaults-feature/pkg/appconsts). See [celestia-core#841](https://github.com/celestiaorg/celestia-core/issues/841).

### Types

1. Introduce a new type `InfoReservedByte` to encapsulate the logic around getting the `Version()` or `IsMessageStart()` from a share.

### Logic

#### celestia-core

1. Account for the new `InfoReservedByte` in `./types/share_splitting.go` and `./types/share_merging.go`.
    - **NOTE**: These files are subject to be deleted soon. See <https://github.com/celestiaorg/celestia-core/issues/842>

#### celestia-app

1. Account for the new `InfoReservedByte` in all share splitting and merging code.

## Status

Proposed

## Consequences

### Positive

This proposal resolves challenges posed above.

### Negative

This proposal reduces the number of bytes a share can use for data by one byte.

### Neutral

If 127 versions is larger than required, the share format can be updated (in a subsequent version) to reserve fewer bits for the version in order to use some bits for other purposes.

If 127 versions is smaller than required, the share format can be updated (in a subsequent version) to occupy multiple bytes for the version. For example if the 7 bits are `1111111` then read an additional byte.

## References

- <https://github.com/celestiaorg/celestia-core/issues/839>
- <https://github.com/celestiaorg/celestia-core/issues/759>
- <https://github.com/celestiaorg/celestia-core/issues/757>
- <https://github.com/celestiaorg/celestia-app/issues/659>
