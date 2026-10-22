# ADR 031: Fibre Request Memory Admission

## Changelog

- 2026-10-19: Initial draft

## Status

Proposed

## Summary

gRPC buffers an entire request message (up to `MaxRecvMsgSize` ≈ 132 MiB for Fibre) into memory before the handler runs. Nothing bounded that receive memory except the connection and stream caps, so worst-case RAM scaled with `max_connections × max_concurrent_streams × 132 MiB`. This ADR admits every request against a global byte budget before it is read, so receive memory is bounded directly rather than through the connection count.

## Context

A Fibre validator serves unauthenticated RPCs (`UploadShard`, `DownloadShard`, and gRPC health). For each, grpc decodes the whole message before any application code — including the codec's size checks — runs. So a peer can send a 132 MiB `UploadShard`, `DownloadShard`, or health request and have it buffered pre-auth. The only pre-handler bound was transport-level (connection/stream caps), which couples memory to the connection count and is handled separately (see the Connections section, added by the connection-limits change).

## Decision

Admit every request against a global, FIFO `semaphore.Weighted` byte budget (`max_request_memory`, default 16 GiB) **before** it is read.

- **Generic wrapping.** Every method of the fibre service descriptor (and of any service registered through the server's registrar, e.g. health) is wrapped: unary methods become stream handlers that admit before the generated handler's decode step; streaming methods admit on each `RecvMsg`. Unary methods stay unary on the wire, so clients are unchanged. Verified against grpc v1.84.0: the server registers `ServiceDesc.Streams` as-is and only auto-wraps `ServiceDesc.Methods` (`server.go:808`), which the wrapper leaves empty, so there is no double wrapping.
- **Reserve max, then shrink.** An admitted request reserves the max message size, because grpc buffers up to `MaxRecvMsgSize` before the size is known. Right after decode it shrinks the reservation to the actual size, returning the difference to the budget (which wakes FIFO waiters), so lighter requests let more run concurrently.
- **Bounded wait.** When no budget is free a request waits up to 3s, then is rejected with `ResourceExhausted` carrying a short (~1–2s) `RetryInfo`. A waiter cap (256) fail-fast rejects once too many are parked.
- **Per-IP receive cap.** At most 8 concurrent *receiving* requests per source IP (IPv6 by /64), released once the body is read — so one source cannot pin the budget with many streams that stall before sending a body. After the body is read, the byte budget alone bounds memory.
- **Request deadline.** `grpc.InTapHandle` wraps each request context with a 16s deadline (just above the client's 15s RPCTimeout). grpc uses the tap-returned context as the stream context (`http2_server.go:575`), so a stalled `RecvMsg` returns at the deadline and the reservation is released.
- **Static receive windows** (64 KiB stream / 16 MiB connection) disable grpc BDP tuning; the stream window only bounds a paused stream since `RecvMsg` grants a whole-message window.

### Downloads

`DownloadShard` is served as a stream handler that also counts its response. It keeps the max reservation through the handler (so the shard the handler builds from storage is counted), shrinks the reservation to the response size once known, and releases it when the stream ends rather than at the handler's return. Verified against grpc v1.84.0: the server cancels the stream context in `finishStream`/`closeStream` after the handler returns (`http2_server.go:1338`/`1362`), so `context.AfterFunc(stream.Context(), release)` releases the reservation then without a goroutine leak. Because grpc's `SendMsg` returns as soon as the message is queued (`flowcontrol.go` `writeQuota.get` takes the whole message), the response bytes still draining to a slow reader after that point live in grpc's send buffer, not the budget — so a slow reader cannot pin the budget past the stream's end; those bytes are bounded by grpc connection flow control and `MaxConnectionAge`.

### Connections

With memory bounded by the byte budget, the connection cap no longer needs to be a tight memory bound, so the old `netutil.LimitListener(max_connections)` (default 16) — a pre-auth DoS where one peer could hold every slot and starve honest clients (PROTOCO-2220), and which blocked downloads during uploads (PROTOCO-2706) — is replaced by a fail-fast listener: a fixed global fd-safety cap (4096) plus a per-IP cap (`max_connections_per_ip`, default 16, IPv6 by /64, sharing the admission `keyedCounter`). Over-cap connections are closed immediately rather than parked. `max_connections` is retained as a deprecated, ignored key; `max_concurrent_streams` default is raised to 100.

## Alternatives

- **Tighter connection cap** keeps memory coupled to connections and remains a pre-auth DoS; rejected.
- **`InTapHandle` for admission** runs on the connection I/O goroutine and must not block, so it only sets the deadline; blocking admission runs in the per-request handler.
- **Interfaces for the admitter** were dropped: the concrete type lives in `internal/grpc` and takes an `onReject` callback for the existing metrics counter.

## Consequences

- Receive memory is bounded by a byte budget independent of the connection count; a pre-auth oversized request on any method (incl. downloads and health) is gated.
- `max_request_memory` bounds buffered request bytes, **not RSS**: decode peaks at a few times the message size and verification/storage allocate on top, so it is set well below host RAM.
- The reservation is the max size only while receiving (the size is unknown until decode); downloads' responses are not yet counted (follow-up).
- Rejections reuse `fibre.server.upload_shard.rejected` with reasons `per_ip`, `memory_timeout`, `memory_overflow`.

### Defaults

| Setting | Default | Role |
| --- | --- | --- |
| `max_request_memory` | 16 GiB | Buffered request-byte budget (not an RSS bound) |
| admission wait | 3s | Max wait for budget before a retryable rejection |
| waiter cap | 256 | Parked requests before fail-fast reject |
| per-IP receive cap | 8 | Concurrent receiving requests per source IP (/64) |
| request deadline | 16s | Server-side bound on one request |
| receive windows | 64 KiB / 16 MiB | Static stream/conn windows (BDP disabled) |

## References

- PROTOCO-3014 — request memory admission.
- PROTOCO-3351 — download response accounting.
- PROTOCO-2220, PROTOCO-2706 — connection limits.
- ADR-029 — Fibre upload occupancy (disk) limiting.
- `fibre/internal/grpc/{admission.go,server.go,keyed_counter.go}`.
