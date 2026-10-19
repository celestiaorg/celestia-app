package fibre_test

import (
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestServerUploadShardDuplicate checks that re-uploading an already stored
// shard skips verification, leaves the stored data untouched, and still
// returns the validator signature.
func TestServerUploadShardDuplicate(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	server, valSet, serverValidator := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		cfg.Meter = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	})

	req := makeTestRequest(t, valSet, serverValidator, nil)
	var commitment fibre.Commitment
	copy(commitment[:], req.Promise.Commitment)

	first, err := server.UploadShard(t.Context(), req)
	require.NoError(t, err)
	stored, err := server.Store().Get(t.Context(), commitment)
	require.NoError(t, err)
	require.True(t, gogoproto.Equal(req.Shard, stored))

	// A retry with a corrupted proof would fail verification on a first
	// upload, so success here proves verification was skipped.
	req.Shard.Rows[0].Proof[0] = []byte("invalid proof")
	dup, err := server.UploadShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.ValidatorSignature, dup.ValidatorSignature)

	// The duplicate path never inspects the rows, so an empty shard is fine.
	req.Shard.Rows = nil
	dup, err = server.UploadShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.ValidatorSignature, dup.ValidatorSignature)

	// Stored data is the originally verified shard, not the corrupted retry.
	after, err := server.Store().Get(t.Context(), commitment)
	require.NoError(t, err)
	require.True(t, gogoproto.Equal(stored, after))

	require.Equal(t, map[string]int64{"before_verification": 2}, dupeHits(t, reader))
}

// TestServerUploadShardConcurrentDuplicates uploads the same shard from many
// goroutines at once. Exactly one stores it and every other call is counted
// as a duplicate, either before or after verification.
func TestServerUploadShardConcurrentDuplicates(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	server, valSet, serverValidator := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		cfg.Meter = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	})

	req := makeTestRequest(t, valSet, serverValidator, nil)

	const n = 8
	responses := make([]*types.UploadShardResponse, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			responses[i], errs[i] = server.UploadShard(t.Context(), req)
		})
	}
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		require.Equal(t, responses[0].ValidatorSignature, responses[i].ValidatorSignature)
	}

	var total int64
	for _, v := range dupeHits(t, reader) {
		total += v
	}
	require.Equal(t, int64(n-1), total)
}

// dupeHits collects the upload_shard.dupe_hits counter, keyed by stage.
func dupeHits(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	hits := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "fibre.server.upload_shard.dupe_hits" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				stage, _ := dp.Attributes.Value(attribute.Key("stage"))
				hits[stage.AsString()] += dp.Value
			}
		}
	}
	return hits
}
