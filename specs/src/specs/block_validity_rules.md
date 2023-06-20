# Block Validity Rules

Unlike most blockchains, Celestia derives most of its functionality from
stateless commitments to data rather than stateful transitions. This means that
the protocol relies heavily on block validity rules. Notably, resource
constrained light clients must be able to detect when these validity rules have
not been followed in order to avoid making an honest majority assumption.

> **Note** Celestia relies on cometBFT (formerly tendermint) for consensus,
> meaning that it has single slot finality and is fork-free. Therefore, in order
> to ensure that an invalid block is never committed to, each validator must
> check that each block follows all validity rules before voting. If over two
> thirds of the voting power colludes to break a validity rule, then fraud
> proofs are created for light clients. After light clients verify fraud proofs,
> they halt.

Before any Celestia specific validation is performed, all cometBFT [block
validation
rules](https://github.com/cometbft/cometbft/blob/v0.34.28/spec/core/data_structures.md#block)
must be followed. The only deviation from these rules is how the data root
([DataHash](https://github.com/cometbft/cometbft/blob/v0.34.28/spec/core/data_structures.md#header))
is generated. Almost all of Celestia's functionality is derived from this
change, including how it proves data availability to light clients.

## Data Availability

The data for each block must be considered available before a given block can be
considered valid. For consensus nodes, this is done via an identical mechanism
to a normal cometBFT node, which involves downloading the entire block by each
node before considering that block valid.

Light clients however do not download the entire block. They only sample a
fraction of the block. More details on how sampling actually works can be found
in the seminal ["Fraud and Data Availability Proofs: Maximising Light Client
Security and Scaling Blockchains with Dishonest
Majorities"](https://arxiv.org/abs/1809.09044) and in the
[`celestia-node`](https://github.com/celestiaorg/celestia-node) repo.

Per the [LazyLedger white paper](https://arxiv.org/pdf/1905.09274.pdf), Celestia
uses a 2D Reed-Solomon coding scheme
([rsmt2d](https://github.com/celestiaorg/rsmt2d)) to accommodate data
availability sampling. This involves "splitting" the cometBFT block data into
shares. All with the 2D scheme, Celestia also makes use of [namespaced merkle
trees (nmt)](https://github.com/celestiaorg/nmt). These are combined to create
the commitment over block data instead of the typical merkle tree used by
cometBFT.

<img src="./figures/data_root.svg" alt="Figure 1: Data Root" width="400"/> <img
src="./figures/rs2d_quadrants.svg" alt="Figure 2: rsmt2d" width="400"/>

### Bad Encoding Fraud Proofs

In order for data availability sampling to work, light clients must be convinced
that erasure encoded parity data was encoded correctly. For light clients, this
is ultimately enforced via [bad encoding fraud proofs
(BEFPs)](https://github.com/celestiaorg/celestia-node/blob/v0.11.0-rc3/docs/adr/adr-006-fraud-service.md#detailed-design).
Consensus nodes must verify this themselves before considering a block valid.
This is done automatically by verifying the data root of the header, since that
requires reconstructing the square from the block data, performing the erasure
encoding, calculating the data root using that representation, and then
comparing the data root found in the header.

### Square Construction

The construction of the square is critical in providing additional guarantees to
light clients. Since the data root is a commitment to the square, the
construction of that square is also vital to correctly computing it.

TODO

#### Share Encoding

Each chunk of block data is split into equally size shares for sampling
purposes. The encoding was designed to allow for light clients to decode these
shares to retrieve relevant data and to be future-proof yet backwards
compatible. The share encoding is deeply integrated into square contraction, and
therefore critical to calculate the data root.

See [shares spec](./shares.md)

## `BlobTx` Validity Rules

Each `BlobTx` consists of a transaction to pay for the blob, and the blob
itself. Each `BlobTx` that is included in the block must be valid. Those rules
are described in [`x/blob` module
specs](../../../x/blob/README.md#validity-rules)

## Blob Inclusion

TODO

## State Fraud Proofs

State fraud proofs allow light clients to avoid making an honest majority for
state validity. While these are not incorporated into the protocol as of v1.0.0,
there are example implementations that can be found in
[Rollkit](https://github.com/rollkit/rollkit). More info in
[rollkit-ADR009](https://github.com/rollkit/rollkit/blob/4fd97ba8b8352771f2e66454099785d06fd0c31b/docs/lazy-adr/adr-009-state-fraud-proofs.md).
