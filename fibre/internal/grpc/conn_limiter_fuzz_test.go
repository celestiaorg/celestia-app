package grpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"unsafe"

	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
)

// connOpWidth is the fixed width of one listener op record.
const connOpWidth = 8

type fuzzAddr string

func (a fuzzAddr) Network() string { return "tcp" }
func (a fuzzAddr) String() string  { return string(a) }

// fuzzConn records how often the listener or a caller closed it.
type fuzzConn struct {
	net.Conn
	addr   net.Addr
	closes int
}

func (c *fuzzConn) RemoteAddr() net.Addr { return c.addr }
func (c *fuzzConn) Close() error         { c.closes++; return nil }

// fuzzListener hands out queued connections, then fails, so Accept returns
// after it has dropped every rejected connection.
type fuzzListener struct{ queue []*fuzzConn }

var errListenerDrained = errors.New("drained")

func (l *fuzzListener) Accept() (net.Conn, error) {
	if len(l.queue) == 0 {
		return nil, errListenerDrained
	}
	c := l.queue[0]
	l.queue = l.queue[1:]
	return c, nil
}
func (l *fuzzListener) Close() error   { return nil }
func (l *fuzzListener) Addr() net.Addr { return fuzzAddr("127.0.0.1:9090") }

// fuzzRemoteAddr builds a peer address from a record and returns it with the
// bucket it must fall in: an IPv4 address in its dotted or IPv4-mapped IPv6
// form, an IPv6 address in one of two /64s in compressed or upper-case
// expanded form, a non-IP address, or none at all.
func fuzzRemoteAddr(r *fuzzutil.Consumer) (net.Addr, string) {
	kind, sel, b := r.Byte()%6, r.Byte(), [4]byte{r.Byte(), r.Byte(), r.Byte(), r.Byte()}
	v4 := netip.AddrFrom4([4]byte{10, 0, b[0] % 4, b[1] % 4})
	port := fmt.Sprintf("%d", 1024+int(b[2]))
	switch kind {
	case 0:
		return fuzzAddr(net.JoinHostPort(v4.String(), port)), v4.String()
	case 1:
		mapped := netip.AddrFrom16(v4.As16())
		return fuzzAddr(net.JoinHostPort(mapped.String(), port)), v4.String()
	case 2, 3:
		var ip [16]byte
		copy(ip[:], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, sel % 2})
		ip[8], ip[15] = b[0]%4, b[1]%4
		addr := netip.AddrFrom16(ip)
		host := addr.String()
		if kind == 3 {
			host = strings.ToUpper(addr.StringExpanded())
		}
		key := netip.PrefixFrom(addr, 64).Masked().Addr().String()
		return fuzzAddr(net.JoinHostPort(host, port)), key
	case 4:
		name := fmt.Sprintf("pipe-%d", sel%2)
		return fuzzAddr(name), name
	default:
		return nil, ""
	}
}

