# Block Validity Rules

Unlike most blockchains, Celestia derives most of its functionality from
stateless commitments to data rather than stateful transitions. This means that
the protocol relies heavily on block validity rules. Notably, resource constrained light clients must be able to detect when these validity rules have not been followed in order to avoid making an honest majority assumption.

> **Note**
> Celestia relies on cometBFT (formerly tendermint) for consensus, meaning that it has single slot finality and is fork-free. Therefore, in order to ensure that an invalid block is never committed to, each validator must check that each block follows all validity rules before voting. If over two thirds of the voting power colludes to break a validity rule, then fraud proofs are created for light clients.

Before any Celestia specific validation is performed, all cometBFT [block validation
rules](https://github.com/cometbft/cometbft/blob/v0.34.28/spec/core/data_structures.md#block)
must be followed.

The only addition of the standard cometBFT block validation rules involve the
block data. CometBFT creates a commitment to the block data using a simple
binary merkle tree, while Celestia uses a binary merkle root over the row and
column namespaced merkle tree roots of an erasure encoded square.

<img src="./figures/data_root.svg" alt="Figure 1: Data Root" width="400"/>
<img src="./figures/rs2d_quadrants.svg" alt="Figure 2: rsmt2d" width="400"/>

Identically to cometBFT, these roots must be recalculated by all consensus and full nodes, if the roots do not match, then the block is invalid.

## Partiy Data

In order for data availability sampling to work, light clients must be convinced
that erasure encoded parity data was encoded correctly.

### Rule

All parity data must be erasure encoded. The original block data

Full nodes ensure the encoding was performed correctly by reconstructing the square in figure 2, then comparing the resulting data root. 

Light clients verify the validity of the erasure encoding this by using Bad Encoding Fraud Proofs (BEFPs)

### BEFP

#### Fraud Proof Status

Implemented

## BlobTx Validity Rules

All BlobTx must abide by the BlobTx Validity rules, which includes the MsgPayForData Validity Rules.

### State Fraud Proofs

#### Fraud Proof Status

Not Implemented

## Square Construction

### Lexicographical Ordering Proof (#97 NMT)

#### Fraud Proof Status

Not Implemented

## Blob Inclusion

### Non-interaction rules

## State Validity

### State Fraud Proofs

#### Fraud Proof Status

Planned but not implemented

## Share Encoding
