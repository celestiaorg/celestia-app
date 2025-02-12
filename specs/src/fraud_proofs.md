# Fraud Proofs

## Bad Encoding Fraud Proofs

In order for data availability sampling to work, light clients must be convinced
that erasure encoded parity data was encoded correctly. For light clients, this
is ultimately enforced via [bad encoding fraud proofs
(BEFPs)](https://github.com/celestiaorg/celestia-node/blob/v0.11.0-rc3/docs/adr/adr-006-fraud-service.md#detailed-design).
Consensus nodes must verify this themselves before considering a block valid.
This is done automatically by verifying the data root of the header, since that
requires reconstructing the square from the block data, performing the erasure
encoding, calculating the data root using that representation, and then
comparing the data root found in the header.

## Blob Inclusion

Blob inclusion fraud proofs allow light clients to verify that Pay-for-Blob (PFB) transactions in a block correctly correspond to their associated blobs. These proofs consist of two main components:

1. A PFB transaction inclusion proof that demonstrates the PFB transaction exists in the block
2. A blob inclusion proof that verifies the blob data matches the commitment in the PFB transaction

The fraud proof mechanism works as follows:

1. The PFB inclusion proof contains:
   - The shares containing the PFB transaction
   - NMT proofs showing these shares exist in their respective row roots
   - Merkle proofs showing the row roots exist in the block's data root

2. The blob inclusion proof contains:
   - The subtree roots of the blob data
   - Merkle proofs showing these subtree roots exist in the correct position specified by the PFB

If a validator includes a PFB transaction but the corresponding blob data either doesn't exist or doesn't match the commitment, a fraud proof can be constructed to prove this violation. This allows light clients to detect and reject blocks containing invalid blob commitments without having to download the entire blob data.

The fraud proof verification process:
1. Verifies the PFB transaction inclusion and extracts:
   - The blob's starting index
   - The blob's length
   - The blob's commitment
2. Verifies the blob inclusion proof using the extracted information
3. Calculates the commitment over the proven blob data
4. Compares the calculated commitment with the one in the PFB transaction

If the commitments don't match, the fraud proof is valid and indicates the block is invalid. This mechanism ensures that validators cannot include PFB transactions without their corresponding valid blob data.

## State

State fraud proofs allow light clients to avoid making an honest majority assumption for
state validity. While these are not incorporated into the protocol as of v1.0.0,
there are example implementations that can be found in
[Rollkit](https://github.com/rollkit/rollkit). More info in
[rollkit-ADR009](https://github.com/rollkit/rollkit/blob/4fd97ba8b8352771f2e66454099785d06fd0c31b/docs/lazy-adr/adr-009-state-fraud-proofs.md).