// FuzzLimitedListenerSequence drives the connection limiter with accepts and
// repeated closes from IPv4, IPv4-mapped, IPv6 and non-IP peers. A model keyed
// by an independent bucketing (IPv4 exact, IPv6 by /64) predicts every
// accept; the open count, per-key slots and rejection counters must match it
// after each step, every rejected connection is closed exactly once, and
// closing a connection more than once frees its slot only once.
func FuzzLimitedListenerSequence(f *testing.F) {
	accept := func(kind, sel, a, b byte) []byte { return []byte{0, kind, sel, a, b} }
	closeConn := func(idx, times byte) []byte { return []byte{1, idx, times} }
	f.Add(uint8(7), uint8(2), []byte{})
	// One IPv4 peer in both forms hits its cap of two, then a close frees one slot.
	f.Add(uint8(7), uint8(2), fuzzutil.Records(connOpWidth,
		accept(0, 0, 1, 1), accept(1, 0, 1, 1), accept(0, 0, 1, 1), closeConn(0, 3), accept(1, 0, 1, 1)))
	// Two IPv6 peers in one /64 share a slot; a peer in the other /64 does not.
	f.Add(uint8(7), uint8(1), fuzzutil.Records(connOpWidth,
		accept(2, 0, 0, 0), accept(3, 0, 1, 1), accept(2, 1, 1, 1), accept(5, 0, 0, 0), accept(4, 0, 0, 0), accept(4, 0, 0, 0)))
	// The global cap rejects before the per-IP cap.
	f.Add(uint8(0), uint8(0), fuzzutil.Records(connOpWidth,
		accept(0, 0, 0, 0), accept(5, 0, 0, 0), closeConn(0, 2), accept(2, 0, 0, 0)))

	f.Fuzz(func(t *testing.T, globalMax, perIPMax uint8, ops []byte) {
		gmax, pmax := int64(globalMax%8)+1, int(perIPMax%4) // perIP 0 is unlimited
		inner := &fuzzListener{}
		l := newLimitedListener(inner, gmax, pmax)

		type liveConn struct {
			conn  net.Conn
			inner *fuzzConn
			key   string
		}
		var live []*liveConn
		var open int64
		var rejGlobal, rejPerIP int64
		slots := map[string]int{}

		c := fuzzutil.New(ops)
		for n := 0; n < 128 && !c.Exhausted(); n++ {
			r := c.Record(connOpWidth)
			switch r.Byte() % 2 {
			case 0:
				addr, key := fuzzRemoteAddr(r)
				fc := &fuzzConn{addr: addr}
				inner.queue = append(inner.queue, fc)
				wantGlobal := open < gmax
				wantOK := wantGlobal && (pmax == 0 || key == "" || slots[key] < pmax)
				conn, err := l.Accept()
				if !wantOK {
					require.ErrorIs(t, err, errListenerDrained, "accepted a connection over the cap")
					require.Equal(t, 1, fc.closes, "rejected connection must be closed once")
					if wantGlobal {
						rejPerIP++
					} else {
						rejGlobal++
					}
					break
				}
				require.NoError(t, err)
				require.Zero(t, fc.closes)
				open++
				if pmax > 0 && key != "" {
					slots[key]++
				}
				live = append(live, &liveConn{conn: conn, inner: fc, key: key})
			case 1:
				if len(live) == 0 {
					break
				}
				i := int(r.Byte()) % len(live)
				lc, times := live[i], int(r.Byte()%3)+1
				for range times {
					require.NoError(t, lc.conn.Close())
				}
				require.Equal(t, times, lc.inner.closes, "every Close must reach the inner connection")
				open--
				if pmax > 0 && lc.key != "" {
					if slots[lc.key]--; slots[lc.key] == 0 {
						delete(slots, lc.key)
					}
				}
				live = append(live[:i], live[i+1:]...)
			}

			require.Equal(t, open, l.open.Load(), "open connections diverged")
			require.LessOrEqual(t, l.open.Load(), gmax)
			require.Equal(t, rejGlobal, l.rejectedGlobal.Load())
			require.Equal(t, rejPerIP, l.rejectedPerIP.Load())
			l.perIP.mu.Lock()
			require.Equal(t, slots, l.perIP.m, "per-IP slots diverged")
			l.perIP.mu.Unlock()
		}

		for _, lc := range live {
			require.NoError(t, lc.conn.Close())
		}
		require.Zero(t, l.open.Load(), "leaked a global slot")
		require.Empty(t, l.perIP.m, "leaked a per-IP slot")
	})
}

// reuseOpWidth is the fixed width of one upload buffer op record.
const reuseOpWidth = 4

// bufferSpan returns the address range of buf's backing array.
func bufferSpan(buf []byte) (uintptr, uintptr) {
	start := uintptr(unsafe.Pointer(unsafe.SliceData(buf)))
	return start, start + uintptr(cap(buf))
}

