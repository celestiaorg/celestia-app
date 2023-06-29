# Block Validity Rules

Unlike most blockchains, Celestia derives most of its functionality from
stateless commitments to data rather than stateful transitions. This means that
the protocol relies heavily on block validity rules. Notably, resource
constrained light clients must be able to detect when these validity rules have
not been followed in order to avoid making an honest majority assumption on the consensus network. This
has a significant impact on thier design. More information on how light clients verify
block validity rules can be foud in the [Fraud Proofs](./fraud_proofs.md) spec.

> **Note** Celestia relies on CometBFT (formerly tendermint) for consensus,
> meaning that it has single slot finality and is fork-free. Therefore, in order
> to ensure that an invalid block is never committed to, each validator must
> check that each block follows all validity rules before voting. If over two
> thirds of the voting power colludes to break a validity rule, then fraud
> proofs are created for light clients. After light clients verify fraud proofs,
> they halt.

Before any Celestia specific validation is performed, all CometBFT [block
validation
rules](https://github.com/cometbft/cometbft/blob/v0.34.28/spec/core/data_structures.md#block)
must be followed. The only deviation from these rules is how the data root
([DataHash](https://github.com/cometbft/cometbft/blob/v0.34.28/spec/core/data_structures.md#header))
is generated. Almost all of Celestia's functionality is derived from this
change, including how it proves data availability to light clients.

## Data Availability

The data for each block must be considered available before a given block can be
considered valid. For consensus nodes, this is done via an identical mechanism
to a normal CometBFT node, which involves downloading the entire block by each
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
availability sampling. This involves "splitting" the CometBFT block data into
shares. Along with the 2D scheme, Celestia also makes use of [namespaced merkle
trees (nmt)](https://github.com/celestiaorg/nmt). These are combined to create
the commitment over block data instead of the typical merkle tree used by
CometBFT.

<img src="./figures/data_root.svg" alt="Figure 1: Data Root" width="400"/> <img
src="./figures/rs2d_quadrants.svg" alt="Figure 2: rsmt2d" width="400"/>

### Square Construction

The construction of the square is critical in providing additional guarantees to
light clients. Since the data root is a commitment to the square, the
construction of that square is also vital to correctly computing it.

TODO
[data square layout](./data_square_layout.md)

#### Share Encoding

Each chunk of block data is split into equally size shares for sampling
purposes. The encoding was designed to allow for light clients to decode these
shares to retrieve relevant data and to be future-proof yet backwards
compatible. The share encoding is deeply integrated into square construction, and
therefore critical to calculate the data root.

See [shares spec](./shares.md)

## `BlobTx` Validity Rules

Each `BlobTx` consists of a transaction to pay for one or more blobs, and the
blobs themselves. Each `BlobTx` that is included in the block must be valid.
Those rules are described in [`x/blob` module
specs](../../../x/blob/README.md#validity-rules)
