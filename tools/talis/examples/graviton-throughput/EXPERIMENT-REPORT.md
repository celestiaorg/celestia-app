# Graviton benchmark: 383 GB/s average, 461 GB/s peak over 60 seconds

See the [optimization inventory](OPTIMIZATIONS.md) for inherited work, benchmark additions, source links and final deployment status.

## Result and meaning

120 colocated validator/Fibre/tx-sim hosts sustained **383.295 decimal GB/s** of raw payload represented by **25,597 unique transaction hashes with indexed code-zero PFF receipts**, over **143.412 seconds**. This represents 54,969,138,809,871 declared raw bytes, with 24 workers per host / 2,880 workers total.

| Measurement | Raw throughput | Measurement duration | Unique confirmed PFFs |
| --- | ---: | --- | ---: |
| Full-load average | **383.3 GB/s** | 143.412 s | 25,597 |
| Maximum rolling 60-second average | **461.1 GB/s** | 60 s | 12,882 |
| Maximum rolling 30-second average | **534.2 GB/s** | 30 s | 7,463 |

These maxima use deduplicated, successfully indexed PFF bytes and complete rolling windows within the 120-producer period. They describe bursts of confirmed payload, not instantaneous network/S3 throughput. The window length matters: the full-load average remains the sustained-window result. [Window calculations and per-block aggregates](throughput-windows.json) preserve the method and counts.

This was a synthetic throughput experiment: the 2 GiB-minus-five-byte payload was preencoded and reused. The branch deliberately disabled PFF validation and removed a row-floor check. Thus the result is neither independently generated data ingestion at that rate nor performance under normal production validation. Deduplication is by transaction hash. A subsequent identity audit found zero cross-hash duplicate payment promises; blob contents remained deliberately reused.

The final window contained 27,808 PFF block appearances, but those are not the numerator: identical transactions appeared in multiple blocks. Across the complete run, 44,686 appearances reduced to 42,204 unique successfully indexed transactions, with 2,482 repeated appearances. Saved contemporary scans and all 42,204 indexed raw transactions/code-zero receipts (approximately 249.6 MB compressed) support the accounting. Historical block-results queries were unavailable, and all 357 measured blocks had been pruned before retrospective raw-block archival. Do not claim a complete cryptographically re-verifiable block archive.

## What changed

The original setup suffered from S3 failures, too-small RPC deadlines, partially completed shard uploads and exhausted escrow. Improvements combined application, storage and infrastructure work:

1. Moved app state and Fibre metadata to the second EBS disk and made tx-sim use its own local app RPC.
2. Increased account funding, stream limits and timeouts, then lowered concurrency to diagnose S3 failures. Added error backoff rather than repeatedly hammering failed work.
3. Tried hash-first keys, more buckets and different bucket assignments. Some buckets were consistently healthier, but isolated lower-load tests did not reproduce a permanent capacity difference. Prefix count is not a measured count of physical S3 partitions.
4. Packed 16 shards into each S3 object to reduce PUT request rate. Removed the 200 ms partial flush after measured batches averaged only 3.1 shards. Increased upload admission from 16 to 128 GiB.
5. Split peer uploads onto secondary NICs while keeping S3 on primary NICs; changed primary multiqueue leaf scheduling to `fq`. Retained source binding and source policy routing so this separation was real.
6. Added lock/admission/batch/S3 timing, first expanded hash stripes to 2,048, then replaced striped upload locking with exact-promise coordination and moved remote upload work outside shared lock sections. Increased workers from 20 to 24 after admission ceased to dominate.
7. Identified consensus proposal preparation delays, then used an explicitly benchmark-only PFF-validation bypass. This substantially improved observed block progress. The 5,000-PFF experiment was canceled; final consensus and soft cap were both 2,000.
8. Scaled to 120 hosts, rebuilt a fresh chain with preserved parameters and funding, corrected chain ID/key naming, then aligned local minimum gas price with the onchain fee setting.

Several changes landed together. The sequence provides operational evidence, not controlled attribution of a percentage gain to each individual optimization.

## Experiment progression

All throughput figures below are decimal GB/s of raw PFF bytes unless marked otherwise. Windows and deduplication methods varied; the final run has the strongest transaction-level reconciliation. Earlier figures should not be compared as if they were identical controlled tests.

| Experiment | Setup / finding | Recorded result |
|---|---|---|
| 1 | 100 hosts, 32 workers, 15 s RPC deadline, initial storage setup | 1.267 GB/s over common 54.218 s; 39 PFF in the reported result; incomplete shard work dominated |
| 2 | 16 workers, 60 s RPC, app/metadata on secondary disk | 10.200 GB/s common window; 286 PFF total; S3 failures and ENA throttling |
| 3 | 8 workers, 30 verifiers | 404 PFF total, no PFF in all-host overlap; 62.79% logical store failure |
| 4 | Single-bucket ramp, 30 verifiers / 8 workers | Approximately 70.33 GB/s at 10 producers and 92.54 at 20; error rate rose with load |
| 5 | Ten hash-first buckets | Approximately 84.93 GB/s at 100; PUT 500s plus depleted escrow confounded result |
| 6 | Funded accounts, 20 workers / 100 verifiers | 59.923 GB/s during 66.263 s all-host overlap; 33.9% logical store failure |
| 7 | 100 dedicated buckets, 2,048 stripes, 24 h pruning | Zero PFF in actual full overlap; approximately 79.7% store failure; 649,229 PUT 503 and 123,013 PUT 500 errors in audited window |
| 8 | Full promise hash first | Approximately 79.8% logical storage failure; healthy and failing buckets still differed |
| 9 | Packed uploads, four healthy buckets, 1,000 connection cap, backoff | 128.902 GB/s reported; physical S3 failure near 0.10%; average batch only 3.1 shards; admission/queueing became visible |
| 10 | Full batches, 128 GiB admission, secondary NIC | 213.9 GB/s reported; hash-lock waits and S3 completion dominated |
| 11 | Lock change, 24 workers, `fq`, secondary-only listener | Chain stalled around full 2,000-PFF proposals; proposal preparation exceeded consensus timeout |
| 12 | Coordinated benchmark signature bypass | 316.8 GB/s reported over 151.5 s; median blocks 1.63 s; peak 456 PFF/block |
| 13 | First fresh 120-host chain attempt | Invalid too-long chain ID and uploader key-name mismatch; corrected rather than counted as a result |
| 14 | Corrected short chain, 120 hosts | Local minimum gas-price mismatch limited submissions; fixed before final run |
| 15 | 120 hosts, final binaries and local fee correction | **383.295 GB/s**, 25,597 unique indexed PFFs / 143.412 s |

