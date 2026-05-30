package grpc

// SCRATCH / DRAFT — zero-interim-allocation receive path for DownloadShard.
//
// This file sketches a custom gRPC CodecV2 for the *download response* path,
// the read-side counterpart to PR #7191's send-side scatter-gather codec for
// UploadShardRequest. It is intentionally un-wired (see "WIRING" below) so it
// can be iterated on without touching the live download flow yet.
//
// ───────────────────────────────────────────────────────────────────────────
// GOAL
// ───────────────────────────────────────────────────────────────────────────
// On download the client is the *receiver* of the ~28 MiB BlobShard. The
// stock proto codec mints a fresh []byte per BlobRow.Data (and a contiguous
// per-message buffer) on every DownloadShard response, all of which become GC
// garbage the instant store()/reconstruct() are done with them. That transient
// churn — not the durable slab — is the remaining read-side allocation cost.
//
// The downloader already owns the hard part: a single pooled, ref-counted
// region (download.slab from DataPool, freed via Blob.Free). We want the wire
// bytes to flow into pool-backed memory with NO per-row heap garbage and NO
// full-message Materialize, and we want parity rows to be *used directly* from
// that pooled memory rather than copied — without preallocating the K+N (≈4×)
// parity space we mostly never fill (downloads pull ~1× original worth of
// randomized rows scattered across the K+N index space).
//
// ───────────────────────────────────────────────────────────────────────────
// TWO CONSTRAINTS THAT SHAPE THE DESIGN
// ───────────────────────────────────────────────────────────────────────────
//  1. VERIFY-BEFORE-STORE. Reconstructor.Add verifies the RLC proof against the
//     commitment *before* addVerified marks indices seen, and the "disjoint
//     index sets across concurrent Add" invariant only holds for *verified*
//     rows. A malicious peer can put an arbitrary index (one another peer owns)
//     in its response with garbage bytes. So we must NOT write row bytes into
//     the shared, reused download.slab during Unmarshal: two concurrent
//     responses could race on the same slot pre-verify. => the original-row
//     copy into the contiguous slab MUST stay in store(), post-verify. That
//     copy is therefore structural, not interim garbage — it is the final
//     destination write, and the slab must be contiguous for Blob.Data().
//     (This is exactly why the "decode straight into the slab" variant is
//     rejected; see REJECTED below.)
//
//  2. FRAGMENTED RECV BUFFERS. CodecV2.Unmarshal hands us a mem.BufferSlice —
//     a list of pooled transport buffers (~32 KiB each). A multi-MiB row spans
//     many of them, so it cannot be aliased zero-copy, and Materialize() would
//     allocate one giant contiguous buffer (the very thing we're removing).
//     => we stream-parse the wire format and copy each row's payload exactly
//     once, straight from the fragment reader into ONE pooled per-response
//     arena. Row.Data fields then alias slices of that arena. One copy, no
//     per-row allocs, no full-message materialize.
//
// ───────────────────────────────────────────────────────────────────────────
// RESULTING DATA FLOW (safe variant)
// ───────────────────────────────────────────────────────────────────────────
//   recv fragments ──(stream, 1 copy)──► per-response pooled arena
//       BlobRow.Data[i] = arena[off:off+len]            (alias, no alloc)
//   parseShard ──► RowProof.Row aliases arena            (no copy)
//   Reconstructor.Add ──► verify, dedup                  (reads arena)
//   store(novel):
//       original (idx < K) ──(1 copy)──► download.slab   (post-verify, durable)
//       parity   (idx ≥ K) ──► adopt arena slice by ptr  (no copy, no prealloc)
//   reconstruct() consumes parity (from arena) ──► fills missing originals
//   AFTER reconstruct: free every retained arena back to the pool
//
// Net vs today: per-row heap garbage removed (one pooled arena per response,
// freed deterministically); parity used in place with zero extra allocation
// and zero K+N preallocation. Originals keep their single post-verify slab
// copy, which constraint (1) makes mandatory.
//
// ───────────────────────────────────────────────────────────────────────────
// REJECTED: "decode straight into the slab" (eliminate the originals' copy)
// ───────────────────────────────────────────────────────────────────────────
// Writing original rows into download.slab during Unmarshal would skip the
// store() copy, but it writes UNVERIFIED bytes into shared, reused, pool-backed
// memory before Reconstructor.Add runs, and two concurrent responses can target
// the same slot (adversarial index collision) → data race + corruption. Making
// it safe would require per-slot locking or moving verification into the codec
// (wrong layer; needs RLC+commitment threaded in). The saving is one memcpy
// over only the K original rows — not worth the safety surface. Keep the copy
// in store().
//
// ───────────────────────────────────────────────────────────────────────────
// WIRING (applied)
// ───────────────────────────────────────────────────────────────────────────
// This decode is folded into the shared fibre-proto codec (codec.go), the same
// codec #7191 uses for upload scatter-gather — one content-subtype for all
// fibre messages, in both directions:
//  a) pooledCodec.Unmarshal routes a *DownloadReply here (arena decode); the
//     serve side marshals DownloadShardResponse via marshalDownloadShardResponseScatter.
//  b) fibreClientCloser.DownloadShardInto invokes the RPC with
//     grpclib.CallContentSubtype(codecName) and a *DownloadReply as the reply,
//     giving the decode somewhere to hand back the retained arena.
//  c) client_download.downloadFrom uses DownloadShardInto when the client
//     implements DownloadInto (else falls back to stock DownloadShard) and
//     passes the reply to download.AddShard, which retains it; download frees
//     all retained arenas in Blob() after reconstruct (parity rows alias them
//     until then). Error/skip paths free the arena immediately.
//
// NOTE: the wire walk below is structured for clarity over micro-optimization.
// Wire-parity and arena round-trip are fuzzed in codec_scatter_test.go.

