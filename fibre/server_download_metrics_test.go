package fibre_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestServerDownloadShardMetrics checks that DownloadShard RPCs are classified
// by outcome and that served responses are counted in response_bytes.
func TestServerDownloadShardMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	server, _, _ := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		cfg.Meter = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	})

	stored := makeTestBlobV0(t, 256)
	storeTestShard(t, server, stored)
	resp, err := server.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: stored.ID()})
	require.NoError(t, err)

	missing := makeTestBlobV0(t, 512)
	_, err = server.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: missing.ID()})
	require.Error(t, err)

	_, err = server.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: []byte{1, 2, 3}})
	require.Error(t, err)

	require.Equal(t, map[string]int64{"": int64(resp.Size())},
		counterByAttr(t, reader, "fibre.server.download_shard.response_bytes", "outcome"))
	require.Equal(t, map[string]int64{"served": 1, "not_found": 1, "invalid": 1},
		histogramCountByAttr(t, reader, "fibre.server.download_shard.duration", "outcome"))
}
