// Package shares provides primitives for splitting block data into shares and
// parsing shares back into block data.
//
// # Compact vs. Sparse
//
// There are two types of shares:
//  1. Compact
//  2. Sparse
//
// Compact shares can contain data from one or more unit (transactions or
// intermediate state roots). Sparse shares can contain data from zero or one
// blob. Compact shares and sparse shares are encoded differently. The
// motivation behind the distinction is that transactions and intermediate state
// roots are expected to have small lengths so they are encoded in compact
// shares to minimize the number of shares needed to store them. On the other
// hand, blobs are expected to be larger and have the desideratum that clients
// should be able to create proofs of blob inclusion. This desiradum is
// infeasible if client A's blob is encoded into a share with another client B's
// blob that is unknown to A. It follows that client A's blob is encoded into a
// share such that the contents can be determined by client A without any
// additional information. See [message layout rational] or
// [adr-006-non-interactive-defaults] for more details.
//
// # Universal Prefix
//
// Both types of shares have a universal prefix. The first 1 byte of a share
// contains the namespace version. The next 32 bytes contain the namespace ID.
// The next one byte contains an [InfoByte] that contains the
// share version and a sequence start indicator. If the sequence start indicator
// is `1` (i.e. this is the first share of a sequence) then the next 4 bytes
// contain a big endian uint32 of the sequence length.
//
// For the first share of a sequence:
//
//	| namespace_version | namespace_id | info_byte | sequence_length | sequence_data             |
//	| 1 byte            | 32 bytes     | 1 byte    | 4 bytes         | remaining bytes of share  |
//
// For continuation share of a sequence:
//
//	| namespace_version | namespace_id | info_byte | sequence_data             |
//	| 1 byte            | 32 bytes     | 1 byte    | remaining bytes of share  |
//
// The remaining bytes depend on the share type.
//
// # Compact Share Schema
//
// The four bytes after the universal prefix are reserved for
// the location in the share of the first unit of data that starts in this
// share.
//
// For the first compact share:
//
//	| namespace_version | namespace_id | info_byte | sequence_length | location_of_first_unit | transactions or intermediate state roots            |
//	| 1 byte            | 32 bytes     | 1 byte    | 4 bytes         | 4 bytes                | remaining bytes of share                            |
//
// For continuation compact share:
//
//	| namespace_version | namespace_id | info_byte | location_of_first_unit | transactions or intermediate state roots            |
//	| 1 byte            | 32 bytes     | 1 byte    | 4 bytes                | remaining bytes of share                            |
//
// Notes
//   - All shares in a reserved namespace belong to one sequence.
//   - Each unit (transaction or intermediate state root) in data is prefixed with a varint of the length of the unit.
//
// # Sparse Share Schema
//
// The remaining bytes contain blob data.
//
// [message layout rational]: https://celestiaorg.github.io/celestia-specs/latest/rationale/message_block_layout.html#message-layout-rationale
// [adr-006-non-interactive-defaults]: https://github.com/celestiaorg/celestia-app/pull/673
//
// [namespace.ID]: https://github.com/celestiaorg/nmt/blob/master/namespace/id.go
package shares
