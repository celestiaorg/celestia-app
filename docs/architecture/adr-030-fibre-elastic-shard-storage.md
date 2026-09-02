# ADR 030: Fibre Elastic Shard Storage

## Changelog

- 2026-09-01: Initial draft
- 2026-09-01: Stream object uploads and simplify the read path

## Status

Proposed

## Summary

Fibre shard capacity is limited by each validator's local disk. Demand increases require coordinated hardware changes before validators need the capacity.

This ADR moves durable shard payloads to S3-compatible object storage. Object mode streams payloads to the provider without local shard writes.

## Context

Fibre currently stores shard payloads as flat files on each validator's local disk. This design makes local disk capacity the limit for Fibre shard storage.

The server calls `Store.Put` before it signs a storage promise. This ADR keeps that order.

### Scope

This ADR changes these parts of Fibre:

- The internal storage of shard payloads below `Store`.
- The construction and configuration of local and object-storage backends.
- The local staging directory for local mode.
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

- **Object mode** streams encoded shards to object storage. Reads access object storage.
- **Local mode** stores durable shards as flat files on the Fibre server disk.

The governance-derived storage budget will continue to limit logical occupancy.

Each Pebble shard marker will contain a version, the durable backend, and an 8-byte encoded payload size. The size lets Fibre calculate occupancy and freed capacity without object listing or metadata requests.

The marker lets Fibre read and prune the correct backend after an operator changes the storage mode between node starts.

## Detailed Design

### Design overview

```text
Fibre server
  └── Store
      ├── Pebble metadata and prune index
      └── shardStorage
          └── durable backend
              ├── localBackend
              └── objectBackend
```

`Store` will continue to own Pebble metadata. `shardStorage` will own durable-backend routing.

Fibre will write each new shard to one durable backend. Local mode will use `localBackend`, and object mode will use `objectBackend`.

