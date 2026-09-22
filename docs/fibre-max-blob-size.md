# Fibre maximum blob size: 128 MiB to 10x/20x

Status: research report and benchmark plan for PROTOCO-2829.

## Executive summary

The 128 MiB limit is a Fibre v0 implementation constant, not a limit derived
from RSEMA1D, the Fibre commitment, or celestia-app consensus. It first appeared
as `128 * 1024 * 1024` in the initial Fibre client implementation (commit
`a84a072520049f83cb7005bc6332380f59bb35b6`) and was moved unchanged into
`ProtocolParams` by commit `2b97183e796826802ffb5c222410d421de0c223a`.
Neither commit explains why 128 MiB was selected. The best-supported historical
explanation is that 128 MiB matched the approximate blob capacity of Celestia
v7's 512x512 original data square. It was therefore a familiar, already-tested
large-blob target rather than a mathematical ceiling. This is an inference,
not a recorded design decision.

Increasing the limit while keeping Fibre v0's 4096 original rows and 1:3
original-to-parity ratio is mathematically valid at both 10x (1.25 GiB) and 20x
(2.5 GiB). The wire fields also fit: both the blob header and PaymentPromise use
`uint32` sizes. The first hard boundary is 4 GiB, not 128 MiB.

The current implementation is not operationally safe at those sizes:

- Encoding has a peak working set of about 12x the original blob: roughly
  15 GiB for 10x and 30 GiB for 20x, before process and concurrent-upload
  overhead.
- Fibre uses unary gRPC. The server buffers a complete request before the
  handler runs and then materializes it into a contiguous buffer for protobuf
  decoding. A validator's shard scales linearly with the blob.
- The gRPC server's connection/stream caps were chosen around a 132 MiB maximum
  request. Leaving them unchanged makes the theoretical receive-memory envelope
  about 10x/20x larger.
- Client upload and download timeouts, encoder concurrency, reader concurrency,
  storage budgets, disk capacity, and network timeouts were all tuned around
  128 MiB.
- Changing the meaning of Fibre blob version 0 creates a mixed-binary network
  split: old servers reject the larger row size and old readers reject it on
  download. A production rollout needs a new blob version or an enforced,
  coordinated protocol-profile activation. A private benchmark network can
  mutate v0 as long as every participant uses the same build.

Recommendation: use a benchmark-only protocol profile for the Cloudflare demo,
start with 10x and upload concurrency 1, and provision at least 32 GiB for the
encoder. Treat 20x as a 64 GiB encoder experiment. For a production design,
first stream/chunk shard RPCs and reduce the codec's 8x work allocation; then
introduce a versioned parameter set.

## Where 128 MiB is enforced

`fibre.DefaultProtocolParams.MaxBlobSize` is the root constant. It derives:

- `BlobConfig.MaxDataSize`, which rejects larger client payloads;
- `BlobConfig.MaxRowSize`, which rejects larger rows at servers and readers;
- client/server gRPC maximum message sizes;
- default escrow low/high watermarks.

The app-side `PaymentPromise.blob_size` is a `uint32`. The app validates that it
is positive but does not independently cap it at 128 MiB. The consensus block
contains only the compact PayForFibre transaction and system-blob metadata, not
the Fibre payload, so raising the Fibre payload ceiling does not raise
`consensus.block.MaxBytes`.

## Parameter comparison

The calculations below retain Fibre v0's `K=4096`, `N=12288`, 64-byte row
alignment, and 1:3 encoding.

| Property | Current | 10x | 20x |
| --- | ---: | ---: | ---: |
| Maximum upload size | 128 MiB | 1,280 MiB | 2,560 MiB |
| Row size | 32 KiB | 320 KiB | 640 KiB |
| Approx. encoder peak (12x) | 1.5 GiB | 15 GiB | 30 GiB |
| Worst-case one-validator shard | 129.8 MiB | 1.252 GiB | 2.502 GiB |
| Configured max unary gRPC message (+2%) | 132.4 MiB | 1.277 GiB | 2.552 GiB |
| PFF data charge | 23.690M utia | 231.050M utia | 461.450M utia |
| Fixed 650k share of data charge | 2.74% | 0.28% | 0.14% |

The worst-case shard corresponds to a validator assigned all 4096 rows. With
100 equal-stake validators, the 100-bit unique-decoding floor assigns about 148
rows per validator; row payload alone is approximately 4.6 MiB, 46.3 MiB, and
92.5 MiB respectively. Real stake is uneven, so large validators receive larger
shards.

## What breaks or becomes unsafe

### Encoding and client memory