// FuzzUploadBufferReuse decodes uploads of at least minUploadBufferSize bytes
// through the reusing server codec, interleaved with malformed uploads and
// repeated releases. No two live requests and no live request and idle buffer
// may share backing memory, every live request must still decode to the bytes
// it was sent, idle memory stays within the limit, and releasing everything
// leaves no active buffer.
func FuzzUploadBufferReuse(f *testing.F) {
	decode := func(extra, corrupt byte) []byte { return []byte{0, extra, corrupt} }
	release := func(idx, times byte) []byte { return []byte{1, idx, times} }
	f.Add(uint8(2), []byte{})
	f.Add(uint8(2), fuzzutil.Records(reuseOpWidth,
		decode(0, 0), release(0, 1), decode(3, 0), decode(1, 0), release(1, 2), decode(0, 0)))
	// A malformed upload returns its buffer, which the next upload reuses.
	f.Add(uint8(4), fuzzutil.Records(reuseOpWidth,
		decode(0, 1), decode(0, 0), decode(2, 1), decode(1, 0), release(0, 0), release(0, 0)))

	f.Fuzz(func(t *testing.T, limitMiB uint8, ops []byte) {
		codec := NewServerCodec(testMaxRows, testMaxProofs).(*pooledCodec)
		limit := (int(limitMiB%4) + 1) * (minUploadBufferSize + 4096)
		codec.uploads = &uploadBuffers{limit: limit}

		type liveReq struct {
			req  *types.UploadShardRequest
			want []byte
		}
		var live []*liveReq
		var fill byte

		c := fuzzutil.New(ops)
		for n := 0; n < 24 && !c.Exhausted(); n++ {
			r := c.Record(reuseOpWidth)
			switch r.Byte() % 2 {
			case 0:
				fill++
				data := bytes.Repeat([]byte{fill}, minUploadBufferSize+int(r.Byte()%4)*1024)
				wire := marshalUploadShard(t, &types.UploadShardRequest{Shard: &types.BlobShard{
					Rows: []*types.BlobRow{{Index: 1, Data: data, Proof: [][]byte{make([]byte, 32)}}},
					Rlcs: make([]byte, 16),
				}})
				corrupt := r.Byte()%2 == 1
				if corrupt {
					wire = wire[:len(wire)-1] // truncated: decoding fails after the buffer is taken
				}
				var got types.UploadShardRequest
				err := codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(wire)}, &got)
				if corrupt || len(wire) > limit {
					require.Error(t, err)
					break
				}
				require.NoError(t, err)
				live = append(live, &liveReq{req: &got, want: data})
			case 1:
				if len(live) == 0 {
					break
				}
				i := int(r.Byte()) % len(live)
				for range int(r.Byte()%2) + 1 {
					codec.uploads.release(live[i].req)
				}
				live = append(live[:i], live[i+1:]...)
			}

			type span struct{ lo, hi uintptr }
			codec.uploads.mu.Lock()
			spans := make([]span, 0, len(live)+len(codec.uploads.idle))
			require.Len(t, codec.uploads.active, len(live), "active buffers diverged from live requests")
			for _, lr := range live {
				buf, ok := codec.uploads.active[lr.req]
				require.True(t, ok, "live request lost its buffer")
				lo, hi := bufferSpan(buf)
				spans = append(spans, span{lo, hi})
			}
			idle := 0
			for _, buf := range codec.uploads.idle {
				lo, hi := bufferSpan(buf)
				spans = append(spans, span{lo, hi})
				idle += cap(buf)
			}
			require.Equal(t, idle, codec.uploads.idleBytes)
			require.LessOrEqual(t, idle, limit, "idle buffers exceed the limit")
			codec.uploads.mu.Unlock()
			for i := range spans {
				for j := range i {
					require.False(t, spans[i].lo < spans[j].hi && spans[j].lo < spans[i].hi, "two buffers share backing memory")
				}
			}
			for _, lr := range live {
				require.Equal(t, lr.want, lr.req.Shard.Rows[0].Data, "a live request's payload was overwritten")
			}
		}

		for _, lr := range live {
			_, err := codec.uploads.intercept(context.Background(), lr.req, nil, func(context.Context, any) (any, error) { return nil, nil })
			require.NoError(t, err)
		}
		require.Empty(t, codec.uploads.active)
	})
}
