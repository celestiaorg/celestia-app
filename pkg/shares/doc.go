// Package shares provides primitives for splitting block data into shares and
// parsing shares back into block data.
//
// # Compact vs. Sparse
//
// There are two types of shares:
//  1. Compact
//  2. Sparse
//
// Compact shares can contain data from one or more unit (transactions,
// intermediate state roots, evidence). Sparse shares can contain data from zero
// or one message. Compact shares and sparse shares are encoded differently. The
// motivation behind the distinction is that transactions, intermediate state
// roots, and evidence are expected to have small lengths so they are encoded in
// compact shares to minimize the number of shares needed to store them. On the
// other hand, messages are expected to be larger and have the desideratum that
// clients should be able to create proofs of message inclusion. This desiradum
// is infeasible if client A's message is encoded into a share with another
// client B's message that is unknown to A. It follows that client A's message
// is encoded into a share such that the contents can be determined by client A
// without any additional information. See [message layout rational] or
// [adr-006-non-interactive-defaults] for more details.
//
// # Universal Prefix
//
// Both types of shares have a universal prefix. The first 8 bytes of a share
// contain the [namespace.ID]. The next byte is an [InfoByte] that contains the
// share version and a sequence start indicator. If the sequence start indicator
// is `1` (i.e. this is the first share of a sequence) then the next 1-10 bytes
// contain a varint of the uint64 sequence length.
//
// For the first share of a sequence:
//
//	| universal prefix                           | data                      |
//	| namespace_id | info_byte | sequence_length | sequence_data             |
//	| 8 bytes      | 1 byte    | 1-10 bytes      | remaining bytes of share  |
//
// For continuation share of a sequence:
//
//	| universal prefix         | data                      |
//	| namespace_id | info_byte | sequence_data             |
//	| 8 bytes      | 1 byte    | remaining bytes of share  |
//
// The remaining bytes depend on the share type.
//
// # Compact Share Schema
//
// The first byte after the universal prefix is a reserved byte that indicates
// the location in the share of the first unit of data that starts in this
// share.
//
// For the first compact share in a reserved namespace:
//
//	| universal prefix                           | reserved byte          | data                                                |
//	| namespace_id | info_byte | sequence_length | location_of_first_unit | transactions, intermediate state roots, or evidence |
//	| 8 bytes      | 1 byte    | 1-10 bytes      | 1 byte                 | remaining bytes of share                            |
//
// For continuation compact share in a reserved namespace:
//
//	| universal prefix         | reserved byte          | data                                                |
//	| namespace_id | info_byte | location_of_first_unit | transactions, intermediate state roots, or evidence |
//	| 8 bytes      | 1 byte    | 1 byte                 | remaining bytes of share                            |
//
// Notes
//   - All shares in a reserved namespace belong to one sequence.
//   - Each unit (transaction, intermediate state root, evidence) in data is prefixed with a varint of the length of the unit.
//   - Although the sequence_length varint can technically be 1-10 bytes to store the maximum possible uint64, in-practice it is hard-coded to 4 bytes to contain the maximum possible sequence_length (based on share size and maximum square size).
//
// # Sparse Share Schema
//
// The remaining bytes contain message data.
//
// [message layout rational]: https://celestiaorg.github.io/celestia-specs/latest/rationale/message_block_layout.html#message-layout-rationale
// [adr-006-non-interactive-defaults]: https://github.com/celestiaorg/celestia-app/pull/673
// [namespace.ID]: https://github.com/celestiaorg/nmt/blob/master/namespace/id.go
package shares
