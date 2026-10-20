# ADR 031: Fibre Request Memory Admission

## Changelog

- 2026-10-19: Initial version

## Status

Proposed

## Context

gRPC buffers a whole request message (up to `MaxRecvMsgSize` ≈ 132 MiB for Fibre) before the handler runs, and Fibre's RPCs (`UploadShard`, `DownloadShard`, gRPC health) are unauthenticated at that point. Receive memory was bounded only by `max_connections × max_concurrent_streams × 132 MiB`, coupling memory to connection caps that are themselves a pre-auth DoS.

## Decision

Admit every request against a global FIFO byte budget (`max_request_memory`, default 16 GiB) before its body is read.

- **Unary methods as stream handlers.** Every unary method of the Fibre and health services is registered as a stream handler that admits, then runs the generated handler, whose decode step reads the body. Clients are unchanged. Streaming methods are not registered (health `Watch` is unsupported anyway), so grpc rejects them before reading a body.
- **Reserve max, then shrink.** A request reserves one max message, since its size is unknown until decode, then shrinks to the decoded size and holds that until the handler returns.
- **Downloads hold the max.** `DownloadShard` keeps its max reservation until the handler returns, so the shard it reads from storage and the response it builds are counted. The reservation is tied to the handler, not the stream context: a client reset cancels the context while the store read is still running.
- **Bounded wait.** A request waits up to 3s for budget, then gets `ResourceExhausted` with a ~1–2s `RetryInfo`. Past 256 waiters it is rejected immediately.
- **Per-IP receive cap.** At most 8 requests per source IP (IPv6 by /64) may be admitted and still receiving, so one source cannot pin the budget with stalled bodies.
- **Request deadline.** An `InTapHandle` deadline of 60s per stream releases a stalled sender's reservation. It is a backstop, well above the client's 15s `RPCTimeout`, so slow honest max-size uploads are not cut off.
- **Static receive windows** (64 KiB stream / 16 MiB connection) disable BDP tuning; a paused stream holds at most its window.
- Rejections are counted on `fibre.server.request.rejected{method,reason}` with reasons `per_ip`, `memory_timeout`, `memory_overflow`.

## Consequences

- Receive memory is bounded by the budget, independent of the connection count; oversized requests on any method are gated before they are read.
- `max_request_memory` bounds buffered request bytes, not RSS: decode and verification allocate on top, so it is set well below host RAM.
- Every upload costs a full max message of budget while it is received.
- Connection caps now only bound concurrency.
- Download send tail: unary `SendMsg` queues the whole response and returns, so the reservation is released while a slow reader is still draining it. Those bytes sit in grpc's send buffer outside the budget, bounded by flow control and `MaxConnectionAge`. Chunked (server-streaming) downloads would close this; that is separate work.

## Alternatives

- **Tighter connection caps** keep memory coupled to connections and remain a pre-auth DoS.
- **Admission in `InTapHandle`** would run on the connection's reader goroutine, which must not block; the tap only sets the deadline.
- **Reserving by a declared size** before decode is unenforceable without authenticating the sender first.

## References

- PROTOCO-3014: request memory admission.
- PROTOCO-3351: download response accounting.
- ADR-029: Fibre upload occupancy (disk) limiting.
- `fibre/internal/grpc/admission.go`, `fibre/internal/grpc/server.go`.
