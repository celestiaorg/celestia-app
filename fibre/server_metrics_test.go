package fibre_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestServerUploadShardRequestBytes checks that every UploadShard request is
// counted in request_bytes under the outcome it ended with.
func TestServerUploadShardRequestBytes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	server, valSet, serverValidator := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		cfg.Meter = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	})

	stored := makeTestRequest(t, valSet, serverValidator, nil)
	_, err := server.UploadShard(t.Context(), stored)
	require.NoError(t, err)

	// Same promise again: a duplicate that skips storage.
	_, err = server.UploadShard(t.Context(), stored)
	require.NoError(t, err)

	invalid := makeTestRequest(t, valSet, serverValidator, func(req *types.UploadShardRequest) {
		req.Promise.ChainId = "wrong-chain"
	})
	_, err = server.UploadShard(t.Context(), invalid)
	require.Error(t, err)

	require.Equal(t, map[string]int64{
		"stored":    int64(stored.Size()),
		"duplicate": int64(stored.Size()),
		"invalid":   int64(invalid.Size()),
	}, counterByAttr(t, reader, "fibre.server.upload_shard.request_bytes", "outcome"))
}

// TestServerUploadShardRequestBytesRejected checks that a storage limiter
// rejection is counted under its own outcome.
func TestServerUploadShardRequestBytesRejected(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	server, valSet, serverValidator := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		cfg.Meter = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
		withStateBudget(int64(fibre.DefaultProtocolParams.Rows))(cfg)
	})

	req := makeTestRequest(t, valSet, serverValidator, nil)
	_, err := server.UploadShard(t.Context(), req)
	require.Error(t, err)

	require.Equal(t, map[string]int64{"rejected": int64(req.Size())},
		counterByAttr(t, reader, "fibre.server.upload_shard.request_bytes", "outcome"))
}
