# Slab Pool Design

`slab.Pool` is a growable byte-region allocator used in the fibre blob
encoding pipeline. `RowAssembler` currently uses two independent pools built
from the same allocator:

- a parity pool for parity rows
- a work pool for large Reed-Solomon FFT scratch buffers

## Problem

Go's `sync.Pool` drops all entries every GC cycle. The leopard RS encoder
allocates ~1 GiB work buffers per encode, which triggers GC frequently enough
that the pool never retains — every encode allocates fresh, inflating RSS from
~1.5 GiB (theoretical) to 8+ GiB through accumulated MADV_FREE pages.

## Properties

### GC-proof retention

Slabs are held in a regular slice (a GC root), not `sync.Pool`. They survive
garbage collection indefinitely and are only released when [Pool.Shrink]
drops them — retention is driven by observed demand, not by GC pressure.

### Demand-driven growth

No upfront sizing or pre-allocation. The pool starts empty and each `Get`
tries existing slabs first; when a request can't be satisfied, a new slab
is grown sized to exactly the unsatisfied deficit. One miss therefore
doesn't inflate off the total live set — concurrent large blobs each add
just the bytes they need, not triangular overprovisioning. The pool adapts
to whatever mix of blob sizes the caller submits.

### Dynamic row sizes

The slab tracks byte regions via a free-list, not fixed-size slots. A 1 MiB
blob's parity (12290 × 320 bytes = 3.8 MiB) and a 128 MiB blob's parity
(12290 × 32 KiB = 384 MiB) both allocate from the same pool proportionally.
Many small blobs pack efficiently into a slab sized for one large blob.

### Contiguous-first allocation

`allocMany` tries to satisfy the full request from a single contiguous region
before falling back to scattered allocation from individual free regions.
This preserves cache/TLB locality for RS FFT work buffers (which access
`work[i]` and `work[i+dist]` in butterfly patterns) while still allowing
parity rows to reuse scattered freed regions after per-validator release.

### Per-validator release

Individual row buffers can be freed back to the pool independently via `Put`.
When a validator's upload completes, its parity rows are returned to the slab
immediately — the next blob's `Get` can reuse those regions without waiting
for all validators to finish. The free-list coalesces adjacent frees
automatically.

The last validator to finish calls terminal `Assembly.Free(nil)` which frees
any remaining rows (head, tail, unassigned parity) and triggers `Shrink`.
Non-terminal validators call `Assembly.Free(rowIndices)` for partial release.

### Shrink when idle

`Shrink()` is called only at blob lifecycle boundaries (terminal
`Assembly.Free`), not on every `Put`. This prevents hot slabs (like the work
buffer slab) from being destroyed and recreated between encode cycles.

Shrink drops the **smallest** fully-free slabs first, so bootstrap-sized
slabs are trimmed before well-adapted warm capacity.

Eviction applies a grace period while the pool is still active. When at
least one slab has outstanding allocations, each fully-free slab may survive
up to `freeSlabShrinkGrace` shrink cycles before becoming evictable. A
successful `alloc` resets the counter, so a slab that briefly becomes free
and is then reused stays warm indefinitely. Once every slab is free (pool
fully idle) the grace period is ignored and all slabs are dropped
immediately — there's no active workload to preserve capacity for. The
pool carries no permanent retention floor; its memory footprint tracks
demand.

This absorbs the common overlap pattern: blob A's terminal release fires
while blob B's encode is mid-flight, briefly leaving a slab free. Without
the grace period, that slab would be evicted and reallocated for B; with
it, the same slab carries over.

### SIMD alignment

All row sizes are protocol-constrained to multiples of 64 bytes, so every
carved offset within a slab is naturally SIMD-aligned. Small slabs (< 1 MiB)
are allocated via `reedsolomon.AllocAligned` on the Go heap; large slabs use
`mmap` which returns page-aligned memory (see below).

### Off-heap allocation via mmap