`ProtocolParams.CodecWorkRows()` is 32768 rows (8x K). Together with one original
region and three parity regions, an encode can hold about 12x the original data.
Most large slabs use mmap, which avoids Go heap-pacer amplification but does not
reduce resident-memory demand. Concurrent encodes multiply this working set.

At 10x, a 16 GiB machine cannot safely encode one maximum blob. At 20x, a
32 GiB machine has no headroom. Existing benchmark documentation already marks
four concurrent 128 MiB uploads as memory-bound and eight concurrent downloads
on a 64 GiB reader as the safe upper bound.

### Networking and gRPC receive memory

Upload requests use a scatter-gather encoder on the client, avoiding a second
contiguous request copy. The server still buffers the whole gRPC message and
`pooledCodec.Unmarshal` calls `data.Materialize()` before gogoproto decoding.
Download responses use the ordinary contiguous marshal path as well.

The server currently permits 16 connections times 13 concurrent streams. Its
code documents a worst-case receive envelope of about 27 GiB at the current
~132 MiB maximum request. The same arithmetic is roughly 265 GiB at 10x and
531 GiB at 20x. These are adversarial/theoretical maxima, but they show that
simply raising `MaxRecvMsgSize` invalidates the current DoS/memory bound.

The gRPC framing limit is uint32 and both proposed message maxima remain below
4 GiB, so framing itself is not yet the blocker. Contiguous allocation, memory
pressure, retransmission cost, HTTP/2 flow control, and the fixed 15-second
per-peer RPC timeout are the practical blockers.

### Verification

Validator verification cost and bandwidth scale with assigned row bytes. Row
count, Merkle depth, RLC-vector length, and proof count remain constant, so the
cryptographic shape remains valid. Verification is capped by
`UploadVerifyWorkers`, but messages consume receive memory before a verifier is
acquired. The concurrency cap therefore does not protect the largest allocation.

### Download and reconstruction

The downloader allocates a K-row reconstruction slab equal to the padded upload
size, retains decoded shard responses while adding them, and may allocate the
codec's reconstruction workspace. Reader concurrency must be reduced in inverse
proportion to the blob factor unless reconstruction memory is redesigned.

### Storage and retention

The custom shard-file codec supports these row sizes: individual row lengths are
capped at 1 GiB, while the proposed rows are 320/640 KiB. Disk writes are
streamed, which is favorable. However, per-node occupancy, disk throughput, and
retained bytes all scale with assigned shard size. The default full-stake budget
is 2 TiB over a four-hour retention window; at 20x, a full-stake budget holds
only about 819 maximum blobs before overhead, versus about 16,384 today.

For load tests, storage budget and physical NVMe capacity must be increased or
retention reduced. A rejected upload returns `ResourceExhausted`, so a run may
look network-limited when it is actually budget-limited.

### Settlement, gas, and escrow

The payment field and calculations remain numerically safe. Gas/payment is
`650,000 + 45,000 * ceil(blob_size / 256 KiB)`, producing 231.05M utia at 10x
and 461.45M utia at 20x. Consensus `MaxGas` defaults to unlimited. Escrow funding
for the load generator must scale accordingly; otherwise uploads can succeed
off-chain and settlement can fail or wait for auto-funding.

The benefit is real but limited to fixed per-blob overhead. The 650k fixed data
charge falls from 2.74% of the charge at 128 MiB to 0.28%/0.14%. PayForFibre
transaction bytes, signature verification, and per-blob control-plane work are
also amortized by 10x/20x.

### Compatibility and rollout

Blob version 0 currently implies the full coding configuration, including the
maximum row size. An old server rejects a large v0 upload; an old reader rejects
the corresponding downloaded rows. The on-chain app cannot tell whether Fibre
servers run matching parameters. This is the same deployment-consistency
problem tracked in celestia-app issue 7475.

For the benchmark, use one pinned binary/profile everywhere. For production,
define a new blob version with explicit activation and keep v0 decoding intact.

### External API/tooling

Any HTTP/JSON API that embeds the blob adds base64 and JSON overhead and may
have a much lower request cap. celestia-node issue 5009 documents a ~16 MiB RPC
cap that already rejects valid current Fibre payloads. The Fibre benchmark tool
uses the native client, but Cloudflare-facing test orchestration must avoid a
JSON gateway for multi-GiB payloads.

## Benchmark plan

The prototype should expose a benchmark-only maximum-blob profile while keeping
128 MiB as the default. Measure:

1. Blob encoding at 128, 256, 512, and (on large hosts) 1,280/2,560 MiB:
   latency, useful MiB/s, RSS, mmap footprint, CPU, and allocations.
2. Upload with one encoder and representative validator stake distributions:
   end-to-end latency, aggregate wire bytes, server receive RSS, verify time,
   store throughput, and signature threshold time.
