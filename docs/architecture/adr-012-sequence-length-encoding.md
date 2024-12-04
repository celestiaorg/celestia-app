# ADR 12: Sequence Length Encoding

## Status

Implemented

## Changelog

- 2022/12/14: initial draft

## Context

The sequence length is written as a varint in both sparse shares and compact shares. In compact shares, the varint is padded to occupy 4 bytes because 4 bytes can contain the maximum possible sequence length (assuming a 512 byte share and a max square size of 128). The fixed 4 byte length enables the compact share writer to write the contents of sparse shares (i.e. transaction data) before it writes the sequence length. See [here](https://github.com/celestiaorg/celestia-app/blob/76153bf7f3263734f31e7afd84f1e48a2f573599/pkg/shares/split_compact_shares.go#L132-L145) and [here](https://github.com/celestiaorg/celestia-app/blob/76153bf7f3263734f31e7afd84f1e48a2f573599/pkg/shares/split_compact_shares.go#L113).

However, sparse shares do not pad the sequence length to 4 bytes. This inconsistency means there is a different code path for parsing sequence lengths from compact shares vs. sparse shares.

We would like to modify the implementation such that there is only one code path for parsing the sequence length. This document explores a few options for doing so.

## Option A: Remove the 4 bytes of padding from compact shares [celestia-app##1106](https://github.com/celestiaorg/celestia-app/issues/1106)

Pros

- Updates the implementation to match the specs

Cons

- Additional complexity. If the sequence length isn’t padded then the compact share writer has to shift the contents of the shares (e.g. transaction data) backwards or forwards a few bytes depending on the final sequence length which can only be determined after writing the shares. Since a single byte shift can cause a transaction to overflow (or underflow) a share, the compact share writer must also re-write all reserved bytes in all compact shares in the sequence. It seems possible but adds complexity.

## Option B: Pad the sequence length to 4 bytes for sparse shares [celestia-app#1092](https://github.com/celestiaorg/celestia-app/issues/1092)

Pros

- Easy to implement

Cons

- Strict protobuf parser will fail to parse.
- Hacky. We’re using a variable length encoding scheme but padding it to a fixed length. By padding to a fixed length, we lose the positives of a variable length encoding scheme (small space usage for small numbers, and flexibility to support larger numbers that can’t be contained in 4 bytes).
Inefficient space usage. Unlike compact share sequences which are bounded (i.e. only one compact share sequence per block for transactions and eventually one more for intermediate state roots), sparse share sequences aren’t bounded. The number of sparse share sequences is 1:1 with the number of PFBs included in a block so this option may waste up to 3 bytes per PFB in a block.
- Unable to support share sequences larger than N without a spec and implementation change. This becomes an issue if we raise the max square size to 1024 with 512 byte shares. See [Go playground](https://go.dev/play/p/xXsk4bIyCQS).

## Option C: Encode the sequence length with a fixed length (e.g. big endian uint32)

- If we choose this option, we should decide the number of bytes we want to allocated based on the maximum sequence length we expect to support.
  - 4 bytes is capable of storing an uint32. An uint32 can contain a max sequence length of 4,294,967,296 bytes. In other words, an uint32 works up until 4GiB blocks. To put this into context, this max sequence length is hit with 1024 byte share size and max square size of 2048.
  - 8 bytes is capable of storing an uint64. An uint64 can contain a max sequence length of 18,446,744,073,709,551,615 bytes so pebibyte scale.
- If we choose this option, we should decide on big endian vs. little endian? Proposal: big endian because it seems more user-friendly and more common on the network
  - Integers in Fuel are big endian. See <https://fuellabs.github.io/fuel-specs/master/vm/index.html?highlight=endian#semantics>.
  <!-- markdown-link-check-disable -->
  - Bitcoin is little endian. Ref: <https://learnmeabitcoin.com/technical/little-endian>.
  <!-- markdown-link-check-enable -->
  - Ethereum uints are little endian. Ref: <https://jeancvllr.medium.com/solidity-tutorial-all-about-bytes-9d88fdb22676>

Pros

- Reduces complexity because fixed lengths make it easy to allocate placeholder bytes of the sequence length when writing compact shares.
- It may be easier to write parsers in non Go languages where varint isn't natively supported.

Cons

- Inefficient space usage for small sequence lengths

## Option D: Encode the sequence length and reserved bytes with a fixed length (e.g. big endian uint32)

Pros

- Consistent encoding for both the sequence length and the reserved bytes
- It may be easier to write parsers in non Go languages where varint isn't natively supported.

Cons

- Increased the number of reserved bytes from 2 to 4 which represents .3% of the share is potentially wasted. This downside seems acceptable given the number of compact shares is expected to be lower than the number of sparse shares.

## Option E: Extend protobuf and introduce a fixed16 type

Big endian uint32 seems equivalent to protobuf fixed32 but there is no fixed16. This option adds a fixed16 type to protobuf so that we can encode the sequence length as a fixed32 and the reserved bytes as a fixed16.

## Table

| Options  | Sequence length type                                      | Reserved bytes type                     |
| -------- | --------------------------------------------------------- | --------------------------------------- |
| Option A | varint                                                    | 2 byte padded varint                    |
| Option B | 4 byte padded varint                                      | 2 byte padded varint                    |
| Option C | 4 byte big endian uint32                                  | 2 byte padded varint                    |
| Option D | 4 byte big endian uint32                                  | 4 byte big endian uint32                |
| Option E | 4 byte big endian uint32 (equivalent to protobuf fixed32) | 2 byte protobuf fixed16 (doesn't exist) |

## Decision

Option D

## Consequences

### Positive

### Negative

### Neutral

- All options retain the need for other language implementations to parse varints because the length delimiter that is prefixed to units in a compact share (e.g. a transaction) is still a varint.
- This document assumes that an encoded big endian uint32 is equivalent to a protobuf fixed32

## References

- <https://developers.google.com/protocol-buffers/docs/encoding#non-varint-nums>
