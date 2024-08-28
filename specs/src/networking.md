# Networking

<!-- toc -->

## Wire Format

### AvailableData

| name                | type                                      | description   |
|---------------------|-------------------------------------------|---------------|
| `availableDataRows` | [AvailableDataRow](#availabledatarow)`[]` | List of rows. |

### AvailableDataRow

| name     | type                                    | description      |
|----------|-----------------------------------------|------------------|
| `shares` | [Share](./data_structures.md#share)`[]` | Shares in a row. |

### ConsensusProposal

Defined as `ConsensusProposal`:

```protobuf
{{#include ./proto/consensus.proto:ConsensusProposal}}
```

When receiving a new block proposal `proposal` from the network, the following steps are performed in order. _Must_ indicates that peers must be blacklisted (to prevent DoS attacks) and _should_ indicates that the network blob can simply be ignored.

1. `proposal.type` must be a `SignedMsgType`.
1. `proposal.round` is processed identically to Tendermint.
1. `proposal.pol_round` is processed identically to Tendermint.
1. `proposal.header` must be well-formed.
1. `proposal.header.version.block` must be [`VERSION_BLOCK`](./consensus.md#constants).
1. `proposal.header.version.app` must be a supported app version.
1. `proposal.header.height` should be previous known height + 1.
1. `proposal.header.chain_id` must be [`CHAIN_ID`](./consensus.md#constants).
1. `proposal.header.time` is processed identically to Tendermint.
1. `proposal.header.last_header_hash` must be previous block's header hash.
1. `proposal.header.last_commit_hash` must be the previous block's commit hash.
1. `proposal.header.consensus_hash` must be the hash of [consensus parameters](./data_structures.md#header).
1. `proposal.header.state_commitment` must be the state root after applying the previous block's transactions.
1. `proposal.header.available_data_original_shares_used` must be at most [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX ** 2`](./consensus.md#constants).
1. `proposal.header.available_data_root` must be the [root](./data_structures.md#availabledataheader) of `proposal.da_header`.
1. `proposal.header.proposer_address` must be the [correct leader](./consensus.md#leader-selection).
1. `proposal.da_header` must be well-formed.
1. The number of elements in `proposal.da_header.row_roots` and `proposal.da_header.row_roots` must be equal.
1. The number of elements in `proposal.da_header.row_roots` must be the same as computed [here](./data_structures.md#header).
1. `proposal.proposer_signature` must be a valid [digital signature](./data_structures.md#public-key-cryptography) over the header hash of `proposal.header` that recovers to `proposal.header.proposer_address`.
1. For full nodes, `proposal.da_header` must be the result of computing the roots of the shares (received separately).
1. For light nodes, `proposal.da_header` should be sampled from for availability.

### MsgWirePayForData

Defined as `MsgWirePayForData`:

```protobuf
{{#include ./proto/wire.proto:MsgWirePayForData}}
```

Accepting a `MsgWirePayForData` into the mempool requires different logic than other transactions in Celestia, since it leverages the paradigm of block proposers being able to malleate transaction data. Unlike [SignedTransactionDataMsgPayForData](./data_structures.md#signedtransactiondatamsgpayfordata) (the canonical data type that is included in blocks and committed to with a data root in the block header), each `MsgWirePayForData` (the over-the-wire representation of the same) has potentially multiple signatures.

Transaction senders who want to pay for a blob will create a [SignedTransactionDataMsgPayForData](./data_structures.md#signedtransactiondatamsgpayfordata) object, `stx`, filling in the `stx.blobShareCommitment` field [based on the blob share commitmentrules](../specs/data_square_layout.md#blob-share-commitment-rules), then signing it to get a [transaction](./data_structures.md#transaction) `tx`.

Receiving a `MsgWirePayForData` object from the network follows the reverse process: verify using the [blob share commitmentrules](../specs/data_square_layout.md#blob-share-commitment-rules) that the signature is valid.

## Invalid Erasure Coding

If a malicious block producer incorrectly computes the 2D Reed-Solomon code for a block's data, a fraud proof for this can be presented. We assume that the light clients have the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) for each block. Hence, given a [ShareProof](#shareproof), they can verify if the `rowRoot` or `colRoot` specified by `isCol` and `position` commits to the corresponding [Share](./data_structures.md#share). Similarly, given the `height` of a block, they can access all elements within the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) of the block.

### ShareProof

| name       | type                                                                                        | description                                                                                       |
|------------|---------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------|
| `share`    | [Share](./data_structures.md#share)                                                         | The share.                                                                                        |
| `proof`    | [NamespaceMerkleTreeInclusionProof](./data_structures.md#namespacemerkletreeinclusionproof) | The Merkle proof of the share in the offending row or column root.                                |
| `isCol`    | `bool`                                                                                      | A Boolean indicating if the proof is from a row root or column root; `false` if it is a row root. |
| `position` | `uint64`                                                                                    | The index of the share in the offending row or column.                                            |

### BadEncodingFraudProof

Defined as `BadEncodingFraudProof`:

```protobuf
{{#include ./proto/types.proto:BadEncodingFraudProof}}
```

| name          | type                                        | description                                                                       |
|---------------|---------------------------------------------|-----------------------------------------------------------------------------------|
| `height`      | [Height](./data_structures.md#type-aliases) | Height of the block with the offending row or column.                             |
| `shareProofs` | [ShareProof](#shareproof)`[]`               | The available shares in the offending row or column.                              |
| `isCol`       | `bool`                                      | A Boolean indicating if it is an offending row or column; `false` if it is a row. |
| `position`    | `uint64`                                    | The index of the offending row or column in the square.                           |

## Invalid State Update

If a malicious block producer incorrectly computes the state, a fraud proof for this can be presented. We assume that the light clients have the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) for each block. Hence, given a [ShareProof](#shareproof), they can verify if the `rowRoot` or `colRoot` specified by `isCol` and `position` commits to the corresponding [Share](./data_structures.md#share). Similarly, given the `height` of a block, they can access all elements within the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) of the block.

### StateFraudProof

Defined as `StateFraudProof`:

```protobuf
{{#include ./proto/types.proto:StateFraudProof}}
```

| name                        | type                                                                                      | description                                                                                                                                                                                           |
|-----------------------------|-------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `height`                    | [Height](./data_structures.md#type-aliases)                                               | Height of the block with the intermediate state roots. Subtracting one from `height` gives the height of the block with the transactions.                                                             |
| `transactionShareProofs`    | [ShareProof](#shareproof)`[]`                                                             | `isCol` of type `bool` must be `false`.                                                                                                                                                               |
| `isrShareProofs`            | [ShareProof](#shareproof)`[]`                                                             | `isCol` of type `bool` must be `false`.                                                                                                                                                               |
| `index`                     | `uint64`                                                                                  | Index for connecting the [WrappedIntermediateStateRoot](./data_structures.md#wrappedintermediatestateroot) and [WrappedTransaction](./data_structures.md#wrappedtransaction) after shares are parsed. |