import (
	"fmt"
	"io"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"google.golang.org/grpc/mem"
)

// Protobuf field tags (field<<3 | wireType) for the messages the arena decoder
// walks. wireType 2 = length-delimited (LEN), 0 = varint.
const (
	tagRespShard   = 1<<3 | 2 // DownloadShardResponse.shard
	tagShardRows   = 1<<3 | 2 // BlobShard.rows (repeated)
	tagShardCoeffs = 2<<3 | 2 // BlobShard.coefficients
	tagShardRoot   = 3<<3 | 2 // BlobShard.root
	tagRowIndex    = 1 << 3   // BlobRow.index (wiretype 0, varint)
	tagRowData     = 2<<3 | 2 // BlobRow.data
	tagRowProof    = 3<<3 | 2 // BlobRow.proof (repeated)
)

// DownloadReply is the reply object passed to Invoke in place of a bare
// *types.DownloadShardResponse. It carries the decoded response plus the pooled
// arena its row payloads alias, so the caller can release the arena once the
// rows are no longer needed (after reconstruct()).
type DownloadReply struct {
	Resp *types.DownloadShardResponse

	pool mem.BufferPool
	buf  *[]byte // pooled arena; row payloads alias slices of (*buf)
}

// Free returns the arena to the pool. Idempotent. MUST be called only after the
// download no longer reads any aliasing row (i.e. after reconstruct()).
func (d *DownloadReply) Free() {
	if d.buf != nil {
		d.pool.Put(d.buf)
		d.buf = nil
	}
}

// decodeDownloadShardResponse stream-decodes a DownloadShardResponse into
// reply.Resp, copying every row payload exactly once from the fragmented recv
// buffers into a single pooled arena and aliasing Row.Data/Proof/Coefficients
// into it. Invoked by pooledCodec.Unmarshal when the reply is a *DownloadReply.
func decodeDownloadShardResponse(data mem.BufferSlice, reply *DownloadReply) error {
	if data.Len() == 0 {
		reply.Resp = &types.DownloadShardResponse{}
		return nil
	}

	// One pooled arena, sized to the whole wire message. Sum of payload bytes is
	// strictly < data.Len() (tags/length-prefixes are excluded), so it always
	// fits. We never Materialize the BufferSlice.
	pool := mem.DefaultBufferPool()
	buf := pool.Get(data.Len())
	arena := &arena{buf: *buf}

	r := data.Reader()
	defer r.Close()

	resp := &types.DownloadShardResponse{}
	if err := decodeResponse(&wireReader{r}, arena, resp); err != nil {
		pool.Put(buf)
		return err
	}

	reply.Resp = resp
	reply.pool = pool
	reply.buf = buf
	return nil
}

