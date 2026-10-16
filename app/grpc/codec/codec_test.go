package codec

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/cometbft/cometbft/libs/protoresource"
	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type recordingCodec struct{ called bool }

func (*recordingCodec) Name() string                  { return "test" }
func (*recordingCodec) Marshal(v any) ([]byte, error) { return proto.Marshal(v.(proto.Message)) }
func (c *recordingCodec) Unmarshal(b []byte, v any) error {
	c.called = true
	return proto.Unmarshal(b, v.(proto.Message))
}

func TestBatchPreflight(t *testing.T) {
	for _, count := range []int{0, 1, tx.MaxTxStatusBatchIDs, tx.MaxTxStatusBatchIDs + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			base := new(recordingCodec)
			bounded, err := New(base)
			require.NoError(t, err)
			ids := make([]string, count)
			for i := range ids {
				ids[i] = strings.Repeat("a", 64)
			}
			wire, err := proto.Marshal(&tx.TxStatusBatchRequest{TxIds: ids})
			require.NoError(t, err)
			var got tx.TxStatusBatchRequest
			err = bounded.Unmarshal(wire, &got)
			if count > tx.MaxTxStatusBatchIDs {
				require.ErrorIs(t, err, protoresource.ErrBudget)
				require.False(t, base.called)
				require.Empty(t, got.TxIds)
			} else {
				require.NoError(t, err)
				require.True(t, base.called)
				if count == 0 {
					require.Empty(t, got.TxIds)
				} else {
					require.Equal(t, ids, got.TxIds)
				}
			}
		})
	}
}

func TestBatchBytesAndOtherRequests(t *testing.T) {
	base := new(recordingCodec)
	bounded, err := New(base)
	require.NoError(t, err)
	err = bounded.Unmarshal(make([]byte, maxBatchBytes+1), new(tx.TxStatusBatchRequest))
	require.ErrorIs(t, err, protoresource.ErrBudget)
	require.False(t, base.called)
	wire, err := proto.Marshal(&tx.TxStatusRequest{TxId: "1234"})
	require.NoError(t, err)
	require.NoError(t, bounded.Unmarshal(wire, new(tx.TxStatusRequest)))
	require.True(t, base.called)
	require.Equal(t, base.Name(), bounded.Name())
	got, err := bounded.Marshal(&tx.TxStatusRequest{TxId: "1234"})
	require.NoError(t, err)
	require.Equal(t, wire, got)
}

type batchServer struct {
	tx.UnimplementedTxServer
	calls atomic.Int32
}

func (s *batchServer) TxStatusBatch(context.Context, *tx.TxStatusBatchRequest) (*tx.TxStatusBatchResponse, error) {
	s.calls.Add(1)
	return &tx.TxStatusBatchResponse{}, nil
}

func TestBatchGRPCIngress(t *testing.T) {
	bounded, err := New(sdkcodec.NewProtoCodec(codectypes.NewInterfaceRegistry()).GRPCCodec())
	require.NoError(t, err)
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.ForceServerCodec(bounded))
	handler := new(batchServer)
	tx.RegisterTxServer(server, handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := tx.NewTxClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ids := make([]string, tx.MaxTxStatusBatchIDs)
	for i := range ids {
		ids[i] = strings.Repeat("a", 64)
	}
	_, err = client.TxStatusBatch(ctx, &tx.TxStatusBatchRequest{TxIds: ids})
	require.NoError(t, err)
	require.EqualValues(t, 1, handler.calls.Load())
	_, err = client.TxStatusBatch(ctx, &tx.TxStatusBatchRequest{TxIds: append(ids, ids[0])})
	require.Error(t, err)
	require.Contains(t, err.Error(), protoresource.ErrBudget.Error())
	require.EqualValues(t, 1, handler.calls.Load())
}
