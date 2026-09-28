package fibre_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerRejectsUnsupportedBlobVersions(t *testing.T) {
	var stateClient *countingPromiseState
	server, vals, val := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		inner := cfg.StateClientFn
		cfg.StateClientFn = func() (state.Client, error) {
			client, err := inner()
			stateClient = &countingPromiseState{Client: client}
			return stateClient, err
		}
	})
	for _, version := range []uint32{1, 255, 256, 512, 1 << 31, ^uint32(0)} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			req := makeTestRequest(t, vals, val, func(r *types.UploadShardRequest) {
				r.Promise.BlobVersion = version
			})
			resp, err := server.UploadShard(t.Context(), req)
			require.Nil(t, resp)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.ErrorContains(t, err, fmt.Sprintf("unsupported blob version %d", version))
			require.Zero(t, stateClient.calls, "rejected promises must not reserve escrow")
		})
	}
}