Preserved summaries are in `evidence/final/history/`; detailed original artifacts are under `load/`. Do not substitute successful shard-store bytes or very short transition-stage peaks for sustained PFF-confirmed throughput. For example, the final ramp's 100-producer transition measured 545 GB/s over only 8.95 seconds; it is not the headline result.

## Final topology and settings

120 AWS c8gn.48xlarge ARM64 hosts in eu-west-1c, each with 192 CPUs, approximately 371 GiB RAM, two network cards and a second 100 GiB EBS data disk. Tx-sim → Fibre uses secondary private IPs on both ends; S3, app peers and monitoring remain on primary. Four S3 buckets are shared round-robin, 30 hosts each; each validator has eight packed key groups. Each S3 pack contains 16 shards, with a 128 GiB admission budget and a 1,000-connection transport limit. Each Fibre server has 100 verification workers and each producer 24 tx-sim workers.

The exact final binaries, genesis, chain parameters, memory caps, source pins, build caveats and ordered setup procedure are in [REPRODUCE.md](REPRODUCE.md). That document supersedes historical 100-host configuration examples.

## Remaining bottlenecks and uncertainty

- **App confirmation became significant at 120 producers.** Final matched confirmation observation latency was median 8.44 s / p95 55.53 s, compared with upload-plus-broadcast median 14.59 s / p95 21.23 s. Full-load block intervals were median 1.56 s / p95 10.30 s / max 11.95 s. Three blocks reached 2,000 PFFs. Configured timing alone does not prove stable 1.5-second blocks.
- Confirmation timings include observer queue and one-second polling, not just app RPC response time. The tracking queue can drop observations, so matched successful confirmations are a biased sample. Transient status RPC errors were not fully logged.
- Final measured network totals over a conservative 135-second interval were approximately 1,055 GB/s primary TX to S3, 1,179 GB/s secondary TX and 1,173 GB/s secondary RX fleet-wide. These are encoded/network traffic, not raw confirmed throughput. Fleet averages do not exclude individual-card microbursts.
- Earlier secondary traffic showed burst throttling despite unsaturated averages. Validator 85 repeatedly reset its secondary ENA interface and produced peer/TLS failures; this is a concrete hardware/driver/path outlier, not a reason to tune every host blindly.
- Earlier full-batch runs did not hit RAM caps: Fibre peaked around 248–256 GiB below 272/288 GiB thresholds, with approximately 8 microseconds admission wait in the initial full-batch run after the 128 GiB change. At 24 workers in the later 24-worker run, the 128 GiB admission budget did fill and mean admission wait was about 0.875 seconds. Thus a higher budget did not eliminate admission pressure at every subsequent load. These are historical samples, not substituted final-run utilization claims.
- Uploads continue after quorum to deliver all shards. Late already-processed rejections and packed timeouts can still produce considerable work and log volume. A successful quorum is not a full-storage completion counter.
- The S3 connection cap was not observed saturated; previous samples had approximately 70 active connections versus 1,000 allowed. Raising a cap alone would not create ready work.
- S3 503s improved after batching/bucket/layout changes, but this does not establish a universal 3,500-RPS account or bucket limit. Physical S3 requests and logical 16-shard failures must be counted separately.

## Codec work retained

The branch replaces upstream Klauspost Reed-Solomon through `go.mod` with `third_party/reedsolomon`: ARM64 NEON GF(2^16) multiply/multiply-XOR and FFT/IFFT butterflies, lookup/dispatch support, larger aligned NEON chunks, and a fused eight-output multiply-XOR fast path. Caller-side RLC adds tiled NEON transpose. Preencoding and buffer reuse are separate application optimizations. Workspace allocation support already existed upstream and is not an original optimization here.

Sources: [fork](https://github.com/celestiaorg/celestia-app/tree/integrate/fibrrrrrr-preston-graviton/third_party/reedsolomon), [NEON kernels](https://github.com/celestiaorg/celestia-app/blob/integrate/fibrrrrrr-preston-graviton/third_party/reedsolomon/galois_leopard_arm64.s), [caller transpose](https://github.com/celestiaorg/celestia-app/blob/integrate/fibrrrrrr-preston-graviton/pkg/rsema1d/rlc/transpose_arm64.s).

## What was not part of the final result

No 8 GiB blobs (current size encoding blocks that change), no dual-NIC S3, no 5,000-PFF limit, no generic TCP-buffer/ring enlargement, no stale signing-state rollback. The temporary alternative consensus repair and our alternate signature bypass are not the final branch implementation. The fresh-chain choice sacrificed old height continuity while preserving declared chain parameters and retaining old local state separately.

After the final run, tx-sims were stopped/remasked and load gates removed; app/Fibre remained available for capture. Stopped load does not stop EC2/EBS/S3 billing. Retained S3 objects and disk state require deliberate operator cleanup after archive verification.
