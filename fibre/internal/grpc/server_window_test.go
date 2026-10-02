package grpc

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestServerReceiveWindows(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", 16, 13, 20, 8, false)
	require.NoError(t, err)
	srv.Register(nil)
	srv.Serve()
	defer srv.Stop(context.Background())
	conn, err := net.Dial("tcp", srv.ListenAddress())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = io.WriteString(conn, http2.ClientPreface)
	require.NoError(t, err)
	framer := http2.NewFramer(conn, conn)
	require.NoError(t, framer.WriteSettings())
	connectionWindow := uint32(65535)
	var streamWindow uint32
	for {
		frame, err := framer.ReadFrame()
		require.NoError(t, err)
		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if f.IsAck() {
				require.EqualValues(t, 1<<20, streamWindow)
				require.EqualValues(t, 13<<20, connectionWindow)
				return
			}
			setting, ok := f.Value(http2.SettingInitialWindowSize)
			require.True(t, ok)
			streamWindow = setting
			require.NoError(t, framer.WriteSettingsAck())
		case *http2.WindowUpdateFrame:
			if f.StreamID == 0 {
				connectionWindow += f.Increment
			}
		}
	}
}