Slabs at or above 1 MiB go through `mmap(MAP_PRIVATE|MAP_ANONYMOUS)` rather
than the Go heap. Large slabs stay invisible to the GC so GOGC's heap-
doubling target doesn't stack multi-GiB slab allocations on top of each
other (which OOMs memory-constrained nodes). `Shrink` calls `munmap`
directly, returning pages to the OS without waiting for GC or MADV_FREE.
Slabs below the threshold stay on the Go heap where allocation is cheap
and GC overhead is negligible.

## Free-path optimizations

Per-validator shards of parity rows arrive at `Put` as a shuffled subset
of offsets (validators are assigned rows through a ChaCha8 permutation in
`Set.Assign`). A naive free-list that inserts each freed region one-by-one
degrades to O(N·R) under this pattern: each insertion scans the regions
list linearly, and R grows with every non-adjacent free until the next
merge chain forms. Three composed optimizations keep the free path close
to O(N) for typical workloads.

### Sort by pointer at the entry of Put

`Put` sorts its input buffers by data-pointer before acquiring `Pool.mu`.
After the sort, buffers within one slab appear in ascending-offset order
and adjacent-in-memory buffers end up contiguous in the sorted sequence.
The sort is on the caller's slice and runs outside the lock.

### Run merging in `freeBatch`

Once Put has grouped consecutive same-slab buffers, `slab.freeBatch`
walks the run and collapses adjacent-in-memory buffers into a single
region before handing it to the coalesce step. Terminal `Assembly.Free(nil)`
of a fast-path group — every slot adjacent — reduces to one region
insertion regardless of the slot count.

### Monotonic coalesce hint

`coalesceAt(r, hint)` takes a starting index for the insertion scan and
returns the final position (after any left-merge). For a sorted sequence
of regions, the caller threads the returned index back in as the next
`hint` so each insertion resumes from where the previous one landed.
Amortized cost per insertion drops from O(R) to O(1) for monotonic batches.

The three compose: sort makes the input sequence monotonic; run merging
reduces insertion count by coalescing adjacencies up front; the hint keeps
each remaining insertion O(1). Result: a sorted-batch Put is effectively
linear in the number of freed bytes.

## Integration

The allocator is exposed through two separate pools:

- **work pool / `reedsolomon.WorkAllocator`** — the RS encoder calls `Get`/`Put`
  for its temporary FFT work buffers. Injected at encoder construction time
  via `reedsolomon.WithWorkAllocator`.

- **parity pool / `Assembly`** — `RowAssembler.Assemble` draws parity rows plus
  head/tail buffers from a dedicated pool. `Assembly` owns that pooled storage
  for a single blob, supports partial release via `Free(rowIndices)`, and
  terminal release via `Free(nil)`. The `Assembly.Freed(index)` query lets
  `Blob.Row()` reject reads on released rows.

This split is intentional. A single shared pool was benchmarked and rejected:
large contiguous work slabs and partially-freed parity slabs fragmented each
other, causing steady-state multi-GiB/min slab churn even though total free
capacity was ample. Separating the pools restored homogeneous allocation
shapes, eliminated work-pool churn after warmup, and left only minor residual
fragmentation inside the parity pool.

## Allocation Lifecycle

```text
Encode blob A:
  parityPool.Get(12290, rowSize) → parity A from slab (contiguous fast path)
  workPool.Get(32768, rowSize)   → work from slab (contiguous, or new slab via growth)
  [RS encode]
  workPool.Put(work)              → work freed back to work pool

Upload blob A (concurrent with encode blob B):
  validator 1 done → asm.Free([rows]) → parity rows freed to parity pool
  validator 2 done → asm.Free([rows])
  ...

Encode blob B (while blob A still uploading):
  parityPool.Get(12290, rowSize) → parity B from freed regions (contiguous or scattered)
  workPool.Get(32768, rowSize)   → work from warm work slab (contiguous fast path)
  [RS encode]
  workPool.Put(work)

Last validator done for blob A:
  asm.Free(nil)                  → remaining rows freed
                                → parityPool.Shrink()
                                → workPool.Shrink()
```

