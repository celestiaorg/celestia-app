package fibre_test

import (
	"context"
	cryptorand "crypto/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/require"
)

// TestClientServerStorageBudget exercises the storage limiter end-to-end over
// the real gRPC path: the client uploads through the network and the server's
// admission check accepts or rejects based on its per-node occupancy budget.
//
// A single validator is used so its per-node budget equals FullStakeStorageBudget
// exactly (it is assigned every row, so assignedRows/OriginalRows == 1), which
// makes the budget arithmetic below directly predictable.
func TestClientServerStorageBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestClientServerStorageBudget in short mode")
	}

	const blobSize = 256 * 1024

	// uploadRandom uploads a fresh random blob of blobSize and returns its ID and
	// the upload error. Distinct content means a distinct commitment every call,
	// so the server never short-circuits on an already-stored shard.
	uploadRandom := func(t *testing.T, ctx context.Context, client *fibre.Client) (fibre.BlobID, error) {
		t.Helper()
		data := make([]byte, blobSize)
		_, err := cryptorand.Read(data)
		require.NoError(t, err)
		blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
		require.NoError(t, err)
		defer blob.Free()

		_, err = client.Upload(ctx, testNamespace, blob)
		return blob.ID(), err
	}

	t.Run("unlimited budget stores the shard", func(t *testing.T) {
		// Default config leaves FullStakeStorageBudget unset (0 == unlimited).
		env := makeTestEnv(t, 1, 1, nil, nil)
		defer env.Close()

		id, err := uploadRandom(t, t.Context(), env.clients[0])
		require.NoError(t, err)
		env.clients[0].Await()

		_, err = env.stores[0].Get(t.Context(), id.Commitment())
		require.NoError(t, err, "the shard must be stored when the budget is unlimited")
	})

	t.Run("tiny budget rejects and stores nothing", func(t *testing.T) {
		env := makeTestEnv(t, 1, 1, nil, func(cfg *fibre.ServerConfig) {
			// A full-stake budget of OriginalRows bytes is orders of magnitude
			// below one shard, so every upload is rejected.
			cfg.FullStakeStorageBudgetFn = func(context.Context) (int64, error) {
				return int64(fibre.DefaultProtocolParams.Rows), nil
			}
		})
		defer env.Close()

		// The reject carries a RetryInfo hint of one prune interval; bound the
		// context so the client's retry does not stall the test on that wait.
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		_, err := uploadRandom(t, ctx, env.clients[0])
		require.Error(t, err, "upload must fail: the only validator rejects, so quorum is unreachable")

		size, err := env.stores[0].Size()
		require.NoError(t, err)
		require.Zero(t, size, "a rejected upload must store nothing")
	})

	t.Run("budget fits exactly one shard", func(t *testing.T) {
		// Measure one shard's on-disk size with an unlimited store. Equal-sized
		// blobs encode to an identical shard size regardless of content, so this
		// measurement is a stable budget for the assertion env below.
		var shardSize int64
		func() {
			env := makeTestEnv(t, 1, 1, nil, nil)
			defer env.Close()

			id, err := uploadRandom(t, t.Context(), env.clients[0])
			require.NoError(t, err)
			env.clients[0].Await()
			_, err = env.stores[0].Get(t.Context(), id.Commitment())
			require.NoError(t, err)

			shardSize, err = env.stores[0].Size()
			require.NoError(t, err)
			require.Positive(t, shardSize)
		}()

		// Budget = exactly one shard: the first upload fits, the second exceeds it.
		env := makeTestEnv(t, 1, 1, nil, func(cfg *fibre.ServerConfig) {
			cfg.FullStakeStorageBudgetFn = func(context.Context) (int64, error) {
				return shardSize, nil
			}
		})
		defer env.Close()

		_, err := uploadRandom(t, t.Context(), env.clients[0])
		require.NoError(t, err, "the first shard fits within the budget")
		env.clients[0].Await()

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		_, err = uploadRandom(t, ctx, env.clients[0])
		require.Error(t, err, "the second shard exceeds the budget and is rejected")

		size, err := env.stores[0].Size()
		require.NoError(t, err)
		require.Equal(t, shardSize, size, "only the first shard is stored")
	})
}
