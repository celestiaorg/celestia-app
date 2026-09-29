# Fibre documentation review

This review compares the Fibre reader-facing documentation with the current implementation in this checkout. The review and edits were refreshed against the current PR base, including nil-keyring clients, object storage, configurable transport limits, and the certificate signature cache. The accompanying edits treat executable code and protobuf definitions as the source of truth. They change documentation only.

## Findings and proposed corrections

| Existing documentation claim or omission | Correction in the draft | Implementation evidence |
| --- | --- | --- |
| The module spec puts validator-signature verification in the settlement handler. | Describe CheckTx and ProcessProposal verification, certificate caching, separate gas charging, and the FinalizeBlock skip. | [Ante decorators](../x/fibre/ante/ante.go), [message handler](../x/fibre/keeper/msg_server.go) |
| The module README requires **more than** two thirds; the spec says every supplied signature is verified. | State the actual integer threshold: at least `floor(2 * total_power / 3)`. Reject an oversized signature slice up front; stop verification when quorum is reached. | [SignatureSet](../fibre/validator/signature_set.go), [validator signature verification](../x/fibre/keeper/msg_server.go) |
| The module spec describes `ValidatePaymentPromise` as stateful-only. | Explain that the default query also verifies the owner signature and reserves a validator-local budget. Distinguish that cache from consensus state and from server storage occupancy. | [Query handler](../x/fibre/keeper/grpc_query.go), [local cache](../x/fibre/keeper/local_promise_cache.go), [app wiring](../app/app.go) |
| Freshness validation, genesis, and BeginBlock descriptions omit the persisted floor and future-timestamp bound. | Document chain-ID checking, the monotonic freshness cutoff, the ten-minute clock-skew bound, and the genesis field. | [Stateful validation](../x/fibre/keeper/keeper.go), [BeginBlock](../x/fibre/keeper/abci.go), [genesis schema](../proto/celestia/fibre/v1/genesis.proto) |
| Server docs say there is no resource exhaustion response, retry hint, or transport concurrency limit. | Document shard occupancy admission, `ResourceExhausted` with `RetryInfo`, connection/stream caps, and client retries. Distinguish wrapped app-query failures from storage-budget rejection. | [Upload handler](../fibre/server_upload.go), [transport](../fibre/internal/grpc/server.go), [upload retries](../fibre/client_upload.go) |
| Server overview says storage ends at promise expiry; storage section puts metadata before file rename. | Retain until `max(expiry, creation + retention)`; describe payload-before-metadata commit, local and object backends, and marker retention until pruning. | [Retention calculation](../fibre/server_upload.go), [store](../fibre/store.go), [backend configuration](../fibre/store_config.go) |
| Registry spec refers to a build tag and the old unfiltered `AllFibreProviders` API. | Describe default compilation, `AllBondedFibreProviders`, its REST route, bonded filtering, and stale-record cleanup. | [App wiring](../app/app.go), [query schema](../proto/celestia/valaddr/v1/query.proto), [query handler](../x/valaddr/keeper/grpc_query.go), [cleanup](../x/valaddr/keeper/keeper.go) |
| Quickstart suggests batching several blobs in a PFF. | Require one promise per PFF and exactly one PFF message per transaction. | [Transaction classification](../app/filtered_square_builder.go), [proposal validation](../app/process_proposal.go) |
| Auto-funding is described as a general upload feature; the ledger is said never to overstate funds. | Scope admission to auto-funded `Put` calls using that ledger. Explain external spending and direct `Upload` bypasses, and correctly distinguish refunded pre-dispatch failures from retained reservations. | [Put reservations](../fibre/client_put.go), [Upload](../fibre/client_upload.go), [ledger](../fibre/escrow_ledger.go) |
| A startup grace of one promise timeout is said to make restart seeding exact. | Remove the guarantee: timeout expiry opens a further settlement window. A wait alone does not establish that outstanding liabilities have settled. | [Timeout settlement](../x/fibre/keeper/msg_server.go), [freshness validation](../x/fibre/keeper/keeper.go), [startup grace](../fibre/escrow_ledger.go) |
| Quickstart omits `Blob.Free` and does not explain retrieval height; the client spec still requires a keyring for downloads. | Release pooled data, document nil-keyring download clients, and explain promise height versus settlement height. | [Client constructor](../fibre/client.go), [download](../fibre/client_download.go), [PutResult](../fibre/client_put.go) |
| Operator docs put config directly under home, imply no startup traffic outside the active set, and link to a missing dashboard. | Use `home/config/server_config.toml`; describe the zero-budget startup warning and temporary unlimited behavior; link to the existing observability dashboard. | [Config path](../fibre/server_config.go), [server startup](../fibre/server.go), [payment signing](../fibre/server_upload.go) |

The encoding format and TLS identity references were cross-checked with the blob, codec, system-blob, and TLS implementation. This is a documentation consistency review, not a proof of the protocol's security or a claim that every code path was audited.

## Introducing Fibre to a new reader

The previous landing page was primarily a table of contents. The next pages immediately introduced Go types, protobufs, and validation details. That left the reader to assemble the protocol's purpose and lifecycle from several references. Some repeated flow descriptions also contradicted more recent sections in the same document.

The proposed structure keeps one introductory narrative at [How Fibre works](../specs/src/fibre.md), followed by task-oriented quickstarts and detailed references. The introduction explains:

- Who publishes, stores, settles, and reads a blob.
- Why erasure coding and assignment allow partial replication.
- What a server signature attests to and what consensus records.
- How upload success, settlement, and verified retrieval differ.
- Why promise expiry, shard retention, and withdrawal delay are separate clocks.
- Which metadata readers must retain and what the client does not verify.

The detailed specs keep exact APIs and byte layouts. The module spec's duplicated settlement lists were removed rather than adding a third version of the same flow. Future changes should update the relevant reference and only update the introduction when reader-visible behavior changes.

## Follow-up documentation work

The ADRs should retain their design history, but would benefit from short implementation-status notes linking to current references. In particular, [ADR 025](architecture/adr-025-fibre-local-promise-cache.md) still says “Proposed” and “Decision: TBD” despite the implemented local cache. [ADR 029](architecture/adr-029-fibre-upload-rate-limit.md) says “Implemented” while its context describes the pre-limiter server. [ADR 027](architecture/adr-027-single-sequencer-ordering-fibre.md) is explicitly a draft and should not be presented as the current protocol. This draft does not infer a formal ADR decision from the presence of code.

The review focuses on the protocol specs, package/module READMEs, and operator guide. Experiment-specific instructions and every tool README have not been exhaustively audited. Code comments also repeat some stale recommendations, notably batching in `client_put.go` and timeout-length restart safety in the escrow configuration; updating those would be a useful separate cleanup.

## Validation

- `git diff --check` passes.
- Markdown lint introduces no new findings compared with the same files at the PR base. The locally available linter reports 55 existing table-format findings in those files; unrelated tables were left unchanged.
- Local Markdown link targets in all edited documents resolve. The operator guide's stale dashboard link was corrected.
- No Go files changed, so Go builds and tests were not run. The mdBook renderer is not installed locally; a rendered-book check remains outstanding.
