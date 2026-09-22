# Results: Fibre object storage with 1280 MiB blobs

Experiment for [PROTOCO-2830](https://linear.app/celestia/issue/PROTOCO-2830), run on 2026-09-22 in AWS `us-east-2`.
It used 3 validators, one S3 bucket with a prefix per validator, and 1280 MiB blobs.
Integration of [#7808](https://github.com/celestiaorg/celestia-app/pull/7808) (`38e43d16`) and [#7913](https://github.com/celestiaorg/celestia-app/pull/7913) (`99969ce9`).

## Summary

The object storage path works end to end with 1280 MiB blobs.
Blobs upload, verify, store in S3, confirm on chain, and download and reconstruct byte for byte.
There were no failed uploads apart from in-flight work cancelled when a run ended.

The target of 4 GB/s of S3 writes per validator was not reached.
The best sustained result was 1.53 GB/s per validator (4.57 GB/s aggregate, 2.74 TB) over 600 seconds.
Short 2-minute stages on x86 validators reached 1.43 GB/s per validator.
With 3 validators, each one stores a full 4096-row shard, so S3 bytes per validator equal the blob throughput.

The main S3 adapter bottleneck is the single-part `PutObject`.
It uploads each 1.34 GB shard at about 116 MB/s, so every put takes 11.6 s.
On x86 validators that is about 89% of the server's time per upload.

## Setup and results

| Run | Validators | Encoders | Workers | S3 writes per validator | Notes |
| --- | --- | --- | --- | --- | --- |
| Ramp 1–3 | 3 × c8gn.24xlarge (arm64, 192 GiB) | 1 × c8in.48xlarge | 12 / 24 / 40 | 0.42 / 0.79 / 1.22 GB/s | Scaled linearly until validator memory was full |
| 600 s window | same | same | 40 | **1.52–1.53 GB/s** (min rolling 60 s ≥ 1.21) | 748 confirmed, 0 failures, no OOM |
| Ramp A–B | 3 × c8gn.48xlarge (arm64, 384 GiB) | 3 (r6in/r8i.16xlarge) | 45 / 75 | 1.07 / 1.25 GB/s | More memory did not help: slower uploads |
| x86 ramp | 3 × m8ib.48xlarge (x86, 768 GiB) | 3 × m8i.16xlarge | 45 / 60 | **1.43** / ~1.3 GB/s | Encoder side plateaued at ~1.5 GB/s per encoder |

Direct probes from the validators set the ceilings:

- Network: about 100 Gbit/s from the encoder to each validator.
- S3 with multipart uploads (s5cmd): 11.6–14.8 GB/s per validator.
- S3 with 70 concurrent single-part puts: 6.1 GB/s.

## Bottlenecks found

1. **Single-part `PutObject`: the S3 adapter.**
   Each shard uploads as one HTTP PUT at 115–116 MB/s, taking 11.6 s at p50, p90, and max.
   The shard's memory (about 4 GiB with decode copies) is held for the whole put.
   A failed or cancelled put discards all 1.34 GB.
   S3 showed no throttling or errors.
2. **Memory held per in-flight shard: the server.**
   Each upload holds about three whole copies of the shard during decoding: the gRPC buffer, `Materialize`, and `BlobRow.Unmarshal`.
   One copy then stays until the put finishes.
   `GOMEMLIMIT` cut peak memory by about 23%.
3. **Reed-Solomon on arm64.**
   klauspost/reedsolomon v1.14.2 has no arm64 SIMD for GF(2^16), so it uses pure-Go `refMulAdd` kernels.
   This affects encoding (about 30 s per blob) and server verification (64% of server CPU).
   Verification takes 1.4 s on x86 and about 11 s on Graviton.
4. **Encoder side.**
   Each client process uses one TCP flow per validator, which carries about 0.63 GB/s.
   Each worker also holds about 7–9 GiB.
   Upload latency stayed around 33–48 s at p50, while the server spent about 13 s per upload.

## Fixes made on this branch

- #7913: fibre-txsim now encodes with the configured max blob size, and the client RPC timeout scales with blob size (15 s would cancel every large upload).
- talis:
  - Supports arm64 instance types and IAM instance profiles, and adds `start-fibre --object-storage-*` flags.
  - Reads the public key from `TALIS_SSH_PUB_KEY_PATH` instead of the private key variable.
  - Writes the fibre config to `<home>/config/`.
  - Truncates `config.json` when saving.
- fibre-txsim `--key-offset`, so several processes can share a keyring.
- Experiment-only logs of verify and put durations in `UploadShard`.

Known talis issues not fixed here:

- `--config` is ignored, so `talis down --config` destroys every instance in `config.json`.
- `talis add` numbers new nodes from 0.
- Parallel `talis up` races when importing the key pair.

## Next steps

1. Upload shards with parallel multipart puts, for example 16 parts of about 84 MB. This should cut the put from 11.6 s to about 1 s, based on the s5cmd probes. Retries would then cover one part instead of the whole shard.
2. Avoid the decode copies: unmarshal from gRPC's buffers without `Materialize`, and let row data point into the receive buffer instead of copying it.
3. Stream `UploadShard` (a proto change) so the server verifies and uploads in chunks instead of holding the whole shard.
4. Add a NEON GF(2^16) path to reedsolomon, or run x86 validators and encoders until one exists.
5. Rerun with multipart puts on x86, with more encoders or larger encoders.

Cost was about $72 of EC2, plus less than $1 of S3 and EBS.
All resources were destroyed, and no instances, volumes, endpoints, buckets, or instance profiles remain.
