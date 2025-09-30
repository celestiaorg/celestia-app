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

## Invalid Erasure Coding

If a malicious block producer incorrectly computes the 2D Reed-Solomon code for a block's data, a fraud proof for this can be presented. We assume that the light clients have the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) for each block. Hence, given a [ShareProof](#shareproof), they can verify if the `rowRoot` or `colRoot` specified by `isCol` and `position` commits to the corresponding [Share](./data_structures.md#share). Similarly, given the `height` of a block, they can access all elements within the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) of the block.

### ShareProof

| name       | type                                                                                        | description                                                                                       |
|------------|---------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------|
| `share`    | [Share](./data_structures.md#share)                                                         | The share.                                                                                        |
| `proof`    | [NamespaceMerkleTreeInclusionProof](./data_structures.md#namespacemerkletreeinclusionproof) | The Merkle proof of the share in the offending row or column root.                                |
| `isCol`    | `bool`                                                                                      | A Boolean indicating if the proof is from a row root or column root; `false` if it is a row root. |
| `position` | `uint64`                                                                                    | The index of the share in the offending row or column.                                            |

## Invalid State Update

If a malicious block producer incorrectly computes the state, a fraud proof for this can be presented. We assume that the light clients have the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) for each block. Hence, given a [ShareProof](#shareproof), they can verify if the `rowRoot` or `colRoot` specified by `isCol` and `position` commits to the corresponding [Share](./data_structures.md#share). Similarly, given the `height` of a block, they can access all elements within the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) of the block.
