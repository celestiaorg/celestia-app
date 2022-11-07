# ADR 007: Universal Share Prefix

## Terminology

- **compact share**: a type of share that can accommodate multiple units. Currently, compact shares are used for transactions, and evidence to efficiently pack this information into as few shares as possible.
- **sparse share**: a type of share that can accommodate zero or one unit. Currently, sparse shares are used for messages.
- **share sequence**: an ordered list of shares

## Context

### Current Compact Share Schema

`namespace_id (8 bytes) | reserved (1 byte) | data`

Where:

- `reserved (1 byte)`: is the location of the first transaction or evidence in the share if there is one and `0` if there isn't one.
- `data`: contains the raw bytes where each unit is prefixed with a varint 1 to 10 bytes that indicates how long the unit is in bytes.

### Current Sparse Share Schema

- First share of message:<br>`namespace_id (8 bytes) | message length (varint 1 to 10 bytes) | data`
- Contiguous share in message:<br>`namespace_id (8 bytes) | data`

Where:

- `message length (varint 1 to 10 bytes)`: is the length of the entire message in bytes

The current share format poses multiple challenges:

1. Clients must have two share parsing implementations (one for compact shares and one for sparse shares).
1. It is difficult to make changes to the share format in a backwards compatible way because clients can't determine which version of the share format an individual share conforms to.
1. It is not possible for a client that samples a random share to determine if the share is the first share of a message or a contiguous share in the message.

## Proposal

Introduce a universal share encoding that applies to both compact and sparse shares:

- First share of sequence:<br>`namespace_id (8 bytes) | info (1 byte) | data length (varint 1 to 10 bytes) | data`
- Contiguous share of sequence:<br>`namespace_id (8 bytes) | info (1 byte) | data`

Compact shares have the added constraint: the first byte of `data` in each share is a reserved byte so the format is:<br>`namespace_id (8 bytes) | info (1 byte) | data length (varint 1 to 10 bytes) | reserved (1 byte) | data` and every unit in the compact share `data` is prefixed with a `unit length (varint 1 to 10 bytes)`.

Where `info (1 byte)` is a byte with the following structure:

- the first 7 bits are reserved for the version information in big endian form (initially, this will be `0000000` for version 0);
- the last bit is a **sequence start indicator**, that is `1` if the share is at the start of a sequence or `0` if it is a continuation share.

Note: all compact shares in a reserved namespace are grouped into one sequence.

Rationale:

1. The first 9 bytes of a share are formatted in a consistent way regardless of the type of share (compact or sparse). Clients can therefore parse shares into data via one mechanism rather than two.
1. The sequence start indicator allows clients to parse a whole message in the middle of a namespace, without needing to read the whole namespace.
1. The version bits allow us to upgrade the share format in the future, if we need to do so in a way that different share formats can be mixed within a block.

## Example

| share number             | 10                               | 11                               | 12                               | 13                               |
| ------------------------ | -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| namespace                | `[]byte{1, 1, 1, 1, 1, 1, 1, 1}` | `[]byte{1, 1, 1, 1, 1, 1, 1, 1}` | `[]byte{1, 1, 1, 1, 1, 1, 1, 1}` | `[]byte{2, 2, 2, 2, 2, 2, 2, 2}` |
| version                  | `0000000`                        | `0000000`                        | `0000000`                        | `0000000`                        |
| sequence start indicator | `1`                              | `1`                              | `0`                              | `1`                              |
| data                     | foo                              | bar                              | bar (continued)                  | buzz                             |

Without the universal share prefix: if a client is provided share 11, they have no way of knowing that a message length delimiter is encoded in this share. In order to parse the bar message, they must request and download all shares in this namespace (shares 10 and 12) and parse them in-order to determine the length of the bar message.

With the universal share prefix: if a client is provided share 11, they know from the prefix that share 11 is the start of a sequence and can therefore parse the data length delimiter in share 11. With the parsed data length, the client knows that the bar message will complete after reading N bytes (where N includes shares 11 and 12) and can therefore avoid requesting and downloading share 10.

