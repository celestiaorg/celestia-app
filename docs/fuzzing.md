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
existing targets that were missing from the nightly script. All **23 targets**
are now registered in [scripts/fuzz_targets.txt](../scripts/fuzz_targets.txt).

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
| 1 | `app.ProcessProposal`: proposer transaction bytes | `FuzzProcessProposalMutatedTxs` in [app/test](../app/test/process_proposal_fuzz_test.go) | Mutates a prepared proposal; detects recovered panics and requires unchanged proposals to be accepted. Header root and square size remain fixed. |
| 1 | `app/ante.NestedMsgDecorator.AnteHandle`: decoded authz/governance containers | `FuzzNestedMsgDecorator` in [app/ante](../app/ante/nested_msg_fuzz_test.go) | Varies message type, position, count, wrapper and simulation mode; forbidden messages must never reach the next handler. Exercises the guard directly, independently of signature/fee rejection earlier in the ante chain. |
| 1 | `app/ante.verifyMinFee`, `getTxPriority`: transaction gas/fees | `FuzzAnteFeeArithmetic` in [app/ante](../app/ante/fee_fuzz_test.go) | Uses a `math/big` oracle for rounded minimum fees and overflow-safe priority, including zero gas and values above MaxInt64. Random fees and prices span uint64; keeper-backed fee deduction is outside this target. |
| 1 | `app.pffDecodeCandidate`: raw transaction classification | `FuzzPFFDecodeCandidateParity` in [app](../app/pff_decode_candidate_fuzz_test.go) | Fast-path classification agrees with the SDK and fibre decoders. |
| 1 | `app.countICAPacketMsgs`: IBC packet contents | `FuzzCountICAPacketMsgs` in [app](../app/msg_count_test.go) | Fuzzes ICA message counting from packet data. |
| 1 | `x/blob/types.ValidateBlobTx`, `ValidateBlobTxSkipCommitment`: blob transaction envelopes | `FuzzValidateBlobTx` in [x/blob/types](../x/blob/types/blob_tx_fuzz_test.go) | Cached validation must agree with full validation except for the skipped commitment check. |
| 1 | `x/blob/types.MsgPayForBlobs.ValidateBasic`, gas estimation: message fields | `FuzzMsgPayForBlobsValidateBasic`, `FuzzPFBGasEstimation` in [x/blob/types](../x/blob/types) | Inconsistent vector lengths and share versions; gas helpers run after their validation preconditions. |
| 1 | `x/fibre/types.ParsePayForFibreTx`, `TryParseFibreTx`, `ClassifyTxs`: transaction bytes | `FuzzParsePayForFibreTx` in [x/fibre/types](../x/fibre/types/classified_tx_fuzz_test.go) | Parser/classifier agreement and system blob consistency; decoded PFF basic validation, signature cache keys and gas estimates. |
| 2 | `pkg/proof.ShareProof.Validate`, `RowProof.Validate`: client-supplied inclusion proofs | `FuzzShareProofValidate` in [pkg/proof](../pkg/proof/proof_fuzz_test.go) | Single-share and multi-row seeds, arbitrary proof/root bytes. `VerifyProof` is reached through `Validate`; nil row proofs are excluded as described below. |
| 2 | `pkg/proof.QueryTxInclusionProof`, `QueryShareInclusionProof`, `ParseNamespace`, `NewTxInclusionProof`: `abci_query` block bytes and path indices | `FuzzProofQueries` in [pkg/proof](../pkg/proof/querier_fuzz_test.go) | Arbitrary paths and request bytes must not panic. For blocks of up to 16 pool txs, returned proofs must validate against an independently computed data root and cover exactly the requested tx or share range. One input class is excluded pending a fix tracked in Linear. |
| 2 | `pkg/da.DataAvailabilityHeaderFromProto`: serialized row/column roots | `FuzzDataAvailabilityHeaderFromProto` in [pkg/da](../pkg/da/data_availability_header_fuzz_test.go) | Accepted headers validate, hash, respect square bounds and survive a proto round trip. |
| 2 | `fibre/internal/grpc` scatter/view/pooled codecs: upload and download RPC bytes | `FuzzScatterMarshalParity`, `FuzzUploadViewsParity`, `FuzzDownloadViewsParity` in [fibre/internal/grpc](../fibre/internal/grpc) | Canonical protobuf parity, malformed wire inputs and contiguous/fragmented buffer decoding. |
| 2 | `fibre` shard codec and reader: stored/transferred shard bytes | `FuzzShardCodecRoundTrip`, `FuzzShardCodecReadNoPanic`, `FuzzShardReader` in [fibre](../fibre) | Codec round trips, arbitrary read bytes and reader behavior. |
| 3 | `pkg/rsema1d/merkle.RootFromProofs`: decoded shard proofs | `FuzzRootFromProofs` in [pkg/rsema1d/merkle](../pkg/rsema1d/merkle/proof_test.go) | Batch verification agrees with individual proofs under mutation. |
| 3 | `pkg/rsema1d/rlc.Compute`: validated rows and coefficients | `FuzzCompute` in [pkg/rsema1d/rlc](../pkg/rsema1d/rlc/poly_test.go) | Dispatched, pure-Go and scalar computations agree for bounded valid shapes. |
| 3 | `pkg/rsema1d/internal/sha256x2.Sum`: hash input buffers | `FuzzSum` in [pkg/rsema1d/internal/sha256x2](../pkg/rsema1d/internal/sha256x2/sha256x2_test.go) | Two-lane hashing agrees with the standard implementation. Assembly coverage depends on the runner architecture. |

## Remaining coverage and known exclusions

The next additions should extend priority 1 coverage to malformed mempool batches
in PrepareProposal, CheckTx rechecks, and stateful sequences across proposal
construction, validation and finalization. These need isolated committed state
and meaningful accept/reject oracles. The current mutation harnesses do not
provide a state-machine sequence fuzzer.

Fibre upload/download codec coverage does not establish coverage of payment
promise signature verification, shard cryptographic validation or escrow
settlement. ZK-ISM proof/key decoders and module message servers also need dedicated
stateful or cryptographic harnesses. These are follow-up surfaces; neither the
nested-message guard nor transaction byte mutation claims to cover them fully.

Existing CheckTx/ProcessProposal harnesses exclude the known empty-message or
nil-fee ante panic class. The share proof harness excludes a nil row proof before
calling `ShareProof.Validate`. These are tracked in
[PROTOCO-3433](https://linear.app/celestia/issue/PROTOCO-3433), items 4 and 5.
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

## Nightly failures

A crashing input can describe an exploitable bug, so the nightly job writes each
target's output to a file and prints only pass/fail to the public log. When a
target fails on `main`, the job files a Linear issue in the Protocol team with
the output tail and the minimized crashing input, or comments on the open issue
for that target. The registry priority sets the issue priority: 1 is Urgent, 2
is High and 3 is Medium. Triage the issue, and keep crash details out of public
PRs and GitHub issues until a fix ships.
