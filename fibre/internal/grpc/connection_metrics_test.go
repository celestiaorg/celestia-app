package grpc

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type partialConn struct {
	net.Conn
	local, remote string
}

func (c *partialConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.ParseIP(c.local), Port: 1234} }
func (c *partialConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(c.remote), Port: 5678}
}
func (*partialConn) Read(p []byte) (int, error) { return copy(p, "abc"), io.EOF }
func (*partialConn) Write([]byte) (int, error)  { return 2, io.ErrClosedPipe }
func (*partialConn) Close() error               { return nil }

func TestConnectionMetrics(t *testing.T) {
	for _, role := range []string{"client", "server"} {
		t.Run(role, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			metrics, err := newConnectionMetrics(provider.Meter("test"))
			require.NoError(t, err)
			first := metrics.wrap(&partialConn{local: "10.0.0.1", remote: "10.0.0.2"}, role)
			second := metrics.wrap(&partialConn{local: "10.1.0.1", remote: "10.1.0.2"}, role)
			n, err := first.Write(make([]byte, 10))
			require.Equal(t, 2, n)
			require.ErrorIs(t, err, io.ErrClosedPipe)
			n, err = second.Read(make([]byte, 10))
			require.Equal(t, 3, n)
			require.ErrorIs(t, err, io.EOF)
			assertMetrics := func(want map[string]map[string]int64) {
				t.Helper()
				var data metricdata.ResourceMetrics
				require.NoError(t, reader.Collect(context.Background(), &data))
				got := map[string]map[string]int64{}
				for _, scope := range data.ScopeMetrics {
					for _, m := range scope.Metrics {
						sum, ok := m.Data.(metricdata.Sum[int64])
						require.True(t, ok)
						got[m.Name] = map[string]int64{}
						for _, point := range sum.DataPoints {
							local, _ := point.Attributes.Value("local_ip")
							r, _ := point.Attributes.Value("role")
							require.Equal(t, role, r.AsString())
							_, hasRemote := point.Attributes.Value("remote_ip")
							require.Equal(t, role == "client", hasRemote)
							wantAttributes := 2
							if role == "client" {
								wantAttributes++
							}
							require.Equal(t, wantAttributes, point.Attributes.Len())
							got[m.Name][local.AsString()] = point.Value
						}
					}
				}
				require.Equal(t, want, got)
			}
			want := map[string]map[string]int64{
				"fibre.transport.connections": {"10.0.0.1": 1, "10.1.0.1": 1},
				"fibre.transport.sent":        {"10.0.0.1": 2},
				"fibre.transport.received":    {"10.1.0.1": 3},
			}
			assertMetrics(want)
			require.NoError(t, first.Close())
			require.NoError(t, first.Close())
			want["fibre.transport.connections"]["10.0.0.1"] = 0
			assertMetrics(want)
			require.NoError(t, second.Close())
			want["fibre.transport.connections"]["10.1.0.1"] = 0
			assertMetrics(want)
		})
	}
}
