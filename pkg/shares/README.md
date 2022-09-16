
# Shares

Package shares provides primitives for splitting block data into shares and
parsing shares back into block data.

## Compact vs. Sparse

There are two types of shares:

1. Compact shares
1. Sparse shares

Compact shares can contain data from one or more unit (transactions,
intermediate state roots, evidence). Sparse shares can contain data from zero or
one message. Compact shares and sparse shares are encoded differently. The
motivation behind the distinction is that transactions, intermediate state
roots, and evidence are expected to have small lengths so they are encoded in
compact shares to minimize the number of shares needed to store them. On the
other hand, messages are expected to be larger and have the desideratum that
clients should be able to create proofs of message inclusion. This desiradum is
infeasible if client A's message is encoded into a share with another client B's
message that is unknown to A. It follows that client A's message is encoded into
a share such that the contents can be determined by client A without any
additional information. See [message layout rational](https://celestiaorg.github.io/celestia-specs/latest/rationale/message_block_layout.html#message-layout-rationale) for additional details.

## Universal Prefix

Both types of shares have a universal prefix. The first 8 bytes of a share contain the [namespace.ID](https://github.com/celestiaorg/nmt/blob/master/namespace/id.go). The next byte is an [InfoReservedByte](./info_reserved_byte.go) that contains the share version and a message start indicator. The remaining bytes depend on the share type.

### Compact Share Schema

// TODO

### Sparse Share Schema

// TODO
