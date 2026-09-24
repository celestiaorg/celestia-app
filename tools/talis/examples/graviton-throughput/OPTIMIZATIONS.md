# Benchmark optimization inventory

This inventory covers performance-related changes carried by the deployed branch relative to its common ancestor with `main`, `c399a7cc2b689048faf39e6f39009f16f5b5719b`. The deployed source is `83f2e81c5ab42c97039e0adf2d557e169ab90396`. These are changes to a dedicated benchmark branch, not a claim that they were deployed to Celestia mainnet. Commit links show the implementation and affected files; the final source and [reproduction settings](REPRODUCE.md) determine what was actually active.

## Inherited application and transport work

| Change | Implementation | Final-run status |
| --- | --- | --- |
| Cache transaction/PFF signatures, preverify in parallel, cache validator sets and check quorum signatures | [55b5d3ca2](https://github.com/celestiaorg/celestia-app/commit/55b5d3ca2) | Present in history; broad PFF validation bypass later removes checks from the measured application path. Do not attribute the final result to normal cached validation. |
| Order older payment promises first, preserving signer sequence order | [df534305f](https://github.com/celestiaorg/celestia-app/commit/df534305f) | Carried by the final branch; intended to improve propagation/cache readiness. |
| Consensus pacing and compact-block propagation: avoid per-part WAL fsync, forward before local recovery, parallelize mempool recovery and improve uncovered-part selection | [app integration df534305f](https://github.com/celestiaorg/celestia-app/commit/df534305f), [paired core change](https://github.com/celestiaorg/celestia-core/commit/aafdbb6eb34b25386895d90f1a36385cf4905c7b) | Inherited dependency work. Later pacing fixes remain relevant; use the final source's dependency pin, not the initial core commit alone. |
| Pace block time using TimeoutCommit and anchor the floor to the committed proposal timestamp | [1210b7d8c](https://github.com/celestiaorg/celestia-app/commit/1210b7d8c), [e089b4793](https://github.com/celestiaorg/celestia-app/commit/e089b4793), [3f7e23046](https://github.com/celestiaorg/celestia-app/commit/3f7e23046) | Final flags: delayed precommit 1 s, timeout commit 500 ms; measured blocks still varied under load. |
| Flat 1 utia Fibre transaction fee and settlement; align local admission with the network minimum | [df534305f](https://github.com/celestiaorg/celestia-app/commit/df534305f), [a41346ba0](https://github.com/celestiaorg/celestia-app/commit/a41346ba0) | Benchmark economics retained; deployed minimum gas price 0.000001 utia. The inherited Pebble default was overridden with goleveldb for this run. |
| Raise v11 PFF capacity and make the local proposal cap configurable | [7e47db30e](https://github.com/celestiaorg/celestia-app/commit/7e47db30e), [c5940c1d8](https://github.com/celestiaorg/celestia-app/commit/c5940c1d8) | Both caps were 2,000 in the final run; not 5,000. |
| Support 2 GiB blobs without changing padding | [02bc8a4f4](https://github.com/celestiaorg/celestia-app/commit/02bc8a4f4) | Actual raw payload 2,147,483,643 bytes. |
| Source-bound TCP paths and dual-NIC deployment support | [e275222ee](https://github.com/celestiaorg/celestia-app/commit/e275222ee), [56720882a](https://github.com/celestiaorg/celestia-app/commit/56720882a) | Peer transfers used secondary private interfaces; S3 used primary. |
| Bound concurrent client storage, expose pool/memory statistics and release upload storage before settlement | [3d0dc08e3](https://github.com/celestiaorg/celestia-app/commit/3d0dc08e3), [d99da77f1](https://github.com/celestiaorg/celestia-app/commit/d99da77f1) | Included in the client baseline. |

## Codec and Preston integration

| Change | Implementation | Final-run status |
| --- | --- | --- |
| Vendor Klauspost Reed-Solomon and enable the tested ARM GF16 NEON fork | [b44153e86](https://github.com/celestiaorg/celestia-app/commit/b44153e86), [2b389cfa4](https://github.com/celestiaorg/celestia-app/commit/2b389cfa4) | Local `go.mod` replacement selects `third_party/reedsolomon`. |
| ARM64 NEON multiply/multiply-XOR and FFT/IFFT butterflies, larger aligned chunks, fused eight-output multiply-XOR, tiled caller-side transpose | [fork integration eb0e771c7](https://github.com/celestiaorg/celestia-app/commit/eb0e771c7), [codec source](../../../../third_party/reedsolomon), [transpose source](../../../../pkg/rsema1d/rlc/transpose_arm64.s) | Retained. Workspace allocation support already existed upstream; it is not an original addition. |
| Object-storage adapter, local metadata/markers, remote shard readers and object lifecycle support | [eb0e771c7](https://github.com/celestiaorg/celestia-app/commit/eb0e771c7) | Preston work integrated onto the Fibre branch, then extended below. |
| Tx-sim preencoding and reusable payload buffers | [eb0e771c7](https://github.com/celestiaorg/celestia-app/commit/eb0e771c7) | Always enabled in the benchmark. Sustained throughput does not measure fresh encoding for every submission. |

## Storage and load-generator additions

| Change | Implementation | Final-run status |
| --- | --- | --- |
| Hash-first object layout and bucket migration routing | [855b7a624](https://github.com/celestiaorg/celestia-app/commit/855b7a624), [236f12fe9](https://github.com/celestiaorg/celestia-app/commit/236f12fe9), [a7ef9bcc9](https://github.com/celestiaorg/celestia-app/commit/a7ef9bcc9) | Evolved into packed keys with eight promise-derived groups per validator and four shared buckets. Earlier layouts are historical, not final settings. |
| Skip remote object HEAD checks on the upload path | [b97b27445](https://github.com/celestiaorg/celestia-app/commit/b97b27445) | Removes request overhead; this is not a claim that all hashing was removed. |
| Run expired-shard pruning every 24 hours | [b5102e1dc](https://github.com/celestiaorg/celestia-app/commit/b5102e1dc) | Reduces pruning frequency; distinct from onchain retention parameters. |
| Pack shards into indexed S3 objects; require full batches and raise upload admission | [25adfb6d7](https://github.com/celestiaorg/celestia-app/commit/25adfb6d7), [d8a89810c](https://github.com/celestiaorg/celestia-app/commit/d8a89810c) | 16 shards per pack, no partial timer flush, 128 GiB admission. Fewer PUTs with larger objects. |
| Bound aggregate S3 connections | [56ba2c1c1](https://github.com/celestiaorg/celestia-app/commit/56ba2c1c1) | 1,000 per process, not a measured AWS account quota. |
| Expand upload stripes, then replace them with exact-promise coordination | [855b7a624](https://github.com/celestiaorg/celestia-app/commit/855b7a624), [69bf8e8fe](https://github.com/celestiaorg/celestia-app/commit/69bf8e8fe) | Exact identity coordination supersedes 2,048 stripes, avoiding unrelated-promise collisions. |
| Release canceled queued shard references | [ebac6ced6](https://github.com/celestiaorg/celestia-app/commit/ebac6ced6) | Prevents canceled work from retaining queued buffers. |
| Add retry backoff and increase worker/concurrency defaults | [bfa4e151c](https://github.com/celestiaorg/celestia-app/commit/bfa4e151c), [edab8c4d1](https://github.com/celestiaorg/celestia-app/commit/edab8c4d1), [8e0f8e54e](https://github.com/celestiaorg/celestia-app/commit/8e0f8e54e) | 24 tx-sim workers, 100 verifiers, 200 streams, 256 gRPC connections; exact limits in reproduction guide. |
| Measure lock, verification, admission, persistence, TCP, stage and memory behavior | [853575f14](https://github.com/celestiaorg/celestia-app/commit/853575f14), [c124e449b](https://github.com/celestiaorg/celestia-app/commit/c124e449b), [186b0e935](https://github.com/celestiaorg/celestia-app/commit/186b0e935), [b72729387](https://github.com/celestiaorg/celestia-app/commit/b72729387) | Diagnostic improvements; not independently measured throughput gains. |

## Benchmark-only application/protocol changes

- [Broad PFF validation bypass, ba60f5497](https://github.com/celestiaorg/celestia-app/commit/ba60f5497) alters ante signature handling, CheckTx, proposal checks and message validation/execution paths. This is broader than a signature-cache optimization and changes the validation model. It is the final implementation, superseding our alternate bypass.
- [Remove the default validator row floor, 5130ec601](https://github.com/celestiaorg/celestia-app/commit/5130ec601) changes protocol parameter derivation and validator-set row allocation. It is not simply a deployment tuning flag.
- Fee/settlement reductions and the genesis funding described above are benchmark economics, not production throughput optimizations with unchanged behavior.

## Deployment changes without a new application algorithm

Second EBS disk for app state/Fibre metadata; local RPC per tx-sim; longer RPC deadline; funded uploader escrows; process memory budgets; secondary-interface private peer traffic with source policy routing; primary S3 gateway routing; `fq` scheduling and BBR; four selected S3 buckets; phased load ramp; expansion from 100 to 120 validators. [REPRODUCE.md](REPRODUCE.md) records the final values and topology.

The [experiment report](EXPERIMENT-REPORT.md) records intermediate outcomes and changes not deployed. Several optimizations were combined, so the evidence does not isolate an individual speedup for every row. To inspect the complete source delta, including non-performance changes, use `git diff c399a7cc2b689048faf39e6f39009f16f5b5719b 83f2e81c5ab42c97039e0adf2d557e169ab90396`; do not substitute a moving `main` reference.
