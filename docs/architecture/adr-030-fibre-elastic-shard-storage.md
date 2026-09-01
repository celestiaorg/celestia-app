# ADR 030: Fibre Elastic Shard Storage

## Changelog

- 2026-09-01: Initial draft

## Status

Proposed

## Summary

Fibre shard capacity is limited by each validator's local disk. Demand increases require coordinated hardware changes before validators need the capacity.

This ADR moves durable shard payloads to S3-compatible object storage. It keeps recent object-backed shards in a bounded local cache.

## Context

Fibre currently stores shard payloads as flat files on each validator's local disk. This design makes local disk capacity the limit for Fibre shard storage.

The server calls `Store.Put` before it signs a storage promise. This ADR keeps that order.

### Scope

This ADR changes these parts of Fibre:

- The internal storage of shard payloads below `Store`.
- The construction and configuration of local and object-storage backends.
- The local staging and recent-shard cache directories.
- The write, read, startup recovery, and prune flows for shard payloads.
- The backend and payload size in each Pebble shard marker.

This ADR does not change these parts:

- The public `Store` method signatures.
- The upload service, payment rules, or storage-promise format.
- The `BlobShard` binary codec or the unit of stored data.
- Any protobuf, consensus state, or governance parameter.
- Client egress from Fibre servers.
- The need for persistent local Pebble metadata.
- The deployment of an independent read tier.

## Decision

Fibre will keep Pebble as its local metadata store. Fibre will store each durable shard payload in a local flat file or object storage.

The first object-storage implementation will support Cloudflare R2 and AWS S3 through one S3-compatible backend. Cloudflare R2 is the expected provider.

- **Object mode** stores durable shards in object storage. It keeps recent shards in a bounded local flat-file cache.
- **Local mode** stores durable shards as flat files on the Fibre server disk. It does not use the cache.

The governance-derived storage budget will continue to limit logical occupancy.

Each Pebble shard marker will contain a version, the durable backend, and an 8-byte encoded payload size. The size lets Fibre calculate occupancy and freed capacity without object listing or metadata requests.

Existing empty marker values will mean legacy local storage. The marker lets Fibre read and prune the correct backend after an operator changes the storage mode between node starts.

## Detailed Design

### Design overview

```text
Fibre server
  └── Store
      ├── Pebble metadata and prune index
      └── shardStorage
          ├── bounded local flat-file cache (object mode only)
          └── durable backend
              ├── localBackend
              └── objectBackend
```

`Store` will continue to own Pebble metadata. `shardStorage` will own cache policy and durable-backend routing.

Fibre will write each new shard to one durable backend. Local mode will use `localBackend`, and object mode will use `objectBackend`.

The cache will operate only in object mode. It will be a best-effort performance layer, not a durable backend.

