package grpc

import (
	"context"
	"io"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// stalledServer accepts plaintext HTTP/2 connections, grants each the largest
// flow-control windows, and stops reading once the first request starts. The
// connections stay open, so the peer looks alive but never drains.
func stalledServer(t *testing.T) net.Listener {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = lis.Close()
	})
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if stallAfterHeaders(conn) == nil {
					<-done
				}
			}()
		}
	}()
	return lis
}

func stallAfterHeaders(conn net.Conn) error {
	const maxWindow = 1<<31 - 1
	preface := make([]byte, len(http2Preface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return err
	}
	fr := http2.NewFramer(conn, conn)
	if err := fr.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: maxWindow}); err != nil {
		return err
	}
	if err := fr.WriteWindowUpdate(0, maxWindow-65535); err != nil {
		return err
	}
	for {
		f, err := fr.ReadFrame()
		if err != nil {
			return err
		}
		switch f := f.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				if err := fr.WriteSettingsAck(); err != nil {
					return err
				}
			}
		case *http2.HeadersFrame:
			return nil
		}
	}
}

// A validator that grants a large window and then stops reading blocks the
// client's writer inside Write. The queued row views stay reachable from it, so
// the GC fallback cannot release them, and the cancelled stream stays active
// because only the writer can remove it. When the channel then goes idle gRPC
// drains the transport instead of closing it, so not even closing the client
// reaches it: the client must give up on the stalled write itself.
func TestUploadCodecReleasesRowsAfterConnectionStalls(t *testing.T) {
	oldIdle, oldStall := clientIdleTimeout, writeStallTimeout
	clientIdleTimeout, writeStallTimeout = 500*time.Millisecond, 500*time.Millisecond
	t.Cleanup(func() { clientIdleTimeout, writeStallTimeout = oldIdle, oldStall })

	lis := stalledServer(t)

	// More than the loopback socket buffers hold, so the writer blocks with
	// most of the rows still queued behind it.
	const rowSize, rowCount = 1 << 20, 64
	pub := ed25519.GenPrivKey().PubKey()
	val := &core.Validator{Address: pub.Address(), PubKey: pub}
	newClient := DefaultNewClientFn(staticHostRegistry(lis.Addr().String()), func() string { return "test-chain" }, 2*rowSize*rowCount, nil)
	client, err := newClient(t.Context(), val)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	var refs atomic.Int32
	codec := NewUploadCodec(func() bool { refs.Add(1); return true }, func() { refs.Add(-1) })
	data := make([]byte, rowSize)
	rows := make([]*types.BlobRow, rowCount)
	for i := range rows {
		rows[i] = &types.BlobRow{Index: uint32(i), Data: data}
	}
	req := &types.UploadShardRequest{Shard: &types.BlobShard{Rows: rows}}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = client.UploadShard(ctx, req, grpc.ForceCodecV2(codec))
	require.Error(t, err)
	require.Positive(t, refs.Load(), "the stalled connection should hold queued rows")

	conn := client.(*fibreClientCloser).conn
	require.Eventually(t, func() bool { return conn.GetState() == connectivity.Idle }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, client.Close())
	require.Eventually(t, func() bool {
		runtime.GC()
		return refs.Load() == 0
	}, 10*time.Second, 50*time.Millisecond, "row references still held")
}
