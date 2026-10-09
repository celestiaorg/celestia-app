package grpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

// transportStream exposes methods on the pinned gRPC transport's internal concrete type.
// gRPC has no public API to read the message length before allocating its body.
type transportStream interface {
	ReadMessageHeader([]byte) error
	RecvCompress() string
	SendCompress() string
}

type receiveTimerKey struct{}

var errReceiveTimeout = status.Error(codes.ResourceExhausted, "request receive timeout")

// receiveTimeoutTap installs cancellation before gRPC creates its transport reader.
// The timer stops after receipt, unlike a context deadline that also limits handler work.
func receiveTimeoutTap(ctx context.Context, _ *tap.Info) (context.Context, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	var once sync.Once
	timer := time.AfterFunc(receiveTimeout, func() { once.Do(func() { cancel(errReceiveTimeout) }) })
	stop := func() {
		// Wait for cancellation if it started; otherwise prevent it before handler work.
		once.Do(func() {})
		timer.Stop()
	}
	context.AfterFunc(ctx, stop)
	return context.WithValue(ctx, receiveTimerKey{}, stop), nil
}

// receive reserves uploads before reading their bodies and downloads before their storage reads.
func (a *Admission) receive(ctx context.Context, target any, lease *memoryLease) (err error) {
	stopReceive := ctx.Value(receiveTimerKey{}).(func())
	defer func() {
		stopReceive()
		if context.Cause(ctx) == errReceiveTimeout {
			err = errReceiveTimeout
		}
	}()
	reader, ok := grpc.ServerTransportStreamFromContext(ctx).(transportStream)
	if !ok {
		return status.Error(codes.Internal, "gRPC transport stream unavailable")
	}
	limit := a.maxMessage
	if lease.download {
		limit = maxDownloadShardRequestSize
	}
	size, compressed, err := readMessageHeader(reader, limit)
	if err != nil {
		return err
	}
	if !lease.download {
		if err := lease.reserve(ctx, int64(size)); err != nil {
			return err
		}
	}
	body, err := readMessageBody(reader, size)
	stopReceive()
	if err != nil {
		return err
	}
	if !lease.download {
		charge := size
		if compressed {
			charge = limit // Compressed length does not bound the decoded payload.
		}
		if err := lease.reserve(ctx, a.estimateMemory(int64(charge))-int64(size)); err != nil {
			return err
		}
	}
	if compressed {
		body, err = decompressMessage(body, reader.RecvCompress(), limit)
		if err != nil {
			return err
		}
	}
	if err := a.codec.unmarshalBytes(body, target); err != nil {
		return err
	}
	if lease.download {
		// Downloads carry only a small blob ID; inspect it before estimating the stored payload.
		size := int64(a.maxMessage) * 42 / 100 // 14% stake receives about 42% of the original rows.
		if a.DownloadSize != nil && a.total > 0 {
			size, err = a.DownloadSize(ctx, target.(*types.DownloadShardRequest).BlobId)
			if err != nil {
				return err
			}
		}
		return lease.reserve(ctx, a.estimateMemory(size))
	}
	return nil
}

// estimateMemory covers an RPC's payload copies, metadata, response and storage-reader overhead.
func (a *Admission) estimateMemory(size int64) int64 {
	// Six copies cover decoding, response encoding and gRPC's tiny-frame compaction.
	// Each row allows 256 bytes for row views and 64 bytes per proof segment.
	metadata := int64(a.codec.maxShardRows) * (256 + 64*int64(a.codec.maxProofSegments))
	return 6*min(max(size, 0), int64(a.maxMessage)) + metadata + 1<<20 // 1 MiB covers the reader and small RPC buffers.
}

// readMessageHeader validates gRPC's compression byte and four-byte body length before allocation.
func readMessageHeader(reader transportStream, limit int) (int, bool, error) {
	var header [5]byte
	if err := reader.ReadMessageHeader(header[:]); err != nil {
		return 0, false, status.Error(codes.Internal, err.Error())
	}
	size := binary.BigEndian.Uint32(header[1:])
	if uint64(size) > uint64(limit) || header[0] > 1 {
		return 0, false, status.Error(codes.ResourceExhausted, "invalid RPC message size or compression flag")
	}
	return int(size), header[0] == 1, nil
}

// readMessageBody copies directly into one allocation and preserves gRPC's message-level flow-control credit.
func readMessageBody(reader transportStream, size int) ([]byte, error) {
	body := make([]byte, size)
	if err := reader.ReadMessageHeader(body); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return body, nil
}

// decompressMessage uses the registered gRPC compressor and bounds its output before protobuf decoding.
func decompressMessage(body []byte, name string, limit int) ([]byte, error) {
	compressor := encoding.GetCompressor(name)
	if compressor == nil {
		return nil, status.Error(codes.Unimplemented, "unsupported request compression")
	}
	decoded, err := compressor.Decompress(bytes.NewReader(body))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if closer, ok := decoded.(io.Closer); ok {
		defer closer.Close()
	}
	body, err = io.ReadAll(io.LimitReader(decoded, int64(limit)+1))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if len(body) > limit {
		return nil, status.Error(codes.ResourceExhausted, "decompressed message exceeds protocol limit")
	}
	return body, nil
}