Both durable backends will use the existing shard binary codec. One payload contains one validator-assigned `BlobShard` from one upload request ([`fibre/store_codec.go:15`](../../fibre/store_codec.go#L15)).

### Write flow

#### Object mode

1. The server validates the upload, payment promise, assignment, and shard.
2. `Store` encodes the shard once into a random staging file and gets its exact file size.
3. `objectBackend` uploads the staging file with `PutObject`.
4. The request uses `If-None-Match: *` to prevent an overwrite.
5. `Store` waits for the `PutObject` response.
6. `Store` renames the staging file to the stable cache path.
7. If the rename fails, `Store` logs the cache error and removes the staging file.
8. `Store` commits the Pebble metadata with the `object` backend and encoded payload size.
9. The server signs and returns the storage promise.

Object storage is authoritative in object mode. A cache error does not fail an upload after the object write succeeds.

If the object write fails, `Store` removes the staging file. It does not commit metadata or sign the storage promise.

If the Pebble commit fails, `Store` attempts to remove the object and cache entry. It returns an error after the cleanup attempt.

The staging and cache directories must use the same filesystem. This requirement permits an atomic rename and prevents a second local shard write.

At startup, `Store` removes incomplete staging files. In object mode, startup removes cache files older than `max_age`.

Startup then removes the oldest cache files until cache use is less than `max_bytes`. In local mode, startup removes the complete cache directory with best-effort cleanup.

Cache cleanup errors produce logs and metrics but do not stop startup.

#### Local mode

Local mode will keep the current stage, rename, and Pebble commit flow. The Pebble marker will record `local` and the encoded payload size.

This design keeps the current stage, publish, and commit pattern ([`fibre/store.go:134`](../../fibre/store.go#L134)).

### Read flow

Pebble will return the candidate shard markers for a requested commitment. Each marker records the payload's durable backend.

For each candidate, `Store` will use this flow:

1. In object mode, read the local cache. In local mode, continue to the recorded durable backend.
2. If the cache contains the shard, return the shard.
3. If the cache misses, record a metric and read the recorded durable backend.
4. If the cache returns an error, record a metric, log the error, and read the recorded durable backend.
5. If object storage returns `NotFound`, keep the marker, record an integrity error, and try the next candidate.
6. If no candidate succeeds, keep the marker and return any durable-storage error.

An old read will not evict a more recent upload. Reads will not populate the cache.

The current read flow uses Pebble markers to select candidate shard files. It removes stale markers after missing-file errors ([`fibre/store.go:265`](../../fibre/store.go#L265), [`fibre/store.go:289`](../../fibre/store.go#L289)).

### Store integration

The `Store` API will not change. Its methods will delegate shard payload operations to `shardStorage`.

| Method | Behavior |
|---|---|
| `Store.Put` | Call `shards.Put` before the Pebble batch commit. |
| `Store.Get` | Get candidate markers from Pebble, then call `shards.Get` for each candidate. |
| `Store.Has` | Read Pebble first, then call `shards.Has` for the recorded durable backend. |
| `Store.PruneBefore` | Call `shards.Delete` before removing the Pebble entries. |
| `Store.Size` | Sum the sizes in shard markers and stat empty legacy markers. |
| `Store.DiskAvailable` | Report local space for durable shards in local mode, or Pebble and cache files in object mode. |

The current `Store` directly owns Pebble and its filesystem ([`fibre/store.go:68`](../../fibre/store.go#L68)). The new `shardStorage` field will replace only direct shard-payload operations.

### Backend construction

`NewStore` will always open Pebble and the selected durable backend. It will open the cache only in object mode.

In local mode, `NewStore` will remove an existing cache directory. It will still open `objectBackend` while live markers record `object`.

The primary backend will support these values:

- `local`: Store new shards in `localBackend`. This value is the default.
- `object`: Store new shards in `objectBackend` and recent copies in the local cache.

Tests can inject durable backends directly. `NewMemoryStore` will use `localBackend` with the existing in-memory filesystem.

The object backend will use provider-neutral configuration:

```toml
storage_backend = "local" # local or object

[object_storage]
endpoint = "https://<account-id>.r2.cloudflarestorage.com"
region = "auto"
bucket = "fibre-shards"
prefix = "fibre"

[shard_cache]
max_age = "1m"
max_bytes = 171798691840 # 160 GiB
```

Object mode requires a positive cache age and byte limit. The initial values cover one minute at 2.2 GB/s with approximately 30 percent byte headroom.

Operators must create the bucket before Fibre starts. Operators will configure the bucket and object-key prefix.

Fibre will derive the chain ID and validator address. Fibre will read credentials from the standard AWS environment variables.

The implementation will use the existing AWS SDK for Go v2 dependency ([`go.mod:21`](../../go.mod#L21)).

### Object layout

Each object key will use this format:

```text
<prefix>/<chain-id>/<validator-address>/shards/<commitment>-<promise-hash>
```

The prefix is part of each key. The chain ID prevents collisions when one bucket contains several networks.

The validator address prevents collisions when one operator uses the bucket for several validators. Each object will contain one complete validator-assigned `BlobShard`.

### Recent-shard cache

The cache will store bounded local copies of recent object-backed shards. It will preserve the current local read path for recent uploads.

The cache will use these rules:

- Populate the cache only from a validated upload.
- Publish each entry with an atomic rename.
- Set the file modification time to the cache publication time.
- Store cache files separately from legacy durable shard files.
- Remove cache files older than `max_age`.
- Evict the oldest uploads until a new entry fits.
- Keep total cache use within a fixed byte limit.
- Read from the durable backend after a cache miss or error.
- Do not populate the cache from durable reads.
- Keep the cache out of durable `Has` and logical `Size` results.
- Attempt to remove cached copies during pruning.
- Permit complete cache removal without data loss.

The implementation will use flat files and the Go standard library. It will not add a cache dependency.

At startup, Fibre will rebuild an in-memory cache index from file modification times. The index will track keys, sizes, and publication times.

The cache index will not contain shard data. It will provide oldest-first eviction without a directory scan after each upload.

### Prune flow

Pebble will continue to select expired entries from its prune index ([`fibre/store.go:410`](../../fibre/store.go#L410)).

For each expired entry, `Store` will use this flow:

1. If the cache entry exists, attempt to remove it. A missing entry counts as success.
2. Select the durable backend from the shard marker.
3. Read the durable payload size from the shard marker.
4. Remove the durable payload. A missing payload counts as success.
5. Remove the payment promise, shard marker, and prune marker from Pebble.

If cache removal fails, `Store` logs the error and continues with durable deletion. Cache errors do not block protocol pruning.

If durable deletion fails, `Store` keeps the Pebble entries. A later prune cycle can retry the deletion.

For a legacy empty marker, `Store` will get the size from the local file before deletion.

Fibre protocol pruning will be the only authority for shard expiration. Operators must not configure provider lifecycle rules to delete Fibre shard objects.

Provider lifecycle rules cannot read Fibre promise expiration or Pebble prune markers. A provider rule can remove a promised shard too early.

### Capacity accounting

Fibre will sum marker sizes and stat local files for empty legacy markers. Both modes will exclude Pebble and cache bytes, preserve the governance budget, and use `DiskAvailable` to monitor local space.

### Storage migration

A storage-mode change will not move existing payloads. Live shards can exist in both durable backends during one retention window.

- Local to object: New uploads use object storage, while old markers continue to use local storage.
- Object to local: New uploads use local storage, while old markers continue to use object storage.

Every marker records where its payload lives. Fibre reads and prunes that backend after the operator changes the write mode.

During an object-to-local migration, operators must keep the object-storage configuration and credentials until all object-backed shards expire.

#### Binary downgrade

A direct binary downgrade is not possible while object-backed shards remain live. Old Fibre versions read every marker as a local flat-file reference.

Before a downgrade, an external utility must copy each live object to its legacy local path. The utility must also clear each copied marker value.

## Alternative Approaches

### Preserve empty Pebble marker values

`shardStorage` could keep all marker values empty. It could use a primary durable backend and an optional secondary durable backend.

After a cache miss, reads would use the primary backend. They would use the secondary backend after `NotFound` from the primary backend.

After another primary read error, Fibre would record the error and try the secondary backend. If the secondary misses, Fibre would return the primary error.

Fibre would not call `Has` before `Get`, because this sequence adds an object-storage request.

During migration, pruning would attempt both durable backends before it removes Pebble metadata. The secondary-backend configuration could remain after old shards expire.

This approach preserves the Pebble marker format. It adds backend requests and requires backend listing for startup size accounting.

## Consequences

### Positive

- Object storage removes local disk as the durable shard-capacity limit.
- The bounded cache keeps recent reads on local disk in object mode.
- Marker routing supports storage-mode changes between node starts.
- Marker sizes avoid object listing and metadata requests for capacity accounting.
- Fibre keeps authority over the complete shard lifecycle.

### Negative

- Every accepted object-mode upload waits for `PutObject`.
- An object-storage outage stops new object-mode uploads and uncached reads.
- Object operations create provider charges.
- Pebble remains the only metadata index for object-backed shards.
- A binary downgrade requires an external migration utility while object-backed shards remain live.

### Risks and mitigations

| Area | Risk | Mitigation or limit |
|---|---|---|
| Write latency | An object upload can exceed the current upload timeout. | Measure the complete write path against the timeout. |
| Provider availability | An outage stops new uploads and uncached reads. | Cached reads remain available, but the cache is not durable. |
| Request cost | Object operations create provider charges. | Use direct `GetObject` reads and marker-based routing. |
| Binary downgrade | Old Fibre cannot read live object-backed shards. | Copy live objects to legacy local paths before downgrade. |
| Disaster recovery | Loss of Pebble removes the object index. | Complete disaster recovery is outside this ADR. |
| Provider lifecycle rules | Provider deletion can bypass promise expiration. | Keep Fibre pruning as the only expiration authority. |

## References

- [R2 S3 API compatibility](https://developers.cloudflare.com/r2/api/s3/api/)
- [R2 consistency model](https://developers.cloudflare.com/r2/reference/consistency/)
- [R2 durability](https://developers.cloudflare.com/r2/reference/durability/)
- [R2 limits](https://developers.cloudflare.com/r2/platform/limits/)
- [R2 pricing](https://developers.cloudflare.com/r2/pricing/)
- [R2 object lifecycle rules](https://developers.cloudflare.com/r2/buckets/object-lifecycles/)
- [AWS S3 pricing](https://aws.amazon.com/s3/pricing/)
- [AWS SDK for Go with R2](https://developers.cloudflare.com/r2/examples/aws/aws-sdk-go/)
