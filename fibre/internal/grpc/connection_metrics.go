package grpc

import (
	"context"
	"errors"
	"net"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type connectionMetrics struct {
	active   metric.Int64UpDownCounter
	sent     metric.Int64Counter
	received metric.Int64Counter
}

func newConnectionMetrics(m metric.Meter) (*connectionMetrics, error) {
	active, e1 := m.Int64UpDownCounter("fibre.transport.connections", metric.WithDescription("Open TCP connections by local IP; includes TLS handshakes"))
	sent, e2 := m.Int64Counter("fibre.transport.sent", metric.WithUnit("By"), metric.WithDescription("Bytes written to TCP, including TLS and HTTP/2 overhead; excludes TCP/IP headers and retransmissions"))
	received, e3 := m.Int64Counter("fibre.transport.received", metric.WithUnit("By"), metric.WithDescription("Bytes read from TCP, including TLS and HTTP/2 overhead; excludes TCP/IP headers and retransmissions"))
	if err := errors.Join(e1, e2, e3); err != nil {
		return nil, err
	}
	return &connectionMetrics{active: active, sent: sent, received: received}, nil
}

func addressIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "unknown"
	}
	return host
}

func (m *connectionMetrics) wrap(c net.Conn, role string) net.Conn {
	attrs := []attribute.KeyValue{attribute.String("role", role), attribute.String("local_ip", addressIP(c.LocalAddr()))}
	// Server peers are untrusted and may have unbounded address cardinality.
	if role == "client" {
		attrs = append(attrs, attribute.String("remote_ip", addressIP(c.RemoteAddr())))
	}
	conn := &meteredConn{Conn: c, metrics: m, options: metric.WithAttributeSet(attribute.NewSet(attrs...))}
	m.active.Add(context.Background(), 1, conn.options)
	return conn
}

type meteredConn struct {
	net.Conn
	metrics *connectionMetrics
	options metric.MeasurementOption
	closed  atomic.Bool
}

func (c *meteredConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.metrics.received.Add(context.Background(), int64(n), c.options)
	}
	return n, err
}

func (c *meteredConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.metrics.sent.Add(context.Background(), int64(n), c.options)
	}
	return n, err
}

func (c *meteredConn) Close() error {
	err := c.Conn.Close()
	if !c.closed.Swap(true) {
		c.metrics.active.Add(context.Background(), -1, c.options)
	}
	return err
}

type meteredListener struct {
	net.Listener
	metrics *connectionMetrics
}

func (l *meteredListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.metrics.wrap(c, "server"), nil
}
