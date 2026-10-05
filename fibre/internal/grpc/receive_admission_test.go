package grpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
)

// TestReceiveAdmissionBeforePayload pins the transport methods used by admission.
func TestReceiveAdmissionBeforePayload(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	admission := NewAdmission(1, 0, 8<<20, 4096, 14)
	server := grpc.NewServer(grpc.ForceServerCodecV2(admission.codec))
	admission.register(server, &types.UnimplementedFibreServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = io.WriteString(conn, http2.ClientPreface)
	require.NoError(t, err)
	framer := http2.NewFramer(conn, conn)
	framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	require.NoError(t, framer.WriteSettings())
	var headers bytes.Buffer
	encoder := hpack.NewEncoder(&headers)
	for _, field := range []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/celestia.fibre.v1.Fibre/UploadShard"},
		{Name: ":authority", Value: listener.Addr().String()},
		{Name: "content-type", Value: "application/grpc+fibre-proto"},
		{Name: "te", Value: "trailers"},
	} {
		require.NoError(t, encoder.WriteField(field))
	}
	require.NoError(t, framer.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: headers.Bytes(), EndHeaders: true}))
	// Only send the message header; the advertised 1 MiB body never arrives.
	header := binary.BigEndian.AppendUint32([]byte{0}, 1<<20)
	require.NoError(t, framer.WriteData(1, false, header))
	for {
		frame, err := framer.ReadFrame()
		require.NoError(t, err)
		if headers, ok := frame.(*http2.MetaHeadersFrame); ok {
			for _, field := range headers.Fields {
				if field.Name == "grpc-status" {
					require.Equal(t, "8", field.Value)
					return
				}
			}
		}
	}
}
