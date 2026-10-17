# Production coverage backlog

This testing plan addresses [PROTOCO-2971](https://linear.app/celestia/issue/PROTOCO-2971).
The audit was based on main at `494baf5d41ebca925dad7a3a29f1799c0cdbe292` (2026-10-16).
Before starting a follow-up, check current tests and rerun coverage to confirm
which work remains.
Each numbered follow-up below is a separate PR scope. Prioritize state and money
movement before CLI, RPC, and off-chain client coverage.

## Measuring coverage and scope

Use [the coverage script](../../scripts/test_cover.sh), including its current
production exclusions. It instruments eligible packages across test binaries,
credits `app/test` and integration suites to the code they exercise, removes
generated protobuf code, and combines duplicate blocks before counting statements.
The original audit predates [#7898](https://github.com/celestiaorg/celestia-app/pull/7898)
and [#7899](https://github.com/celestiaorg/celestia-app/pull/7899).
Also retain the normalization in [#8017](https://github.com/celestiaorg/celestia-app/pull/8017)
and production scope in [#8170](https://github.com/celestiaorg/celestia-app/pull/8170).
Do not compare the original percentage directly with this denominator.

```sh
go version
git rev-parse HEAD
umask 0022
make test-coverage
go tool cover -func=coverage.txt > /tmp/coverage-functions.txt
go tool cover -html=coverage.txt -o /tmp/coverage.html
```

Run without `-short` or `-run` filters. A failed command does not publish a new
profile; an old `coverage.txt` is not evidence for the current commit.
The per-test-binary percentages printed with `-coverpkg` are not per-package
totals. Use the merged profile for the baseline and function inventory.
The default build profile does not measure multiplexer-tagged paths or external
Docker E2E binaries. Measure those separately and identify their build tags.
Tools, scripts, generated code, and test infrastructure are outside this backlog.

Inspect low-level error returns, impossible marshal failures, constructors, and
service wiring for reachability before adding tests. Do not remove production
code from the denominator to reach the target. Generate current function coverage
with the commands above instead of maintaining counts or rankings in this plan.

## Existing coverage to retain

| Area | Evidence already on main | Consequence for follow-ups |
| --- | --- | --- |
| Proposal handling | [PrepareProposal handler tests](../../app/test/prepare_proposal_handler_test.go), [ProcessProposal failures](../../app/test/process_proposal_failure_test.go), [context tests](../../app/test/proposal_context_test.go), [settlement rollback](../../app/filtered_square_builder_fibre_test.go) | Extend specific missing decisions; do not recreate the proposal suites. |
| Cache correctness | [PFF replay parity](../../app/test/finalize_block_test.go), [PFF mutation tests](../../app/test/process_proposal_fibre_test.go), [blob cache validation](../../app/pfb_tx_cache_validation_test.go) | Percentage alone cannot establish cache safety. Preserve cold/warm parity assertions. |
| Decoding | [Index wrapper cases](../../app/encoding/index_wrapper_decoder_test.go), [empty/nil/blob decoder tests](../../app/encoding/encoding_test.go), [classification parity](../../x/fibre/types/classified_tx_test.go), [decoded-cache alignment](../../x/fibre/types/classify_decoded_test.go) | These are already tested. Review malformed wrapper and cache-key fallback branches individually. |
| Fibre keeper | [Message server suite](../../x/fibre/keeper/msg_server_test.go), [ABCI suite](../../x/fibre/keeper/abci_test.go), [keeper suite](../../x/fibre/keeper/keeper_test.go), [validator cache](../../x/fibre/keeper/validator_set_cache_test.go), [preverification and decode tests](../../x/fibre/keeper/preverify_test.go) | Credit escrow deposits, withdrawals, settlement routing, timeouts, pruning, replay rejection, freshness-floor export/import, signature checks, and serial/decoded verification parity. |
| Forwarding | [Keeper cases](../../x/forwarding/keeper/msg_server_test.go), [custom hooks](../../x/forwarding/keeper/custom_hook_test.go), [interop](../../test/interop/forwarding_integration_test.go) | Credit same-denom refund success, refund-error propagation, locked/unrelated balances, address binding, and real warp transfers. |
| Minfee | [Authority update](../../x/minfee/keeper/grpc_query_test.go), [legacy migration](../../x/minfee/keeper/migrations_test.go), [non-default genesis round-trip](../../x/minfee/keeper/genesis_test.go) | Authority success/rejection and non-default params survival are already covered. |
| Mint | [Inflation/annual provision tests](../../x/mint/keeper/abci_test.go), [time and provision tests](../../x/mint/types/minter_test.go), [integration suite](../../x/mint/test/mint_test.go) | Credit yearly schedule/floor, backward time, 15/30-second truncation, and accumulated rounding error checks. |
| Valaddr | [Registration](../../x/valaddr/keeper/msg_server_test.go), [cleanup boundaries](../../x/valaddr/keeper/endblocker_test.go), [genesis](../../x/valaddr/keeper/genesis_test.go), [serialization](../../x/valaddr/keeper/serialization_test.go) | Credit host validation, exact grace-period boundary, staking lookup-error abort, historical-host preservation, and round-trips. |
| ZK ISM | [Keeper tests](../../x/zkism/keeper) and [proof rejection regression PR](https://github.com/celestiaorg/celestia-app/pull/8159) | Retain rejection-without-state-mutation assertions when extending uncovered keeper paths. |
| Upgrade/export | [Upgrade tests](../../app/upgrades_test.go), [EndBlocker failures](../../app/end_blocker_test.go), [zero-height export regression](../../app/test/export_test.go) | Credit prefunded Fibre module-account replacement, missing consensus params, migration failure, and export after slashing. |
| Multiplexer | [Activation tests](../../multiplexer/abci/abci_activation_test.go), [startup synchronization](../../multiplexer/abci/multiplexer_sync_test.go), [version routing](../../multiplexer/abci/multiplexer_getapp_test.go) | Credit commit/snapshot activation and restart fixes; measure tagged embedded-binary paths separately. |
| DA commitments | [DA vectors](../../pkg/da/data_availability_header_test.go), [wrapper tests](../../pkg/wrapper/nmt_wrapper_test.go) | Keep pinned roots and pooled-tree parity; do not prioritize them merely because the original audit listed them. |

## Ranked behavioral follow-ups

These scopes describe missing assertions found by reading the existing suites.
An executed function may still need a behavioral test. Conversely, an unexecuted
defensive branch may not warrant fault injection. Use current coverage results
and review the existing assertions before choosing a follow-up.

### 1. Forwarding transaction rollback and conservation

Targets: `Forward`, `forwardToken`, and `calculateExcessIGPFee` in
[msg_server.go](../../x/forwarding/keeper/msg_server.go).

Add transaction-level cases where fee collection succeeds and warp dispatch or
the final refund fails. Use real cached bank/warp stores through the app transaction
boundary; the map-backed keeper mocks do not demonstrate transaction rollback.
Assert unchanged relayer/deposit/module balances and warp state on failure, and
no committed forwarding event. Account for the transaction's own gas fee separately.
For successful forwards, pin forwarded amount, actual IGP charge, and refund with
same and different fee denoms, zero fee, and exact fee consumption. Reuse the
interop fixture and existing custom-hook cases. This PR should add tests only.

### 2. Mint provision boundaries and keeper failure behavior

Targets: `CalculateBlockProvision`, `yearsSinceGenesis`, `mintBlockProvision`, and
`BeginBlocker` in [minter.go](../../x/mint/types/minter.go) and
[abci.go](../../x/mint/keeper/abci.go).

Add deterministic equal-time, sub-one-coin truncation, and one-nanosecond
anniversary-boundary cases with exact integer expectations. Add keeper assertions
for first-block nil previous time and zero annual provisions; the current setup
already commits a first block. Exercise mint/send failure propagation with a small
bank fake and assert previous time is not advanced after an error. Verify the
outer cached context discards prior mint writes on failure. Preserve the existing
yearly schedule and random accumulated-error test.
Include a small `Minter.Validate`/genesis table for valid state, negative inflation
or provisions, and empty denom. The `CalculateBlockProvision` and
`yearsSinceGenesis` cases should pin boundary behavior even when they do not
increase statement coverage.

### 3. Minfee parameter policy and store precedence

Targets: `UpdateMinfeeParams`, `GetParams`, `MigrateParams`, and `Params.Validate`
in [keeper](../../x/minfee/keeper) and [types](../../x/minfee/types/params.go).

Keep existing authority success/rejection cases. Add unchanged-store/no-update-event
assertions on unauthorized updates, typed event contents on success, and a
conflicting legacy/new-store value to pin new-store precedence after migration.
`Params.Validate` currently returns nil unconditionally: its caller's invalid-param
error branch cannot be reached with a parameter value. Explicitly record current
zero/negative-value behavior in tests. Any decision to reject those values is a
separate protocol change, not a coverage fix; do not invent a rejection test that
assumes a validation policy the code does not implement.

### 4. Fibre genesis records and iteration

Targets: `GenesisState.Validate`, module `ValidateGenesis`, `InitGenesis`, `ExportGenesis`, `IterateEscrowAccounts`,
`IterateWithdrawals`, and `IterateProcessedPayments` in
[genesis.go](../../x/fibre/keeper/genesis.go).

Extend the existing freshness-floor round-trip with non-empty escrow accounts,
withdrawals, and processed payments. Import into a fresh keeper; check every
record and the derived withdrawal/payment indexes, then run withdrawal processing
and replay rejection. Add early-stop iterator assertions. Keep this separate from
app upgrade changes. Credit the existing withdrawal, timeout, and pruning suites.
Exercise [genesis validation](../../x/fibre/types/genesis.go) through the module's
JSON validation entry point. Pin existing
rejections for invalid params, empty/duplicate signers, invalid balances or
available balance above total, invalid withdrawal amounts/timestamps, and
empty/duplicate processed hashes or zero processing times. Keep a valid populated
genesis as the positive control.

### 5. Valaddr storage failure paths

Targets: `GetFibreProviderInfo`, `IterateFibreProviderInfo`,
`RemoveStaleFibreProviders`, and message `SetFibreProviderInfo` in
[keeper](../../x/valaddr/keeper).

Reuse the lifecycle and genesis tests. With a small failing store service, pin
corrupt provider decoding, iterator-open failure, callback early stop, and
write/delete error propagation. Assert a sweep does not delete queued stale
entries when a later lookup or decode fails. Preserve the current distinction:
the getter returns not-found on decode/read errors, while iteration returns an
error. Do not redesign that API in this test PR.

### 6. Decoding and classification fallbacks

Targets: `blobTxDecoder` in [encoding](../../app/encoding/blob_tx_decoder.go),
`DecodePayForFibre` and `DecodedFromMsg` in
[decode.go](../../x/fibre/keeper/decode.go).

Credit the index-wrapper matrix and both classification-parity suites. Add only
remaining malformed-blob and decoded-promise fallback cases: ordinary bytes must
pass through unchanged, malformed recognized wrappers must return their error,
and inputs that cannot produce promise sign bytes must leave `PromiseKeyed` false.
Assert the final SDK decoder/validation verdict alongside fast classification.
Use real encoded envelopes and a decoder spy; do not mock out the decision under
test. Keep promise byte identity/context-cache matching assertions intact.

### 7. App export and migrations

Targets: `ExportAppStateAndValidators`, `prepForZeroHeightGenesis`, and the upgrade
handler in [export.go](../../app/export.go) and [upgrades.go](../../app/upgrades.go).

Extend existing zero-height/slashing and migration-error tests with a populated
export/import containing delegations, redelegations, unbondings, validator signing
information, and a jail allowlist. Pin reset heights, retained balances, and
validator output. Any newly discovered change to exported consensus state needs
its own reviewed fix. Do not repeat the existing module-account upgrade matrix.

### 8. Signal voting-power cleanup and read errors

Target: `TallyVotingPower` in [keeper.go](../../x/signal/keeper.go).
Extend the existing [keeper suite](../../x/signal/keeper_test.go) with removed and
unbonded validators, non-not-found validator lookup errors, and last-power read
errors. Assert stale signals are removed only for definitively missing validators
and no quorum is returned on read failure. Retain existing threshold/tally and
upgrade scheduling tests.

### 9. ZK ISM genesis rejection

Target: `InitGenesis` in [genesis.go](../../x/zkism/keeper/genesis.go).
Extend the existing [genesis suite](../../x/zkism/keeper/genesis_test.go)
with messages/submissions for an unknown ISM, malformed message IDs, and store
write/read error propagation. Assert each current error and exercise the outer
cached import boundary for rollback. Credit the existing populated round-trip,
large message export, and proof-rejection regressions.

## Non-consensus follow-ups

Schedule these after the consensus priorities. Retain eligible production code
in the denominator even when it has no package-local tests.

1. **Check-state account sequence query**: [QuerySequence](../../app/query_sequence.go),
   despite its committed-state comment, the query reads
   `app.CheckState()` and includes sequence increments from successful pending
   CheckTx transactions. Pin that behavior, an unknown account returning zero,
   missing check state, and other account-query errors. Credit the existing
   [pending-transaction integration test](../../test/util/testnode/query_sequence_test.go).
   Any switch to committed-state semantics needs a separate behavior-change PR.
2. **Transaction status RPC**: [TxStatus and TxStatusBatch](../../app/grpc/tx/server.go).
   Add direct fake-node tests for nil/empty/malformed IDs, batch size 20/21,
   unsupported clients, RPC errors, and response field/order mapping. Existing
   user-client mock-server tests do not by themselves cover the production server.
3. **Fibre app RPC adapter**: [AppClient](../../fibre/internal/grpc/app_client.go).
   Test node/query failures and malformed responses using an in-process gRPC
   fixture; preserve chain ID, timeout, and retention conversions. Credit
   [state cache tests](../../fibre/state/caching_client_test.go), host-registry,
   codec, and server health suites rather than duplicating them.
4. **Transaction worker funding**: [worker account initialization](../../pkg/user/parallel_tx_submission.go).
   Extend existing [worker tests](../../pkg/user/worker_account_test.go) and
   [retry tests](../../pkg/user/worker_account_retry_test.go) for uncovered funding,
   fee-grant, and partial-initialization errors. Assert sequence/account state and
   worker cleanup; retain existing cancellation, restart, and broadcast-error tests.
5. **Fibre storage/client errors**: use current coverage results to select one
   failing read/write or cancellation path per PR. Credit existing object-store,
   cleanup, upload lifetime/detach, escrow, and coordinator regressions. Use fake
   backends and deterministic cancellation rather than another full network suite.
6. **CLI and process startup**: `cmd/celestia-appd/cmd`, `fibre/cmd`, and module
   `client/cli` packages. Split by command family; test flag parsing, invalid
   config, and cleanup with in-process commands. Logging, profiling, and
   pass-through wiring follow funds/data-loss risks.
7. **Multiplexer process lifecycle**: [startApp and startGRPCServer](../../multiplexer/abci/multiplexer.go).
   Cover subprocess startup/cleanup and listener errors separately from app-version
   activation. Retain the activation, synchronization, and version routing suites.
   Report default and `ledger,multiplexer` profiles separately; this default
   baseline cannot establish coverage of embedded-binary execution.

## Completion rule for each follow-up

Record the base SHA, exact command/build tags, corrected before/after covered and
total statements, and the specific new behavioral assertions. Run the full
baseline before claiming global progress toward 80%; a filtered package test is
only a local measurement. Run timing/concurrency/network tests repeatedly and
with the race detector as appropriate. Keep each follow-up independently
reviewable and below the repository's PR-size limit. Do not change production
behavior solely to make a branch coverable.
