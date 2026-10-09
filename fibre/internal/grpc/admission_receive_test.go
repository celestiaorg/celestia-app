package grpc

import (
	"bytes"
	stdgzip "compress/gzip"
	"context"
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type receiveTestStream struct {
	grpc.ServerTransportStream
	header     []byte
	body       []byte
	beforeBody func([]byte)
}

func (s *receiveTestStream) ReadMessageHeader(dst []byte) error {
	if s.header != nil {
		copy(dst, s.header)
		s.header = nil
		return nil
	}
	if s.beforeBody != nil {
		s.beforeBody(dst)
	}
	copy(dst, s.body)
	return nil
}

func (*receiveTestStream) RecvCompress() string { return "gzip" }
func (*receiveTestStream) SendCompress() string { return "identity" }

type receiveTestDecoder struct{ unmarshal func([]byte) error }

func (d receiveTestDecoder) Unmarshal(body []byte) error { return d.unmarshal(body) }

func receiveTestContext(t *testing.T, stream *receiveTestStream) context.Context {
	t.Helper()
	ctx := context.WithValue(t.Context(), receiveTimerKey{}, func() {})
	return grpc.NewContextWithServerTransportStream(ctx, stream)
}

func TestReceiveUploadReservationStages(t *testing.T) {
	for _, compression := range []string{"identity", "gzip"} {
		for _, budget := range []int64{256, 16 << 20} {
			t.Run(compression+"/"+strconv.FormatInt(budget, 10), func(t *testing.T) {
				payload := []byte("upload body")
				wire := payload
				flag := byte(0)
				if compression == "gzip" {
					var compressed bytes.Buffer
					writer := stdgzip.NewWriter(&compressed)
					_, err := writer.Write(payload)
					require.NoError(t, err)
					require.NoError(t, writer.Close())
					wire, flag = compressed.Bytes(), 1
					if budget == 256 {
						wire = []byte("invalid gzip") // Admission must reject before decompression.
					}
				}
				admission := NewAdmission(budget, 1<<20, testMaxRows, testMaxProofs)
				lease := newMemoryLease(admission.memoryBudget, false)
				bodyRead, decoded := false, false
				stream := &receiveTestStream{
					header: binary.BigEndian.AppendUint32([]byte{flag}, uint32(len(wire))), body: wire,
					beforeBody: func([]byte) {
						bodyRead = true
						require.EqualValues(t, len(wire), admission.used, "reserve one wire body before reading")
					},
				}
				target := receiveTestDecoder{unmarshal: func(body []byte) error {
					decoded = true
					require.Equal(t, payload, body)
					charge := len(wire)
					if flag == 1 {
						charge = admission.maxMessage
					}
					require.Equal(t, admission.estimateMemory(int64(charge)), admission.used, "reserve processing memory before decoding")
					return nil
				}}
				err := admission.receive(receiveTestContext(t, stream), target, lease)
				require.True(t, bodyRead)
				if budget == 256 {
					require.Equal(t, codes.ResourceExhausted, status.Code(err))
					require.False(t, decoded, "reject before decoding when the processing reservation does not fit")
				} else {
					require.NoError(t, err)
					require.True(t, decoded)
				}
				lease.release()
				require.Zero(t, admission.used)
			})
		}
	}
}

func TestReceiveDecodesBodyWithoutCopy(t *testing.T) {
	admission := NewAdmission(16<<20, 1<<20, testMaxRows, testMaxProofs)
	lease := newMemoryLease(admission.memoryBudget, false)
	defer lease.release()
	var received []byte
	stream := &receiveTestStream{
		header: binary.BigEndian.AppendUint32([]byte{0}, 3), body: []byte{1, 2, 3},
		beforeBody: func(body []byte) { received = body },
	}
	target := receiveTestDecoder{unmarshal: func(body []byte) error {
		require.Same(t, &received[0], &body[0], "decode directly from the receive allocation")
		return nil
	}}
	require.NoError(t, admission.receive(receiveTestContext(t, stream), target, lease))
}

func TestReceiveRejectsExcessUploadRows(t *testing.T) {
	admission := NewAdmission(16<<20, 1<<20, testMaxRows, testMaxProofs)
	lease := newMemoryLease(admission.memoryBudget, false)
	defer lease.release()
	body := marshalShard(t, testMaxRows+1, 1)
	stream := &receiveTestStream{header: binary.BigEndian.AppendUint32([]byte{0}, uint32(len(body))), body: body}
	var target types.UploadShardRequest
	require.ErrorContains(t, admission.receive(receiveTestContext(t, stream), &target, lease), "rows")
	require.Nil(t, target.Shard, "reject before the generated decoder allocates the shard")
}
