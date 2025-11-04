# ADR 024: Extend TxStatus to Include Transaction Signers

## Changelog

- 2025-10-30: Initial draft (@ninabarbakadze)

## Status

Implemented

## Context

The `TxClient` must expose which account signed each transaction once it’s confirmed onchain. With parallel tx submission, different worker accounts sign transactions, so users can’t assume which account signed a given one. Applications need to track which worker was signing a given tx, and users should be able to easily see the signing address.

### Initial Approach and Its Limitations

The initial approach to populating tx signers involved parsing them directly from the transaction bytes (`TxBytes`) stored in the `TxClient`'s `TxTracker`. This approach worked as follows:

1. When a transaction was submitted, its bytes were stored in an in-memory `TxTracker`
2. On confirmation, the `ConfirmTx()` method would parse the signers from these stored transaction bytes
3. After successful confirmation, the transaction would be pruned from the `TxTracker` to free up memory

**The Problem:** Once a transaction was confirmed and pruned from the `TxTracker`, its signer information could no longer be retrieved.

To repeatedly confirm transactions and provide signer info, signer data or tx bytes need to be persisted somewhere. Previously, this was handled by the `KV` indexer but it was too resource intensive. We replaced it with the `TxStatus` endpoint in [celestia-core](https://github.com/celestiaorg/celestia-core/blob/237ebe4e1839ea0b0ed3b8c0aad6d14b894c931b/rpc/core/routes.go#L61C1-L61C15), which indexes only a limited set of transaction fields.

The `TxStatus` endpoint currently does not include raw `TxBytes`. As a result, once transactions are pruned and the KV indexer is disabled, there's no way to recover the `TxBytes` from the hash.

**Solution:** To keep indexing lightweight while still being able to retrieve desired data, we need to persist signer and related transaction metadata directly in `TxStatus`.

## Decision

We decided to extend the ABCI `ExecTxResult` type to include a `signers` field, which is then persisted in the block store and made available through the `TxStatus` query. This makes signer info accessible at any given time alongside other execution metadata like gas usage and execution code.

### Implementation Approach

The solution involved changes across three repositories:

1. **cosmos-sdk** ([PR #694](https://github.com/celestiaorg/cosmos-sdk/pull/694)): Extend `ExecTxResult` in baseapp to extract and populate signers during transaction execution
2. **celestia-core** ([PR #2593](https://github.com/celestiaorg/celestia-core/pull/2593)): Extend protobuf definitions and block store to persist signer info
3. **celestia-app** ([PR #6036](https://github.com/celestiaorg/celestia-app/pull/6036)): Update `TxClient` and gRPC services to expose signers in responses

## Consequences

### Positive

- **Persistent Access**: Signer info is now stored with transaction metadata, allowing repeated queries for the signers.
- **Better API**: `TxStatus` provides all fields necessary to populate `TxResponse`

### Negative

- **Storage Overhead**: Each transaction result now stores signer information (typically 1-5 addresses per transaction)
- **Tech Debt**: Introduces tech debt in the API
- **Upstream Divergence**: Increases the diff between upstream and our forked repos (cosmos-sdk and celestia-core), especially since the changes include updates to protobuf definitions.