## Questions

1. Does the info byte introduce any new attack vectors?
1. Should one bit in the info byte be used to signify that a continuation share is expected after this share?
    - This **continuation share indicator** is inspired by [protocol buffer varints](https://developers.google.com/protocol-buffers/docs/encoding#varints) and [UTF-8](https://en.wikipedia.org/wiki/UTF-8).
    - The **continuation share indicator** is distinct from the **sequence start indicator**. Consider a message with 3 contiguous shares:

        | share number                 | 1   | 2   | 3                                                                        |
        | ---------------------------- | --- | --- | ------------------------------------------------------------------------ |
        | sequence start indicator     | `1` | `0` | `0`                                                                      |
        | continuation share indicator | `1` | `1` | `0` <- client stops requesting contiguous shares when they encounter `0` |

    - This would enable clients to begin parsing a message by sampling a share in the middle of a message and proceed to parsing contiguous shares until the end without ever encountering the first share of the message which contains the data length. However, this use case seems contrived because a subset of the message shares may not be meaningful to the client. This depends on how roll-ups encode the data in a `PayForData` transaction.
    - Without the continuation share indicator, the client would have to request the first share of the message to parse the data length. If they don't request the first share, they can request contiguous shares until they reach the first share after their message ends to learn that they completed requesting the previous message.

1. What happens if a block producer publishes a message with a version that isn't in the list of supported versions?
    - This can be considered invalid via a `ProcessProposal` validity check. Validators already compute the shares in `ProcessProposal` [here](https://github.com/rootulp/celestia-app/blob/ad050e28678119adae02536db3ef5ce083ea1436/app/process_proposal.go#L104-L110) so we can add a check to verify that every share has a known valid version.
1. What happens if a block producer publishes a message where the sequence start indicator isn't set correctly?
    - Add a check similar to the one above.

## Alternative Approaches

We briefly considered adding the info byte to only sparse shares, see <https://github.com/celestiaorg/celestia-app/pull/651>. This approach was a miscommunication for an earlier proposal and was deprecated in favor of this ADR.

## Decision

Accepted

## Implementation Details

A share version must be specified by a user when authoring a [`MsgWirePayForData`](https://github.com/rootulp/celestia-app/blob/6f3b3ae437b2a70d72ff6be2741abb8b5378caa0/x/payment/types/tx.pb.go#L34) because if a user doesn't specify a share version, a block producer may construct message shares associated with their `MsgWirePayForData` using a different share version. Different share versions will lead to different share layouts which will lead to different `MessageShareCommitment`s. As a result, message inclusion proofs would fail. [See celestia-app#936](https://github.com/celestiaorg/celestia-app/issues/936).

Constants

1. Define a new constant for `InfoBytes = 1`.
1. Update [`CompactShareContentSize`](https://github.com/celestiaorg/celestia-app/blob/566b3d41d2bf097ac49f1a925cb56a3abeabadc8/pkg/appconsts/appconsts.go#L29) to account for one less byte available
1. Update [`SparseShareContentSize`](https://github.com/celestiaorg/celestia-app/blob/566b3d41d2bf097ac49f1a925cb56a3abeabadc8/pkg/appconsts/appconsts.go#L32) to account for one less byte available

Types

1. Introduce a new type `InfoByte` to encapsulate the logic around getting the `Version()` or `IsSequenceStart()` from a share.
1. Remove the `NamespacedShare` type.
1. Introduce a `ShareSequence` type.

Logic

1. Account for the new `InfoByte` in all share splitting and merging code.
1. Encode a total sequence length varint into the first compact share of a sequence.
1. Introduce a new `ParseShares` API that can accept any type of share (compact or sparse).
1. Introduce new block validity rules:
    1. All shares contain a share version that belongs to a list of supported versions (initially this list contains version `0`)
    1. All shares in a reserved namespace belong to one share sequence

## Status

Implemented

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
