package user_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolLifecycleManagement tests proper initialization, starting, and stopping of the pool
func TestPoolLifecycleManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lifecycle test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig().WithFundedAccounts("main"))

	client, err := testnode.NewTxClientFromContext(cctx)
	require.NoError(t, err)

	t.Run("Complete lifecycle", func(t *testing.T) {
		// 1. Initialize pool
		err := client.InitUnorderedTxPool(ctx, "main", 2)
		require.NoError(t, err)

		// Verify pool is not running yet
		workers, queueSize, isRunning := client.GetUnorderedPoolStats()
		assert.Equal(t, 2, workers)
		assert.Equal(t, 0, queueSize)
		assert.False(t, isRunning)

		// 2. Start pool
		err = client.StartUnorderedTxPool()
		require.NoError(t, err)

		// Verify pool is running
		workers, queueSize, isRunning = client.GetUnorderedPoolStats()
		assert.Equal(t, 2, workers)
		assert.Equal(t, 0, queueSize)
		assert.True(t, isRunning)

		// 3. Try to start again (should fail)
		err = client.StartUnorderedTxPool()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		// 4. Use the pool
		namespace := testfactory.RandomBlobNamespace()
		blob, err := share.NewBlob(namespace, []byte("test"), share.ShareVersionZero, nil)
		require.NoError(t, err)

		_, err = client.SubmitPayForBlobUnordered(ctx, []*share.Blob{blob})
		require.NoError(t, err)

		// 5. Stop pool
		err = client.StopUnorderedTxPool()
		require.NoError(t, err)

		// Verify pool is stopped
		workers, queueSize, isRunning = client.GetUnorderedPoolStats()
		assert.Equal(t, 2, workers)
		assert.Equal(t, 0, queueSize)
		assert.False(t, isRunning)

		// 6. Try to use stopped pool (should fail)
		_, err = client.SubmitPayForBlobUnordered(ctx, []*share.Blob{blob})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not running")

		// 7. Stop again (should not error)
		err = client.StopUnorderedTxPool()
		assert.NoError(t, err)
	})
}
