package grpc

import (
	"context"
	"net"
	"sync"

	"google.golang.org/grpc/credentials"
)

// uploadWriteBufferSize is the gRPC write buffer for client connections. With
// [batchingCreds] one buffer flush becomes one socket write instead of one per
// 16 KiB TLS record.
const uploadWriteBufferSize = 256 << 10

// batchingCreds wraps TLS credentials so the records produced by one Write
// reach the socket in a single syscall.
type batchingCreds struct {
	credentials.TransportCredentials
}

func (c batchingCreds) ClientHandshake(ctx context.Context, authority string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	bc := &batchConn{Conn: raw}
	conn, info, err := c.TransportCredentials.ClientHandshake(ctx, authority, bc)
	if err != nil {
		return nil, nil, err
	}
	return &flushConn{Conn: conn, bc: bc}, info, nil
}

func (c batchingCreds) Clone() credentials.TransportCredentials {
	return batchingCreds{c.TransportCredentials.Clone()}
}

// batchConn sits below TLS. While holding, it buffers writes instead of
// sending them.
type batchConn struct {
	net.Conn

	mu   sync.Mutex
	hold bool
	buf  []byte
}

func (b *batchConn) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hold {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	return b.Conn.Write(p)
}

func (b *batchConn) start() {
	b.mu.Lock()
	b.hold = true
	b.mu.Unlock()
}

// flush stops holding and writes the buffered records.
func (b *batchConn) flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hold = false
	if len(b.buf) == 0 {
		return nil
	}
	_, err := b.Conn.Write(b.buf)
	b.buf = b.buf[:0]
	return err
}

// flushConn sits above TLS and flushes the records of each Write.
type flushConn struct {
	net.Conn
	bc *batchConn
}

func (f *flushConn) Write(p []byte) (int, error) {
	f.bc.start()
	n, err := f.Conn.Write(p)
	if ferr := f.bc.flush(); err == nil {
		err = ferr
	}
	return n, err
}
