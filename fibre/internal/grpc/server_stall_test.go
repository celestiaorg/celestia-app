package grpc

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// bigShardServer answers every DownloadShard with a shard larger than the
// loopback socket buffers hold.
type bigShardServer struct {
	types.UnimplementedFibreServer
	served chan struct{}
}

func (s *bigShardServer) DownloadShard(context.Context, *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	const rowSize, rowCount = 1 << 20, 64
	data := make([]byte, rowSize)
	rows := make([]*types.BlobRow, rowCount)
	for i := range rows {
		rows[i] = &types.BlobRow{Index: uint32(i), Data: data}
	}
	close(s.served)
	return &types.DownloadShardResponse{Shard: &types.BlobShard{Rows: rows}}, nil
}

// requestDownloadAndStall sends one DownloadShard request on a raw HTTP/2
// connection that grants the largest windows, then never reads and keeps
// sending PRIORITY frames, which need no reply, until done is closed.
func requestDownloadAndStall(t *testing.T, conn net.Conn, done <-chan struct{}) {
	t.Helper()
	const maxWindow = 1<<31 - 1
	_, err := conn.Write([]byte(http2Preface))
	require.NoError(t, err)
	fr := http2.NewFramer(conn, nil)
	require.NoError(t, fr.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: maxWindow}))
	require.NoError(t, fr.WriteWindowUpdate(0, maxWindow-65535))

	var hdr bytes.Buffer
	enc := hpack.NewEncoder(&hdr)
	for _, f := range [][2]string{
		{":method", "POST"},
		{":scheme", "http"},
		{":path", "/" + types.Fibre_serviceDesc.ServiceName + "/DownloadShard"},
		{":authority", "fibre"},
		{"content-type", "application/grpc"},
		{"te", "trailers"},
	} {
		require.NoError(t, enc.WriteField(hpack.HeaderField{Name: f[0], Value: f[1]}))
	}
	require.NoError(t, fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: hdr.Bytes(), EndHeaders: true}))
	// An empty request: uncompressed, zero length.
	require.NoError(t, fr.WriteData(1, true, []byte{0, 0, 0, 0, 0}))

	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				if fr.WritePriority(1, http2.PriorityParam{Weight: 15}) != nil {
					return
				}
			}
		}
	}()
}

// A downloader that stops reading blocks the server's writer, which keeps the
// queued response in memory after the handler has returned and released its
// admission. The write deadline must close the connection, even though the
// peer keeps sending frames.
func TestServerClosesConnectionWhoseReaderStalls(t *testing.T) {
	defer func(d time.Duration) { writeStallTimeout = d }(writeStallTimeout)
	writeStallTimeout = 200 * time.Millisecond

	srv, err := Listen("127.0.0.1:0", 0, DefaultMaxConcurrentStreams, testAdmission)
	require.NoError(t, err)
	service := &bigShardServer{served: make(chan struct{})}
	srv.Register(service)
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })

	conn, err := net.Dial("tcp", srv.ListenAddress())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	requestDownloadAndStall(t, conn, done)

	select {
	case <-service.served:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}
	require.Eventually(t, func() bool {
		return srv.Stats().OpenConns == 0
	}, 5*time.Second, 20*time.Millisecond, "stalled connection still open")
}
