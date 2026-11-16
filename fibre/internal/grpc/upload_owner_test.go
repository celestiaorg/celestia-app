package grpc

import (
	"bytes"
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/mem"
)

// Every marshaled row view keeps one owner reference until gRPC frees it; the
// wire bytes are unchanged.
func TestUploadCodecHoldsOwnerUntilViewsFreed(t *testing.T) {
	req := &types.UploadShardRequest{Shard: &types.BlobShard{
		Rows: []*types.BlobRow{
			{Index: 1, Data: bytes.Repeat([]byte{1}, 4096), Proof: [][]byte{bytes.Repeat([]byte{9}, 32)}},
			{Index: 2, Data: bytes.Repeat([]byte{2}, 64)}, // below the pooling threshold: copied
			{Index: 3, Data: bytes.Repeat([]byte{3}, 4096)},
		},
		Rlcs: bytes.Repeat([]byte{7}, 2048),
	}}
	want, err := req.Marshal()
	require.NoError(t, err)

	refs := 0
	codec := NewUploadCodec(func() bool { refs++; return true }, func() { refs-- })

	first, err := codec.Marshal(req)
	require.NoError(t, err)
	second, err := codec.Marshal(req) // a retry marshals again
	require.NoError(t, err)
	require.Equal(t, want, first.Materialize())
	require.Equal(t, 4, refs) // two pooled rows per marshal

	// The 64-byte row was copied, not aliased.
	req.Shard.Rows[1].Data[0] = 0xff
	require.Equal(t, want, first.Materialize())

	// Each pooled row view holds its own reference: freeing one view at a time
	// drops exactly one, and gRPC's extra Ref on a view defers its drop.
	var owned []mem.Buffer
	for _, b := range first {
		if _, plain := b.(*mem.SliceBuffer); plain {
			b.Free()
		} else {
			owned = append(owned, b)
		}
	}
	require.Len(t, owned, 2)
	require.Equal(t, 4, refs)
	owned[0].Ref()
	owned[0].Free()
	require.Equal(t, 4, refs)
	owned[0].Free()
	require.Equal(t, 3, refs)
	owned[1].Free()
	require.Equal(t, 2, refs)
	second.Free()
	require.Zero(t, refs)
}

// A failed retain frees the views wrapped so far, dropping their references.
func TestUploadCodecFailsAfterRelease(t *testing.T) {
	req := &types.UploadShardRequest{Shard: &types.BlobShard{Rows: []*types.BlobRow{
		{Data: make([]byte, 4096)}, {Data: make([]byte, 4096)},
	}}}
	refs, retains := 0, 0
	codec := NewUploadCodec(func() bool {
		if retains++; retains > 1 {
			return false
		}
		refs++
		return true
	}, func() { refs-- })
	_, err := codec.Marshal(req)
	require.ErrorIs(t, err, errUploadReleased)
	require.Zero(t, refs)
}

// gRPC never frees data frames still queued on a stream when the connection
// dies, so the references they hold are dropped by GC cleanup instead.
func TestUploadCodecReleasesRowsAfterConnectionDies(t *testing.T) {
	// One blocked upload holds the whole budget, so the next one is parked in
	// the tap handler and its body stays queued in the client transport.
	const msg = 1 << 20
	svc := &fakeFibre{entered: make(chan struct{}, 1), release: make(chan struct{})}
	_, conn := startServer(t, AdmissionConfig{Budget: msg, MaxMessage: msg}, svc)
	c := types.NewFibreClient(conn)
	defer close(svc.release)

	go func() { _, _ = c.UploadShard(context.Background(), uploadReq(1)) }()
	<-svc.entered

	var refs atomic.Int32
	codec := NewUploadCodec(func() bool { refs.Add(1); return true }, func() { refs.Add(-1) })
	rows := make([]*types.BlobRow, 8)
	for i := range rows {
		rows[i] = &types.BlobRow{Index: uint32(i), Data: make([]byte, 128<<10)}
	}
	req := &types.UploadShardRequest{Shard: &types.BlobShard{Rows: rows}}

	done := make(chan error, 1)
	go func() {
		_, err := c.UploadShard(context.Background(), req, grpc.ForceCodecV2(codec))
		done <- err
	}()
	require.Eventually(t, func() bool { return refs.Load() == int32(len(rows)) }, 5*time.Second, time.Millisecond)

	require.NoError(t, conn.Close())
	require.Error(t, <-done)
	require.Eventually(t, func() bool {
		runtime.GC()
		return refs.Load() == 0
	}, 5*time.Second, 10*time.Millisecond)
}
