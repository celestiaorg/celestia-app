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

If a malicious block producer incorrectly computes the 2D Reed-Solomon code for a block's data, a fraud proof for this can be presented. We assume that the light clients have the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) for each block. Given a [ShareProof](#shareproof), they can verify that the provided shares are included under the corresponding row roots committed in the `AvailableDataHeader`. The current proof format only uses row roots; column proofs are not generated.

### ShareProof

| name               | type                                                                                        | description                                                                                           |
|--------------------|---------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------|
| `data`             | `byte[]`                                                                                    | Concatenated shares covered by the proofs, in share order.                                            |
| `shareProofs`      | [NamespaceMerkleTreeInclusionProof](./data_structures.md#namespacemerkletreeinclusionproof)`[]` | NMT inclusion proofs for each row containing the shares.                                              |
| `rowProof`         | Merkle proof                                                                               | Proof that the row roots in `rowProof.rowRoots` are included in the data root committed in the header. |
| `namespaceId`      | `byte[29]`                                                                                 | Namespace ID of the proved shares.                                                                    |
| `namespaceVersion` | `uint32`                                                                                   | Namespace version of the proved shares.                                                               |

## Invalid State Update

If a malicious block producer incorrectly computes the state, a fraud proof for this can be presented. We assume that the light clients have the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) for each block. Hence, given a [ShareProof](#shareproof), they can verify if the `rowRoot` or `colRoot` specified by `isCol` and `position` commits to the corresponding [Share](./data_structures.md#share). Similarly, given the `height` of a block, they can access all elements within the [AvailableDataHeader](./data_structures.md#availabledataheader) and the [Header](./data_structures.md#header) of the block.
