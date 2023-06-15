# Data Square Layout

<!-- toc -->

## Preamble

Celestia uses [a data availability scheme](https://arxiv.org/abs/1809.09044) that allows nodes to determine whether a block's data was published without downloading the whole block. The core of this scheme is arranging data in a two-dimensional matrix then applying erasure coding to each row and column. This document describes the rationale for how data—transactions, blobs, and other data—[is actually arranged](./data_structures.md#arranging-available-data-into-shares). Familiarity with the [originally proposed data layout format](https://arxiv.org/abs/1809.09044) is assumed.

## Layout Rationale

Block data consists of:

1. Standard cosmos-SDK transactions: (which are often represented internally as the [`sdk.Tx` interface](https://github.com/celestiaorg/cosmos-sdk/blob/v1.14.0-sdk-v0.46.11/types/tx_msg.go#L42-L50)) as described in [cosmos-sdk ADR020](https://github.com/celestiaorg/cosmos-sdk/blob/v1.14.0-sdk-v0.46.11/docs/architecture/adr-020-protobuf-transaction-encoding.md)
    1. These transactions contain protobuf encoded [`sdk.Msg`](https://github.com/celestiaorg/cosmos-sdk/blob/v1.14.0-sdk-v0.46.11/types/tx_msg.go#L14-L26)s, which get executed atomically (if one fails they all fail) to update the Celestia state. The complete list of modules, which define the `sdk.Msg`s that the state machine is capable of handling, can be found in the [state machine modules spec](../specs/state_machine_modules.md). Examples include standard cosmos-sdk module messages such as [MsgSend](https://github.com/cosmos/cosmos-sdk/blob/f71df80e93bffbf7ce5fbd519c6154a2ee9f991b/proto/cosmos/bank/v1beta1/tx.proto#L21-L32)), and celestia specific module messages such as [`MsgPayForBlobs`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/proto/celestia/blob/v1/tx.proto#L16-L31)
1. Blobs: binary large objects which do not modify the Celestia state, but which are intended for a Celestia application identified with a provided namespace.

We want to arrange this data into a `k * k` matrix of fixed-sized [shares](../specs/shares.md), which will later be committed to in [Namespace Merkle Trees (NMTs)](https://github.com/celestiaorg/nmt/blob/v0.16.0/docs/spec/nmt.md) so that individual shares in this matrix can be proven to belong to a single data root.

The simplest way we can imagine arranging block data is to simply serialize it all in no particular order, split it into fixed-sized shares, then arrange those shares into the `k * k` matrix in row-major order. However, this naive scheme can be improved in a number of ways, described below.

First, we impose some ground rules:

1. Data must be ordered by namespace. This makes queries into a NMT commitment of that data more efficient.
1. Since non-blob data are not naturally intended for particular namespaces, we assign [reserved namespaces](./consensus.md#Reservered-Namespaces) for them. A range of namespaces is reserved for this purpose, starting from the lowest possible namespace.
1. By construction, the above two rules mean that non-blob data always precedes blob data in the row-major matrix, even when considering single rows or columns.
1. Data with different namespaces must not be in the same share. This might cause a small amount of wasted block space, but makes the NMT easier to reason about in general since leaves are guaranteed to belong to a single namespace.

Transactions can pay fees for a blob to be included in the same block as the transaction itself. It may seem natural to bundle the PayForBlob transaction that pays for a number of blobs with these blobs (which is the case in other blockchains with native execution, e.g. calldata in Ethereum transactions or OP_RETURN data in Bitcoin transactions), however this would mean that processes validating the state of the Celestia network would need to download all blob data. PayForBlob transactions must therefore only include a commitment to (i.e. some hash of) the blob they pay fees for. If implemented naively (e.g. with a simple hash of the blob, or a simple binary Merkle tree root of the blob), this can lead to a data availability problem, as there are no guarantees that the data behind these commitments is actually part of the block data.

To that end, we impose some additional rules onto _blobs only_: blobs must be placed is a way such that both the transaction sender and the block producer can be held accountable—a necessary property for e.g. fee burning. Accountable in this context means that

1. The transaction sender must pay sufficient fees for blob inclusion.
1. The block proposer cannot claim that a blob was included when it was not (which implies that a transaction and the blob it pays for must be included in the same block). In addition all blobs must be accompanied by a PayForBlob transaction.

Specifically, blobs must begin at a new share. We note a nice property from this rule: if the transaction sender knows 1) `k`, the size of the matrix, 2) the starting location of their blob in a row, and 3) the length of the blob (they know this since they are sending the blob), then they can actually compute a sequence of roots to _subtrees in the row NMTs_. More importantly, anyone can compute this, and can compute _the simple Merkle root of these subtree roots_.

This, however, requires the block producer to interact with the transaction sender to provide them the starting location of their blob, so that the sender can sign over the commitment based on that starting location. This can be done selectively, but is not ideal as a default for e.g. end-user wallets.

### Blob Share Commitment Rules

To make the possible starting locations of blobs sufficiently predictable and constrained such that users can deterministically compute subtree roots, needed for the `ShareCommitment`s within a PFB, without the need to interact with the block proposer, we impose one additional rule:

> Blobs must start at an index that is a multiple of the `SubtreeWidth`. The `SubtreeWidth` is the length of the blob in shares, divided by the [`SubtreeRootThreshold`](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/pkg/appconsts/v1/app_consts.go#L6) and rounded up to the nearest power of 2 ([implementation here](https://github.com/celestiaorg/celestia-app/blob/v1.0.0-rc2/pkg/shares/non_interactive_defaults.go#L94-L116)).

The `SubtreeRootThreshold` is an arbitrary versioned protocol constant that aims to put a soft limit on the number of subtree roots included in a blob inclusion proof, as described in [ADR013](../../../docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md). A higher `SubtreeRootThreshold` means less padding and more tightly packed squares but also means greater proof sizes.
With the above constraint, we can compute subtree roots deterministically. For example, a blob of 128 shares and `SubtreeRootThreshold` (SRT) = 64, must start on a share index that is a multiple of 2 because 128/64 = 2. In this case, there will be a maximum of 1 share of padding between blobs (more on padding below). The maximum subtree width in shares will also be 2, meaning that there will be 2 shares under each subtree root.

The last piece of the puzzle is determining _which_ row the blob is placed at (or, more specifically, the starting location). This is needed to keep the block producer accountable. To this end, the block producer simply augments each fee-paying transaction with the starting locations of the blobs the transaction pays for.

### Caveats

The blob placement rules described above conflict with the first rule that shares must be ordered by namespace ID, as shares between two blobs that are not placed adjacent to each other do not have a natural namespace they belong to. This is resolved by requiring that such shares have a value of zero and a namespace ID equal to the preceding blob's. Since their value is known, they can be omitted from NMT proofs of all shares of a given namespace ID.
