# Fuzz entry-point inventory

This is the celestia-app inventory for
[PROTOCO-3326](https://linear.app/celestia/issue/PROTOCO-3326), updated on
2026-10-28. The cross-repository inventory remains tracked by
[PROTOCO-3321](https://linear.app/celestia/issue/PROTOCO-3321).

## Priorities and scope

Priority 1 covers peer/client transactions and proposer-controlled blocks: a
failure affects admission or block validation on every node. Priority 2 covers
proofs and fibre transport: malformed bytes can crash a receiver or make
optimized codecs disagree with canonical decoding. Priority 3 covers internal
primitives reached after those boundaries validate their inputs.

The initial nightly baseline was seven targets. PRs
[#8349](https://github.com/celestiaorg/celestia-app/pull/8349) and
[#8381](https://github.com/celestiaorg/celestia-app/pull/8381) added eight, with
[#8383](https://github.com/celestiaorg/celestia-app/pull/8383) correcting the share
proof harness. This follow-up adds two focused ante targets and registers five
existing targets that were missing from the nightly script. Every target is
registered in [scripts/fuzz_targets.txt](../scripts/fuzz_targets.txt).

The two additional targets prioritize nested-message restrictions, which stop
ante bypasses, and fee/priority arithmetic, where attacker-selected gas and fees
cross rounding and integer-width boundaries. Structured nested messages pass
through the real transaction encoder/decoder before reaching the decorator.

## Covered surfaces

Each row records a fuzzed surface, its untrusted input, and its target. A target
can cover several functions; target presence does not establish branch coverage.

| Priority | Entry point / input source | Fuzz target(s) | Scope and limits |
| --- | --- | --- | --- |
| 1 | `app.CheckTx`: RPC and peer transaction bytes | `FuzzCheckTxMutatedTxs` in [app/test](../app/test/check_tx_fuzz_test.go) | Mutations of send, PFF, single-blob and multi-blob transactions; detects recovered panics. CheckTx state is reused. |
| 1 | `app.PrepareProposal`: mempool transactions | `FuzzCheckTxPrepareProposal` in [app/test](../app/test/check_tx_prepare_proposal_fuzz_test.go) | Re-signs structured messages and checks accepted transactions survive proposal construction. Arbitrary malformed mempool batches are outside this target. |
| 1 | `app.PrepareProposal` then `app.ProcessProposal`: malformed mempool batches | `FuzzPrepareProposalMalformedBatch` in [app/test](../app/test/prepare_proposal_fuzz_test.go) | Up to eight mutations, duplications, reorderings and arbitrary insertions over valid send, PFF and blob txs. Fails on a panic PrepareProposal recovered, or when ProcessProposal rejects the block PrepareProposal built. |
| 1 | `app.ProcessProposal`: proposer transaction bytes | `FuzzProcessProposalMutatedTxs` in [app/test](../app/test/process_proposal_fuzz_test.go) | Mutates a prepared proposal; detects recovered panics and requires unchanged proposals to be accepted. Header root and square size remain fixed. |
| 1 | `pkg/da.ExtendSharesWithTreePool` vs `ExtendShares`, `pkg/wrapper` buffered tree vs NMT wrapper: proposer-controlled original square shares | `FuzzExtendSharesParity` in [pkg/da](../pkg/da/extend_shares_fuzz_test.go) | Squares up to 8x8 with non-square counts, out-of-order namespaces and arbitrary share bytes; shares, per-axis roots and errors, and the data root must match. One tree pool is shared across inputs to exercise resize and reuse after failures. |
| 1 | `app.QuerySequence`: signer bytes named by CAT mempool peers | `FuzzQuerySequence` in [app/test](../app/test/query_sequence_fuzz_test.go) | Arbitrary signer bytes on the ABCI mempool connection, which has no panic recovery. Unknown signers must report sequence 0 and an account that committed a tx reports 1. |
| 1 | `app/ante.NestedMsgDecorator.AnteHandle`: decoded authz/governance containers | `FuzzNestedMsgDecorator` in [app/ante](../app/ante/nested_msg_fuzz_test.go) | Varies message type, position, count, wrapper and simulation mode; forbidden messages must never reach the next handler. Exercises the guard directly, independently of signature/fee rejection earlier in the ante chain. |
| 1 | `app/ante.PreverifyTxSignatures` and the cached signature verification decorator: signed tx bytes | `FuzzCachedSigVerification` in [app/ante](../app/ante/cached_sigverify_fuzz_test.go) | Byte edits and cross-tx splices over signed txs from two accounts at two sequences. After warming the cache, the cached decorator must reach the uncached decision and error for every tx that passes `ValidateBasic`. Txs whose `GetSignaturesV2` panics are excluded until PROTOCO-3482 is fixed. |
| 1 | `app/ante.ValidateTxFee`: transaction fee coins and gas limit | `FuzzValidateTxFee` in [app/ante](../app/ante/validate_tx_fee_fuzz_test.go) | Keeper-backed fee checker with a big-integer oracle for the network minimum, foreign fee denoms and CheckTx/DeliverTx modes. Fees above MaxInt64 are excluded until PROTOCO-3433 item 2 is fixed. |
| 1 | `app/ante.ProcessMessageSignatures`, `ISMSignatureGasDecorator`: `MsgProcessMessage` metadata hex | `FuzzISMSignatureWeight` in [app/ante](../app/ante/ism_signature_gas_fuzz_test.go) | Arbitrary strings and structured message-id and merkle-root multisig metadata with partial signatures, prefixes and letter case. The counted weight must be at least the signatures the hyperlane multisig metadata parsers recover after mailbox hex decoding, and the decorator must charge for them in direct and authz-wrapped messages. Routing ISM dispatch and signature validity are outside this target. |
| 1 | `app/ante.verifyMinFee`, `getTxPriority`: transaction gas/fees | `FuzzAnteFeeArithmetic` in [app/ante](../app/ante/fee_fuzz_test.go) | Uses a `math/big` oracle for rounded minimum fees and overflow-safe priority, including zero gas and values above MaxInt64. Random fees and prices span uint64; keeper-backed fee deduction is outside this target. |
| 1 | `app/encoding` tx decoder: raw tx bytes from peers, clients and proposers | `FuzzTxDecoderFastPath` in [app/encoding](../app/encoding/decoder_fuzz_test.go) | The decoder `MakeConfig` installs, including its plain-envelope fast path, must agree with the full BlobTx/IndexWrapper/empty-tx wrapper chain on errors and decoded tx bytes, for plain, wrapped and type-id-marked seeds. |
| 1 | `app.TxCache.Exists`, `App.ValidateBlobTxWithCache`: blob txs cached in CheckTx and replayed in ProcessProposal | `FuzzTxCacheValidation` in [app](../app/pfb_tx_cache_fuzz_test.go) | Mutates one of namespace, data, share version, signer, inner tx or blob order/count of a valid signed blob tx; the cache must hit only for field-identical txs. With a CheckTx-shaped cache (only fully validated txs inserted), cached validation must accept exactly when full validation does. |
| 1 | `app.pffDecodeCandidate`: raw transaction classification | `FuzzPFFDecodeCandidateParity` in [app](../app/pff_decode_candidate_fuzz_test.go) | Fast-path classification agrees with the SDK and fibre decoders. |
| 1 | `app.firstMsgIsPayForFibre`, `separateTxsDecoded`: PrepareProposal mempool tx routing | `FuzzFirstMsgIsPayForFibre` in [app](../app/filtered_square_builder_peek_fuzz_test.go) | Mutated signed and unsigned send, pay-for-fibre, multi-message and wrapped txs. Wherever the SDK decoder accepts the bytes, the wire-level peek must parse them and agree on whether the first message is a pay-for-fibre, and txs with only pay-for-fibre or only other non-PFB messages must reach the matching list. Mixed and PFB-carrying txs are routed but not checked. |
| 1 | `app.countICAPacketMsgs`: IBC packet contents | `FuzzCountICAPacketMsgs` in [app](../app/msg_count_test.go) | Fuzzes ICA message counting from packet data. |
| 1 | `app.countExecutableMsgs`, `countZKProofMsgs`: decoded tx messages | `FuzzCountTxMsgs` in [app](../app/msg_count_fuzz_test.go) | Structured txs of up to 64 messages mixing sends, `MsgExec`, `MsgModuleQuerySafe`, ICA packets under both channel encodings (structured or raw payloads) and ZK messages, decoded by the app decoder. Neither counter may report fewer messages or proofs than execution would dispatch. |
| 1 | `x/blob/types.ValidateBlobTx`, `ValidateBlobTxSkipCommitment`: blob transaction envelopes | `FuzzValidateBlobTx` in [x/blob/types](../x/blob/types/blob_tx_fuzz_test.go) | Cached validation must agree with full validation except for the skipped commitment check. |
| 1 | `x/blob/types.MsgPayForBlobs.ValidateBasic`, gas estimation: message fields | `FuzzMsgPayForBlobsValidateBasic`, `FuzzPFBGasEstimation` in [x/blob/types](../x/blob/types) | Inconsistent vector lengths and share versions; gas helpers run after their validation preconditions. |
| 1 | `x/blob/ante` `getSharesNeeded`, `validateEnoughGas` and the decorator walks over them: PFB message fields | `FuzzPFBShareAndGasValidation` in [x/blob/ante](../x/blob/ante/pfb_shares_fuzz_test.go) | Arbitrary blob sizes, share versions and namespace counts, including mismatched lengths, as a reordered ante chain could pass them before `ValidateBasic`. Share counts and gas verdicts must match a big-integer oracle. One input class is excluded pending a fix. |
| 1 | `x/valaddr/types.ValidateHost`: fibre provider host registered on chain | `FuzzValidateHost` in [x/valaddr/types](../x/valaddr/types/host_fuzz_test.go) | Accepted hosts must be bounded and split into a non-empty host and a port in range that join back to the input. One input class is excluded pending a fix. |
| 1 | `x/fibre/types.ParsePayForFibreTx`, `TryParseFibreTx`, `ClassifyTxs`: transaction bytes | `FuzzParsePayForFibreTx` in [x/fibre/types](../x/fibre/types/classified_tx_fuzz_test.go) | Parser/classifier agreement and system blob consistency; decoded PFF basic validation, signature cache keys and gas estimates. |
| 1 | `x/zkism` verifying keys, proofs and public values: `MsgCreateInterchainSecurityModule`, `MsgUpdateVerifyingKeys`, `MsgUpdateInterchainSecurityModule`, `MsgSubmitMessages` | `FuzzValidateGroth16Vkey`, `FuzzPublicValuesUnmarshal`, `FuzzParseMerkleProofMetadata`, `FuzzSP1Groth16VerifyProof` in [x/zkism/types](../x/zkism/types/decoders_fuzz_test.go); `FuzzUnmarshalProof` in [x/zkism/internal/groth16](../x/zkism/internal/groth16/proof_fuzz_test.go) | Accepted keys decode in gnark with bounded allocation and a canonical encoding; decoders round trip; only golden proof statements verify. The pairing runs only for proofs within a few bytes of a golden proof. Message servers and keeper state are outside these targets. |
| 1 | `x/fibre/keeper.PreverifySignatures`: block pay-for-fibre txs in PrepareProposal, ProcessProposal and PreBlocker | `FuzzPreverifySignatures` in [x/fibre/keeper](../x/fibre/keeper/preverify_fuzz_test.go) | Up to eight byte-level edits, duplications and removals over two valid signed PFF txs, with and without certificates and early stop. Every cache entry must pass the sequential verifier, and no other entry may be recorded. |
| 1 | `x/signal` `SignalVersion`, `TryUpgrade`, `VersionTally`: DeliverTx upgrade signals over the bonded validator set | `FuzzSignalTryUpgrade` in [x/signal](../x/signal/signal_fuzz_test.go) | Up to 48 signals, upgrade attempts, validator power, bonding and removal changes, upgrade activations and tally queries over up to eight validators, each message on a cache context. A `math/big` oracle decides whether and to which version TryUpgrade schedules an upgrade and checks tallies and stored signals. Bonded totals stay below 5e17, pending a fix to the threshold rounding above it. |
| 1 | `x/forwarding` `MsgForward`, `DeriveForwardingAddress` and `QuoteForwardingFee` queries: forward destinations, hook bindings and IGP fees | `FuzzDeriveForwardingAddress` in [x/forwarding/types](../x/forwarding/types/address_fuzz_test.go); `FuzzForwardingAddressQueryMsgParity`, `FuzzForwardIGPFee` in [x/forwarding/keeper](../x/forwarding/keeper/forward_fuzz_test.go) | Address derivation is injective up to equivalent hook bindings, and the query and message server agree on fuzzed hex inputs. Mocked bank, warp and hook keepers with amounts below 2^248; forwards succeed exactly when their preconditions hold, and the relayer's net cost is the fee the hook consumed. |
| 1 | `x/zkism/keeper` msg server `Create`, `Update`, `SubmitMessages`, `UpdateVerifyingKeys`, `TransferOwnership` and `Keeper.Verify`: DeliverTx ISM messages and Hyperlane message metadata | `FuzzZKISMMsgSequence` in [x/zkism/keeper](../x/zkism/keeper/msg_sequence_fuzz_test.go) | Up to 24 operations with golden SP1 proofs from `x/zkism/internal/testdata`, single-byte proof or public value mutations, fuzz-chosen owners, verifying keys, messages and ids, each message on a cache context. A model predicts every outcome: owner-only key updates and transfers, proofs accepted only unmodified under the matching state and keys, and `Verify` only for proven messages. Ids differing only in module type or specifier are mapped to the ISM, pending a fix. |
| 1 | `x/fibre/keeper` msg server `DepositToEscrow`, `RequestWithdrawal`, `PayForFibre`, `PaymentPromiseTimeout`, `UpdateFibreParams` and `BeginBlocker`: DeliverTx escrow messages and signed payment promises | `FuzzEscrowMsgSequence`, `FuzzFibreStoreKeyRoundTrip` in [x/fibre/keeper](../x/fibre/keeper/escrow_fuzz_test.go) | Up to 32 messages, time advances and injected bank failures over three fixed signers, each message after `ValidateBasic` on a cache context. Escrow balances must match pending withdrawals and a balance-tracking bank mock, coins are conserved and no promise settles twice. Validator certificates are outside this target. Index keys must round-trip; parsing short keys is excluded pending a fix. |
| 2 | `pkg/proof.ShareProof.Validate`, `RowProof.Validate`: client-supplied inclusion proofs | `FuzzShareProofValidate` in [pkg/proof](../pkg/proof/proof_fuzz_test.go) | Single-share and multi-row seeds, arbitrary proof/root bytes. `VerifyProof` is reached through `Validate`; nil row proofs are excluded as described below. |
| 2 | `pkg/proof.QueryTxInclusionProof`, `QueryShareInclusionProof`, `ParseNamespace`, `NewTxInclusionProof`: `abci_query` block bytes and path indices | `FuzzProofQueries` in [pkg/proof](../pkg/proof/querier_fuzz_test.go) | Arbitrary paths and request bytes must not panic. For blocks of up to 16 pool txs, returned proofs must validate against an independently computed data root and cover exactly the requested tx or share range. One input class is excluded pending a fix tracked in Linear. |
| 2 | `pkg/da.DataAvailabilityHeaderFromProto`: serialized row/column roots | `FuzzDataAvailabilityHeaderFromProto` in [pkg/da](../pkg/da/data_availability_header_fuzz_test.go) | Accepted headers validate, hash, respect square bounds and survive a proto round trip. |
| 2 | `app/grpc/gasestimation` gas price estimation: request priority and mempool fees, gas limits and sizes | `FuzzEstimateGasPrice` in [app/grpc/gasestimation](../app/grpc/gasestimation/gas_estimator_fuzz_test.go) | Mock mempool of up to 512 txs within the CheckTx domain (positive gas, fee below MaxInt64) and any priority value. Estimates must be finite, at least the network minimum and bounded by the highest fitting price plus the high priority adjustment. |
| 2 | `app/grpc/tx` `TxStatus`, `TxStatusBatch`: client tx ids and CometBFT RPC responses | `FuzzTxStatus` in [app/grpc/tx](../app/grpc/tx/server_fuzz_test.go) | Arbitrary id strings against a stub node that answers faithfully, fails, truncates, drops, adds, reorders or returns nil results. Malformed ids must be rejected with InvalidArgument and every batch entry must carry the status of its id. Nil, extra and reordered node results are excluded pending a fix. |
| 2 | `fibre/internal/grpc` scatter/view/pooled codecs: upload and download RPC bytes | `FuzzScatterMarshalParity`, `FuzzUploadViewsParity`, `FuzzDownloadViewsParity` in [fibre/internal/grpc](../fibre/internal/grpc) | Canonical protobuf parity, malformed wire inputs and contiguous/fragmented buffer decoding. |
| 2 | `fibre` shard codec and reader: stored/transferred shard bytes | `FuzzShardCodecRoundTrip`, `FuzzShardCodecReadNoPanic`, `FuzzShardReader` in [fibre](../fibre) | Codec round trips, arbitrary read bytes and reader behavior. |
| 2 | `app/errors.ParseInsufficientMinGasPrice`, `ParseExpectedSequence`, `ParseNonceMismatch`: error strings from nodes | `FuzzParseNodeErrors` in [app/errors](../app/errors/parse_fuzz_test.go) | Arbitrary strings wrapped as fee or sequence errors; gas prices must be finite and non-negative and sequences must appear in the string. One input class is excluded pending a fix. |
| 2 | `fibre/internal/tlsid.VerifyPeer`, `verifyCertAt`: peer TLS certificate DER and identity extension | `FuzzVerifyPeerCert`, `FuzzVerifyIdentityExtension` in [fibre/internal/tlsid](../fibre/internal/tlsid/tlsid_fuzz_test.go) | Mutated golden-vector certificates, plus well-formed certificates around fuzzed payloads, signatures, windows, keys and usages; accepted certificates must carry an endorsement valid only under the expected key and chain ID for the presented TLS key. Structured payloads must get the independently computed verdict. |
| 2 | `fibre/validator.QuorumPrefix`, `SignatureSet`, `Set.Assign`, `ShardMap.Verify`: validator sets and certificate signatures | `FuzzQuorumPrefix`, `FuzzAssign` in [fibre/validator](../fibre/validator/set_fuzz_test.go) | The quorum prefix agrees with the sequential `x/fibre` signature walk for absent, valid and invalid signatures; shard maps give each validator its distinct rows and `Verify` accepts exactly them. Voting powers span the CometBFT range; an `AssignedRows` overflow is excluded pending a fix. |
| 2 | `fibre.PaymentPromise` decoding, `Validate`, `SignBytes`, `Hash`: client payment promises | `FuzzPaymentPromiseUnmarshal`, `FuzzPaymentPromiseSignBytes` in [fibre](../fibre/payment_promise_fuzz_test.go) | Round trips, height and timestamp extremes, `Validate` agreement with `x/fibre/types.PaymentPromise.ValidateBasic`, and sign bytes that change with every signed field. Stateful escrow checks are outside this target. |
| 2 | `pkg/rsema1d.Verifier.Verify`, `VerifyShared`, `Reconstructor.Add`/`Reject`/`Reconstruct`, `rlc.Unmarshal`: rows, proofs and RLC values from upload peers and download servers | `FuzzVerifier`, `FuzzReconstructor` in [pkg/rsema1d](../pkg/rsema1d/verifier_fuzz_test.go); `FuzzUnmarshal` in [pkg/rsema1d/rlc](../pkg/rsema1d/rlc/marshal_fuzz_test.go) | Mutated proofs, rows, indices, RLC values and commitments, including poisoned row commitments; accepted rows must be the committed ones and reconstructions must re-encode to the commitment. Small K=N=4 shape; one input class is excluded pending a fix. |
| 3 | `pkg/rsema1d/merkle.RootFromProofs`: decoded shard proofs | `FuzzRootFromProofs` in [pkg/rsema1d/merkle](../pkg/rsema1d/merkle/proof_test.go) | Batch verification agrees with individual proofs under mutation. |
| 3 | `pkg/rsema1d/rlc.Compute`: validated rows and coefficients | `FuzzCompute` in [pkg/rsema1d/rlc](../pkg/rsema1d/rlc/poly_test.go) | Dispatched, pure-Go and scalar computations agree for bounded valid shapes. |
| 3 | `pkg/rsema1d/internal/sha256x2.Sum`: hash input buffers | `FuzzSum` in [pkg/rsema1d/internal/sha256x2](../pkg/rsema1d/internal/sha256x2/sha256x2_test.go) | Two-lane hashing agrees with the standard implementation. Assembly coverage depends on the runner architecture. |

## Remaining coverage and known exclusions

The next additions should extend priority 1 coverage to CheckTx rechecks, and stateful sequences across proposal
construction, validation and finalization. These need isolated committed state
and meaningful accept/reject oracles. The current mutation harnesses do not
provide a state-machine sequence fuzzer.

Fibre upload/download codec and payment promise coverage does not establish
coverage of shard cryptographic validation or escrow settlement. ZK-ISM message
servers and keeper state also need dedicated stateful harnesses. These are
follow-up surfaces; neither the nested-message guard nor transaction byte
mutation claims to cover them fully.
Fibre upload/download codec coverage does not establish coverage of payment
promise signature verification, shard cryptographic validation or escrow
settlement. ZK-ISM proof/key decoders also need dedicated cryptographic
harnesses. These are follow-up surfaces; neither the
promise signature verification or shard cryptographic validation. ZK-ISM proof/key decoders and module message servers also need dedicated
stateful or cryptographic harnesses. These are follow-up surfaces; neither the
nested-message guard nor transaction byte mutation claims to cover them fully.

Existing CheckTx/ProcessProposal harnesses exclude the known empty-message or
nil-fee ante panic class, and `FuzzValidateTxFee` excludes fees above MaxInt64.
The share proof harness excludes a nil row proof before
calling `ShareProof.Validate`. These are tracked in
[PROTOCO-3433](https://linear.app/celestia/issue/PROTOCO-3433), items 2, 4 and 5.
Keep exclusions narrow and remove each with the corresponding fix and regression
test. New crashes should receive an issue, a minimized reproducer and a regression
test with the fix; do not add a broad panic recovery to a harness.

## Running and maintaining coverage

`make test-fuzz` runs the targets registered in
[scripts/fuzz_targets.txt](../scripts/fuzz_targets.txt). The nightly
[test-nightly.yml](../.github/workflows/test-nightly.yml) job splits them
round-robin across parallel shards, so adding a target only needs a registry
line. `scripts/test_fuzz.sh --check` fails when a `Fuzz*` function is missing
from the registry or a registered target no longer exists; the nightly job runs
it first. To smoke-test one target:

```sh
go test ./app/ante -run='^$' -fuzz='^FuzzNestedMsgDecorator$' -fuzztime=60s -parallel=2 -fuzzminimizetime=100x
```

Normal `go test ./app/ante` also runs the targets' seed corpora. Cheap targets
use a bounded minimization budget so nightly runs keep exploring inputs. Update
this inventory and the registry together when adding or renaming a target.

Targets that interpret a byte slice as a script of operations should read it
with [test/util/fuzzutil](../test/util/fuzzutil). Give each operation a
fixed-width `Consumer.Record`, so editing one operation does not shift the bytes
of later ones, and lay out seeds with `fuzzutil.Records`.

## Nightly failures

A crashing input can describe an exploitable bug, so the nightly job writes each
target's output to a file and prints only pass/fail to the public log. When a
target fails on `main`, the job files a Linear issue in the Protocol team with
the output tail and the minimized crashing input, or comments on the open issue
for that target. The registry priority sets the issue priority: 1 is Urgent, 2
is High and 3 is Medium. Triage the issue, and keep crash details out of public
PRs and GitHub issues until a fix ships.
