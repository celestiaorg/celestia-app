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

TODO

## State

State fraud proofs allow light clients to avoid making an honest majority assumption for
state validity. While these are not incorporated into the protocol as of v1.0.0,
there are example implementations that can be found in
[Rollkit](https://github.com/rollkit/rollkit). More info in
[rollkit-ADR009](https://github.com/rollkit/rollkit/blob/4fd97ba8b8352771f2e66454099785d06fd0c31b/docs/lazy-adr/adr-009-state-fraud-proofs.md).