// arena hands out contiguous sub-slices of a single pooled buffer. next(n)
// returns the buffer region the caller then fills via io.ReadFull — the row
// payload lands directly in pooled memory, aliased by the proto message, copied
// exactly once off the wire.
type arena struct {
	buf []byte
	off int
}

func (a *arena) next(n int) []byte {
	s := a.buf[a.off : a.off+n : a.off+n]
	a.off += n
	return s
}

// wireReader reads protobuf primitives directly off the concrete *mem.Reader.
// It deliberately avoids io.ByteReader/binary.ReadUvarint (which box the reader
// into an interface on every call) and io.LimitedReader (one heap alloc per
// nested message); budgets are tracked manually by the callers instead. uvarint
// returns the decoded value and the number of wire bytes it consumed.
type wireReader struct{ r *mem.Reader }

func (w *wireReader) uvarint() (val uint64, n int, err error) {
	var s uint
	for {
		b, err := w.r.ReadByte()
		if err != nil {
			return 0, n, err
		}
		n++
		if b < 0x80 {
			return val | uint64(b)<<s, n, nil
		}
		val |= uint64(b&0x7f) << s
		s += 7
	}
}

// fill reads exactly len(dst) bytes off the wire into dst.
func (w *wireReader) fill(dst []byte) error {
	_, err := io.ReadFull(w.r, dst)
	return err
}

// decodeResponse walks DownloadShardResponse { shard:1 }.
func decodeResponse(w *wireReader, a *arena, resp *types.DownloadShardResponse) error {
	for {
		tag, _, err := w.uvarint()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		ln, _, err := w.uvarint()
		if err != nil {
			return err
		}
		if tag != tagRespShard {
			return fmt.Errorf("fibre-dl: unexpected response tag %#x", tag)
		}
		resp.Shard = &types.BlobShard{}
		if err := decodeShard(w, a, resp.Shard, int(ln)); err != nil {
			return err
		}
	}
}

// decodeShard walks BlobShard { rows:1 (repeated), coefficients:2, root:3 }
// over exactly `budget` wire bytes.
func decodeShard(w *wireReader, a *arena, shard *types.BlobShard, budget int) error {
	for budget > 0 {
		tag, nt, err := w.uvarint()
		if err != nil {
			return err
		}
		ln, nl, err := w.uvarint()
		if err != nil {
			return err
		}
		budget -= nt + nl + int(ln)
		switch tag {
		case tagShardRows:
			row := &types.BlobRow{}
			if err := decodeRow(w, a, row, int(ln)); err != nil {
				return err
			}
			shard.Rows = append(shard.Rows, row)
		case tagShardCoeffs:
			shard.Coefficients = a.next(int(ln))
			if err := w.fill(shard.Coefficients); err != nil {
				return err
			}
		case tagShardRoot:
			shard.Root = a.next(int(ln))
			if err := w.fill(shard.Root); err != nil {
				return err
			}
		default:
			return fmt.Errorf("fibre-dl: unexpected shard tag %#x", tag)
		}
	}
	return nil
}

// decodeRow walks BlobRow { index:1, data:2, proof:3 (repeated) } over exactly
// `budget` wire bytes. The big `data` field is read straight into the arena —
// the single copy off the wire.
func decodeRow(w *wireReader, a *arena, row *types.BlobRow, budget int) error {
	for budget > 0 {
		tag, nt, err := w.uvarint()
		if err != nil {
			return err
		}
		switch tag {
		case tagRowIndex:
			idx, nv, err := w.uvarint()
			if err != nil {
				return err
			}
			row.Index = uint32(idx)
			budget -= nt + nv
		case tagRowData:
			ln, nl, err := w.uvarint()
			if err != nil {
				return err
			}
			row.Data = a.next(int(ln)) // alias arena; filled below, one copy
			if err := w.fill(row.Data); err != nil {
				return err
			}
			budget -= nt + nl + int(ln)
		case tagRowProof:
			ln, nl, err := w.uvarint()
			if err != nil {
				return err
			}
			if row.Proof == nil {
				row.Proof = make([][]byte, 0, 16) // typical Merkle proof depth
			}
			seg := a.next(int(ln))
			if err := w.fill(seg); err != nil {
				return err
			}
			row.Proof = append(row.Proof, seg)
			budget -= nt + nl + int(ln)
		default:
			return fmt.Errorf("fibre-dl: unexpected row tag %#x", tag)
		}
	}
	return nil
}