Both durable backends will use the existing shard binary codec. One payload contains one validator-assigned `BlobShard` from one upload request ([`fibre/store_codec.go:15`](../../fibre/store_codec.go#L15)).

### Write flow

#### Object mode

1. The server validates the upload, payment promise, assignment, and shard.
2. `Store` calculates the exact encoded size without encoding the shard ([`fibre/store_codec.go:90`](../../fibre/store_codec.go#L90)).
3. `Store` streams `writeShardBinary` directly into the `PutObject` request body ([`fibre/store_codec.go:33`](../../fibre/store_codec.go#L33)).
4. The request uses `If-None-Match: *` to prevent an overwrite.
5. `Store` waits for the object upload response.
6. `Store` commits the Pebble metadata with the `object` backend and encoded payload size.
7. The server signs and returns the storage promise.

Object storage is authoritative in object mode. Object mode does not write shard payloads to local disk.

If the object write fails, `Store` does not commit metadata or sign the storage promise.

If the Pebble commit fails, `Store` attempts to remove the object. It returns an error after the cleanup attempt.

#### Local mode

Local mode will keep the current stage, publish, and Pebble commit flow ([`fibre/store.go:134`](../../fibre/store.go#L134)).

The publish step atomically renames the staging file to its final path. The Pebble marker will record `local` and the encoded payload size.

At startup, `Store` removes incomplete local-mode staging files.

### Read flow

Pebble will return the candidate shard markers for a requested commitment. Each marker records the payload's durable backend.

For each candidate, `Store` will use this flow:

1. Select the durable backend from the shard marker.
2. For a local marker, read the local flat file.
3. For an object marker, read the object with `GetObject`.
4. If local storage returns `NotFound`, remove the stale marker and try the next candidate.
5. If object storage returns `NotFound`, keep the marker, record an integrity error, and try the next candidate.
6. If another durable-storage error occurs, record it and try the next candidate.
7. If no candidate succeeds, return the recorded error.

Every read of an object-backed shard accesses object storage.

The current read flow uses Pebble markers to select candidate shard files. It removes stale markers after missing-file errors ([`fibre/store.go:265`](../../fibre/store.go#L265), [`fibre/store.go:289`](../../fibre/store.go#L289)).

### Read latency concerns

Object-backed reads add network and provider latency to the Fibre read path. This latency can be higher than local file latency.

The implementation must measure read latency under the expected request rate. If recent reads require lower latency, a later ADR can add a cache for recent uploads.

This ADR does not define the cache design or its storage medium.

### Store integration

The `Store` API will not change. Its methods will delegate shard payload operations to `shardStorage`.

| Method | Behavior |
|---|---|
| `Store.Put` | Call `shards.Put` before the Pebble batch commit. |
| `Store.Get` | Get candidate markers from Pebble, then call `shards.Get` for each candidate. |
| `Store.Has` | Read Pebble first, then call `shards.Has` for the recorded durable backend. |
| `Store.PruneBefore` | Call `shards.Delete` before removing the Pebble entries. |
| `Store.Size` | Sum the sizes in shard markers and stat empty legacy markers. |
| `Store.DiskAvailable` | Report local space for Pebble, local payloads, and local staging files. |

The current `Store` directly owns Pebble and its filesystem ([`fibre/store.go:68`](../../fibre/store.go#L68)). The new `shardStorage` field will replace only direct shard-payload operations.

### Backend construction

`NewStore` will always open Pebble and `localBackend`.

`NewStore` will open `objectBackend` in object mode. It will also open this backend while live markers record `object`.

The primary backend will support these values:

- `local`: Store new shards in `localBackend`. This value is the default.
- `object`: Stream new shards to `objectBackend`.

Tests can inject durable backends directly. `NewMemoryStore` will use `localBackend` with the existing in-memory filesystem.

The object backend will use provider-neutral configuration:

```toml
storage_backend = "local" # local or object

[object_storage]
endpoint = "https://<account-id>.r2.cloudflarestorage.com"
region = "auto"
bucket = "fibre-shards"
prefix = "fibre"
```

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

### Prune flow

Pebble will continue to select expired entries from its prune index ([`fibre/store.go:410`](../../fibre/store.go#L410)).

For each expired entry, `Store` will use this flow:

1. Select the durable backend from the shard marker.
2. Read the durable payload size from the shard marker.
3. Remove the durable payload. A missing payload counts as success.
4. Remove the payment promise, shard marker, and prune marker from Pebble.

If durable deletion fails, `Store` keeps the Pebble entries. A later prune cycle can retry the deletion.

For a legacy empty marker, `Store` will get the size from the local file before deletion.

Fibre protocol pruning will be the only authority for shard expiration. Operators must not configure provider lifecycle rules to delete Fibre shard objects.

Provider lifecycle rules cannot read Fibre promise expiration or Pebble prune markers. A provider rule can remove a promised shard too early.

### Capacity accounting

Fibre will sum marker sizes and stat local files for empty legacy markers. Both modes will exclude Pebble bytes and preserve the governance budget.

`DiskAvailable` will continue to monitor local filesystem space.

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

Reads would use the primary backend. They would use the secondary backend after `NotFound` from the primary backend.

After a different primary error, Fibre would record the error and try the secondary backend. If the secondary misses, Fibre would return the primary error.

Fibre would not call `Has` before `Get`, because this sequence adds an object-storage request.

During migration, pruning would attempt both durable backends before it removes Pebble metadata. The secondary-backend configuration could remain after old shards expire.

This approach preserves the Pebble marker format. It adds backend requests and requires backend listing for startup size accounting.

## Consequences

### Positive

- Object storage removes local disk as the durable shard-capacity limit.
- Object-mode uploads do not write shard payloads to local disk.
- Marker routing supports storage-mode changes between node starts.
- Marker sizes avoid object listing and metadata requests for capacity accounting.
- Fibre keeps authority over the complete shard lifecycle.

### Negative

- Every accepted object-mode upload waits for `PutObject`.
- An object-storage outage stops new object-mode uploads and object-backed reads.
- Object-backed reads add network and provider latency.
- Object operations create provider charges.
- Pebble remains the only metadata index for object-backed shards.
- A binary downgrade requires an external migration utility while object-backed shards remain live.

### Risks and mitigations

| Area | Risk | Mitigation or limit |
|---|---|---|
| Write latency | An object upload can exceed the current upload timeout. | Measure the complete write path against the timeout. |
| Provider availability | An outage stops new uploads and object-backed reads. | Fibre reports the provider error and keeps its Pebble markers. |
| Read latency | Object storage adds latency to every object-backed read. | Measure object-read latency and define request limits. |
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