3. Download/reconstruction at concurrency 1, then cautiously increase:
   end-to-end latency, aggregate egress, reader RSS, and reconstruction time.
4. Sustained load for at least one retention/prune cycle:
   accepted/rejected uploads, occupancy, NVMe throughput/latency, network
   goodput, and process OOM/restart behavior.

For the headline figure, report both useful payload throughput and total network
throughput. The 1:3 code plus replication/assignment means those are deliberately
different; presenting only wire throughput would overstate user data capacity.

## Preliminary local benchmark results

Hardware: Apple M4, 16 GiB RAM, Darwin arm64. Each result is one operation, so
these are directional prototype numbers rather than publication-quality samples.

Real RSEMA1D blob encoding:

| Maximum blob | Time | Useful throughput | Approx. codec working set |
| --- | ---: | ---: | ---: |
| 128 MiB (1x) | 1.402 s | 95.76 MB/s | 1.5 GiB |
| 256 MiB (2x) | 2.896 s | 92.69 MB/s | 3 GiB |
| 512 MiB (4x) | 5.739 s | 93.55 MB/s | 6 GiB |

Encoding time is linear through 4x. Holding the observed throughput constant
projects approximately 14 seconds for 10x and 29 seconds for 20x on this CPU,
but those projections exclude the likely memory-pressure penalty. The 10x and
20x real encodes were intentionally not run locally: their approximate 15/30 GiB
working sets leave no safe headroom on a 16 GiB host. Go's `B/op` result is not
a resident-memory measurement because the large row pools are mmap-backed.

The equal-stake shard-store benchmark (148 rows, one operation, page-cache hot)
completed for every target profile:

| Maximum blob | Validator shard payload | Store time | Reported throughput |
| --- | ---: | ---: | ---: |
| 128 MiB (1x) | 4.75 MiB | 0.94 ms | 5.28 GB/s |
| 1,280 MiB (10x) | 46.38 MiB | 7.54 ms | 6.45 GB/s |
| 2,560 MiB (20x) | 92.63 MiB | 15.13 ms | 6.42 GB/s |

This confirms that the streaming shard-file codec accepts the proposed row
sizes and scales linearly. The absolute throughput is local page-cache/NVMe-path
performance and must not be used as the Cloudflare headline. A sustained remote
run with fsync/device telemetry is required for that figure.

The 10x profile should not be expected to produce 10x throughput from this
benchmark. It increases the amount of data in one `Store.Put` operation, but it
does not add parallelism or increase the host's memory or storage bandwidth. The
measured shard was 9.76x larger while the operation took 8.02x longer, producing
the observed 1.22x throughput improvement. That smaller gain comes from
amortizing fixed per-operation work and issuing larger sequential writes. The
10x and 20x results then converge near 6.4 GB/s, which indicates saturation of
the local memory/page-cache path rather than a Fibre network throughput limit.

Reproduction:

```sh
FIBRE_LARGE_BLOB_BENCH_FACTORS=1,2,4 go test ./fibre \
  -run='^$' -bench='^BenchmarkLargeBlobEncode$' \
  -benchtime=1x -benchmem -timeout=20m

go test ./fibre -run='^$' \
  -bench='^BenchmarkLargeBlobEqualStakeShardStore$' \
  -benchtime=1x -benchmem -timeout=20m
```

The prototype also wires the profile through Talis. A 10x run starts with:

```sh
talis start-fibre --experimental-max-blob-size-mib 1280

talis fibre-txsim --on-encoders \
  --experimental-max-blob-size-mib 1280 \
  --blob-size 1342177275 \
  --concurrency 1

talis fibre-reader \
  --experimental-max-blob-size-mib 1280 \
  --download-concurrency 1 \
  --download-timeout 10m
```

For 20x, use `2560` MiB and a maximum payload of `2684354555` bytes. The
experimental maximum must be identical on every server, txsim, and reader.

## Sources

- Initial constant: commit `a84a072520049f83cb7005bc6332380f59bb35b6`
- Protocol-parameter extraction/audit: commit
  `2b97183e796826802ffb5c222410d421de0c223a`
- Current implementation: `fibre/protocol_params.go`, `fibre/blob.go`,
  `fibre/internal/grpc/codec.go`, `fibre/internal/grpc/server.go`,
  `fibre/store_codec.go`, and `x/fibre/types/gas.go`
- Historical 128 MiB square capacity:
  <https://gist.github.com/rootulp/8a0083332e6ad6fd3f3f9121de3a956e>
- Fibre rollout/config compatibility discussion:
  <https://github.com/celestiaorg/celestia-app/issues/7475>
- Existing JSON-RPC request-size mismatch:
  <https://github.com/celestiaorg/celestia-node/issues/5009>