## Memory Footprint

For max-size blobs the pool reaches the theoretical lower bound: per
concurrent blob it reserves exactly 4×D work + 3×D parity + 1×D original
= 8×D (D = data size). Nothing is allocated above that floor except a
thin Go heap.

Measured on a 16 GiB node with `--await-all`, 128 MiB blobs:

- **c=1**: ~1.4 GiB steady RSS (1.36 GiB mmap slabs + ~50 MiB Go heap
  under `GOMEMLIMIT=512MiB`).
- **c=10**: 13 GiB floor, ~15 GiB peak. Ten concurrent encodes cleanly
  fit the 16 GiB budget.

Slab-allocation churn in steady state is ~1.5 MiB/min (Pyroscope
`alloc_space`, c=3, 2-min window): the two pools grow to the concurrency
high-water mark and stay warm.

Smaller blobs scale down proportionally. For 1 MiB blobs (rowSize=320)
a single concurrent encode needs ~10 MiB work + ~3.8 MiB parity.

## Limitations

- **Parity-pool fragmentation under mixed blob sizes**: split pools removed the
  dominant work/parity cross-fragmentation, but the parity pool still serves a
  range of row sizes and supports partial release. Mixed small/medium/large
  blobs can therefore still leave scattered holes that force the parity path
  onto the slower gather/scatter allocation path or trigger a new slab for a
  relatively small deficit. This is now the main remaining fragmentation risk:
  pool segregation fixed cross-class contamination, but it does not eliminate
  variable-size fragmentation within one allocation class.

- **Cold-start regrowth after full idle**: once both pools go fully idle,
  all slabs are dropped. A subsequent burst has to pay the mmap + zeroing
  cost for every slab it rebuilds. This is the tradeoff for carrying no
  permanent retention floor — idle memory stays bounded, but the first
  post-idle blob is slower than subsequent ones.

## Future Improvements

- **Independent retention policies per pool**: work and parity have very
  different churn characteristics, so they may deserve different grace
  periods or eviction heuristics. Work is throughput-sensitive and
  homogeneous; parity is more fragmentation-sensitive and memory-sensitive.
  Parity naturally shrinks at blob lifecycle boundaries
  (`Assembly.Free(nil)`); work may be a better fit for an encode-idle
  policy such as "pool fully free for T" rather than aging only through
  later terminal frees.

- **Size segmentation inside the parity pool**: if mixed blob sizes become
  a meaningful source of churn, the next step is to segment parity
  allocations further by size class or row-size bands. For example:
  dedicated small/medium/large parity pools, or a special path for
  oversized parity slabs.

- **Demand-aware retention**: retention today is purely reactive (grace
  period + smallest-first eviction, no floor). If the cold-start-after-idle
  cost shows up under real workloads, `Shrink` could learn a warm-capacity
  target from recent demand with decay, effectively acting as an adaptive
  floor that shrinks after prolonged idle and grows back after bursts.

- **Statically preallocated configurable slabs**: for deployments with
  known-stable workload shape (blob size, concurrency, validator count),
  pay the mmap cost once at `New()` and skip first-burst regrowth entirely.
  The caller supplies a fixed inventory — e.g. `slab.NewWith(slab.Config{
  Preallocate: []slab.SlabSpec{{Size: 1<<30, Count: 2}, {Size: 256<<20, Count: 4}}})`
  — and those slabs are pinned: immune to `Shrink`, warm from the first
  `Get`. Demand beyond the preallocated inventory still grows/shrinks
  normally. Trades configurability + RSS cost for predictable steady-state
  latency and no cold-start penalty; useful for validator/sequencer nodes
  with a deterministic workload profile that currently pays the first-blob
  tax on every cold start.
