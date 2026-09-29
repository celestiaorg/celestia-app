package fibre

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/status"
)

// TestDownloadSlowReader covers both storage backends and the real HTTP/2 send queue.
func TestDownloadSlowReader(t *testing.T) {
	sizes := []int{2 << 20}
	if !testing.Short() {
		sizes = append(sizes, DefaultProtocolParams.MaxBlobSize)
	}
	for _, size := range sizes {
		for _, backend := range []string{"local", "object"} {
			for _, end := range []string{"read", "cancel", "shutdown"} {
				t.Run(fmt.Sprintf("%s/%d/%s", backend, size, end), func(t *testing.T) {
					store := newMarkerTestStore(t)
					shard := &types.BlobShard{Rlcs: make([]byte, DefaultProtocolParams.Rows*16)}
					rowSize := DefaultProtocolParams.MaxRowSize(0)
					row := make([]byte, rowSize)
					for left := size; left > 0; left -= rowSize {
						shard.Rows = append(shard.Rows, &types.BlobRow{Data: row[:min(left, rowSize)]})
					}
					commit, hash := Commitment{}, []byte{1}
					var storage shardBackend = storeLocalBackend(t, store)
					if backend == "object" {
						storage = newObjectBackend(&s3ObjectClientStub{getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
							reader, err := newShardReader(shard)
							if err != nil {
								return nil, err
							}
							return &s3.GetObjectOutput{Body: io.NopCloser(reader)}, nil
						}}, objectNamespace{Bucket: "test"})
					} else {
						require.NoError(t, storage.Put(t.Context(), commit, hash, shard))
					}
					service := &backendDownloadService{backend: storage, hash: hash}
					server, err := fibregrpc.Listen("127.0.0.1:0", 8, 8, 2)
					require.NoError(t, err)
					server.Register(service, grpc.ForceServerCodecV2(fibregrpc.NewServerCodec(4096, 14)), grpc.MaxSendMsgSize(DefaultProtocolParams.MaxMessageSize()))
					server.Serve()
					stopped := false
					t.Cleanup(func() {
						if !stopped {
							server.Stop(context.Background())
						}
					})
					cc, err := grpc.NewClient(server.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()),
						grpc.WithStaticStreamWindowSize(64<<10), grpc.WithStaticConnWindowSize(64<<10),
						grpc.WithDefaultCallOptions(grpc.CallContentSubtype("fibre-proto"), grpc.MaxCallRecvMsgSize(DefaultProtocolParams.MaxMessageSize())))
					require.NoError(t, err)
					t.Cleanup(func() { _ = cc.Close() })
					ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
					defer cancel()
					streams := make([]grpc.ClientStream, 2)
					for i := range streams {
						stream, err := cc.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, "/celestia.fibre.v1.Fibre/DownloadShard", grpc.UseCompressor(gzip.Name))
						require.NoError(t, err)
						require.NoError(t, stream.SendMsg(&types.DownloadShardRequest{}))
						require.NoError(t, stream.CloseSend())
						_, err = stream.Header()
						require.NoError(t, err)
						streams[i] = stream
					}
					client := types.NewFibreClient(cc)
					_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{})
					require.Equal(t, codes.ResourceExhausted, status.Code(err))
					require.EqualValues(t, 2, service.reads.Load(), "rejected download must not read the backend")
					switch end {
					case "read":
						for _, stream := range streams {
							var response types.DownloadShardResponse
							require.NoError(t, stream.RecvMsg(&response))
							require.Equal(t, shard, response.Shard)
							require.ErrorIs(t, stream.RecvMsg(&response), io.EOF)
						}
					case "cancel":
						cancel()
					case "shutdown":
						stopCtx, stop := context.WithCancel(context.Background())
						stop()
						server.Stop(stopCtx)
						stopped = true
						return
					}
					// A missing request checks slot recovery without another large response.
					require.Eventually(t, func() bool {
						_, err := client.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: []byte{1}})
						return status.Code(err) == codes.NotFound
					}, 5*time.Second, 10*time.Millisecond)
				})
			}
		}
	}
}

type backendDownloadService struct {
	types.UnimplementedFibreServer
	backend shardBackend
	hash    []byte
	reads   atomic.Int32
}

func (s *backendDownloadService) DownloadShard(ctx context.Context, req *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	if bytes.Equal(req.BlobId, []byte{1}) {
		return nil, status.Error(codes.NotFound, "missing shard")
	}
	s.reads.Add(1)
	shard, err := s.backend.Get(ctx, Commitment{}, s.hash)
	return &types.DownloadShardResponse{Shard: shard}, err
}
