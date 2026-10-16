# Coverage reports and staged checks

[Coverage CI](../.github/workflows/test-coverage.yml) runs the full serial suite
on main, PRs targeting main, and the merge queue. It preserves the corrected
[cross-package instrumentation and production scope](../scripts/README.md#test_coversh).
Tests in other packages credit the production code they execute. Generated code,
test utilities, and developer tools remain excluded.

[Codecov configuration](../.github/codecov.yml) reports PrepareProposal and
ProcessProposal separately, plus square building, ante, application lifecycle,
DA, inclusion, proofs, NMT wrapping, erasure coding, signature caching, version
constants, the multiplexer, and each application module. Component paths can
overlap; they are views, not partitions of the production total. Proposal
components include helpers in their respective files.

CI also checks the **actual handler functions** with `go tool cover -func`.
Missing instrumentation or zero executed statements in either handler fails the
job even if helpers or the overall app have coverage. The profile and handler
summary are saved as the `coverage` artifact. Run locally with:

```sh
make test-coverage
bash scripts/check_proposal_coverage.sh
```

## Stages

1. **Non-regression:** the production total and every component compare against
   the base report (`target: auto`, zero allowed decrease). Missing reports fail
   the checks. Main uploads supply the corrected baseline. The 80% production,
   95% proposal, and 90% other-component goals are informational initially.
2. **Ratchet:** as existing or new tests raise coverage, `auto` uses the improved
   base on subsequent PRs. Review the full profile before promoting a goal;
   remove `informational: true` for a component once its goal is sustainable.
   Raise the production check to 80% the same way. Keep non-regression checks.
3. **Maintain:** require the coverage job and non-regression statuses in repository
   rules when adopting these as merge requirements. A workflow PR does not
   change GitHub branch protection. Add new consensus components to the config
   when their code is introduced.

Codecov percentages follow Codecov report semantics. Native Go percentages are
statement-weighted; the handler summary records those independently. Neither
metric proves branch coverage. Do not add unreachable store/dependency failures
just to meet a percentage. Keep defensive gaps documented when promoting goals.

## Proposal accept/reject invariant matrix

These existing tests are credited by the full coverage suite; a high percentage
does not replace their assertions. Update this matrix when validity rules change.

| Invariant | Accept / construction evidence | Reject / failure evidence |
| --- | --- | --- |
| Square, data root, and ordering agree | [TestPrepareProposalSquareAndRoot](../app/test/prepare_proposal_handler_test.go), [TestPrepareProposalValidConstruction](../app/test/prepare_proposal_test.go) | [TestProcessProposal](../app/test/process_proposal_test.go), [TestProcessProposalRejectsDataRootComputationFailure](../app/test/process_proposal_failure_test.go) |
| Malformed transactions and blobs cannot enter an accepted proposal | [TestPrepareProposalFiltering](../app/test/prepare_proposal_test.go) | [TestPrepareProposalDropsTxsProcessProposalRejects](../app/test/prepare_proposal_test.go), [TestProcessProposal_ProposalWithInconsistentBlobTxFails](../app/test/process_proposal_test.go) |
| Protocol limits bound proposal resources | [TestPrepareProposalRespectsCoreProtoByteBudget](../app/test/prepare_proposal_test.go) | [TestProcessProposalCappingNumberOfMessages](../app/test/process_proposal_test.go), [TestProcessProposalCappingPayForFibreMessages](../app/test/process_proposal_fibre_test.go) |
| Fibre validation respects escrow, sequence, and gas | [TestProcessProposalWithPayForFibre](../app/test/process_proposal_fibre_test.go) | [TestProcessProposalPayForFibreStatefulChecks](../app/test/process_proposal_fibre_test.go), [TestProcessProposalPayForFibreDoubleSpend](../app/test/process_proposal_fibre_test.go), [TestProcessProposalChargesTxSizeGas](../app/test/process_proposal_fibre_test.go) |
| Proposal state is isolated and lifecycle effects agree with finalization | [TestNewProposalContext, TestProposalBeginBlockParity](../app/test/proposal_context_test.go) | [TestProcessProposalBranchFailures, TestProcessProposalKeepsFailedEscrowDeposit](../app/test/process_proposal_failure_test.go), [TestPrepareProposalHandlerFailures](../app/test/prepare_proposal_handler_test.go) |
| A proposal cache hit still validates transaction content | [TestProcessProposalReusesOwnProposal](../app/test/prepare_proposal_test.go) | [TestProposalCacheMissOnDifferentTxs, TestProposalCacheMissOnMutatedTxs](../app/proposal_cache_test.go) |
