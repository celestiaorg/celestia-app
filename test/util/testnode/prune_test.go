package testnode_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/stretchr/testify/require"
)

// TestMinRetainBlocksPrunes is an integration test that demonstrates a node
// configured with a small min-retain-blocks value actually prunes old blocks
// from the CometBFT blockstore. It is the existence proof that pruning blocks
// well within the evidence window (MaxAgeNumBlocks = 559_940) is technically
// possible.
//
// See https://github.com/celestiaorg/celestia-app/issues/6954.
func TestMinRetainBlocksPrunes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pruning integration test in short mode")
	}

	const (
		minRetainBlocks uint64 = 5
		// Run well past the retain window so several blocks are eligible to be
		// pruned even with the SDK's once-per-block retain height calculation.
		targetHeight int64 = 25
	)

	cfg := testnode.DefaultConfig().
		WithTimeoutCommit(50 * time.Millisecond)

	// Disable state-sync snapshots. baseapp.GetBlockRetentionHeight takes the
	// minimum non-zero of (commitHeight - snapshotRetention) and
	// (commitHeight - minRetainBlocks); leaving the default snapshot window
	// (1500 * 2 = 3000) would force retentionHeight <= 0 and skip pruning.
	cfg.AppConfig.StateSync.SnapshotInterval = 0
	cfg.AppConfig.StateSync.SnapshotKeepRecent = 0

	// Plumb min-retain-blocks through AppOptions so server.DefaultBaseappOptions
	// applies baseapp.SetMinRetainBlocks during app construction.
	cfg.AppOptions.Set(server.FlagMinRetainBlocks, minRetainBlocks)

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	_, err := cctx.WaitForHeight(targetHeight)
	require.NoError(t, err)

	status, err := cctx.Client.Status(cctx.GoContext())
	require.NoError(t, err)

	t.Logf("earliest=%d latest=%d min-retain-blocks=%d",
		status.SyncInfo.EarliestBlockHeight,
		status.SyncInfo.LatestBlockHeight,
		minRetainBlocks,
	)

	require.Greater(t, status.SyncInfo.EarliestBlockHeight, int64(1),
		"expected blockstore base height to advance past genesis after pruning")
}
