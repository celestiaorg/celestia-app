package fibre_test

import (
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

func TestStore(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T, *fibre.Store)
	}{
		{"PutGet_Roundtrip", testStorePutGetRoundtrip},
		{"Put_SameCommitmentSamePromise", testStorePutSameCommitmentSamePromise},
		{"Put_SameCommitmentDifferentPromises", testStorePutSameCommitmentDifferentPromises},
		{"Put_ConcurrentSameKey", testStorePutConcurrentSameKey},
		{"Get_NotFound", testStoreGetNotFound},
		{"Get_DeterministicOrdering", testStoreGetDeterministicOrdering},
		{"PutGet_PreservesRLCCoefficients", testStorePutGetPreservesRLCCoefficients},
		{"PruneBefore_RemovesShardAndPromise", testStorePruneBeforeRemovesShardAndPromise},
		{"PruneBefore_PreservesOtherPromiseShard", testStorePruneBeforePreservesOtherPromiseShard},
		{"PruneBefore_NonUTCCutoff_DoesNotPruneUnexpired", testStorePruneBeforeNonUTCCutoffDoesNotPruneUnexpired},
		{"PruneBefore_IdenticalPruneAt", testStorePruneBeforeIdenticalPruneAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := makeTestStore(t)
			tt.fn(t, store)
		})
	}
}

func testStorePutGetRoundtrip(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1, 2)
	promise := makeTestPaymentPromise(100, blob.ID())

	// put data
	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// get shard by commitment
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.Len(t, gotShard.Rows, 3)
	require.Equal(t, shard.Rows[0].Index, gotShard.Rows[0].Index)
	require.Equal(t, shard.Rows[1].Index, gotShard.Rows[1].Index)
	require.Equal(t, shard.Rows[2].Index, gotShard.Rows[2].Index)

	// get payment promise by hash
	promiseHash, err := promise.Hash()
	require.NoError(t, err)
	gotPromise, err := store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
	require.Equal(t, promise.ChainID, gotPromise.ChainID)
	require.Equal(t, promise.Height, gotPromise.Height)
	require.Equal(t, promise.Commitment, gotPromise.Commitment)
}

func testStorePutGetPreservesRLCCoefficients(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardWithRLC(t, blob, 0, 1, 2)
	promise := makeTestPaymentPromise(100, blob.ID())

	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.Len(t, gotShard.Rows, 3)

	// Coefficients and root must survive the round-trip
	require.Equal(t, shard.Coefficients, gotShard.Coefficients,
		"RLC coefficients should be preserved after store round-trip")
	require.Equal(t, shard.Root, gotShard.Root,
		"RLC root should be preserved after store round-trip")
}

func testStorePutSameCommitmentSamePromise(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	// put data first time
	err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// put the same data again (same commitment, same promise)
	err = store.Put(ctx, promise, shard, promise.CreationTimestamp)
	require.NoError(t, err)

	// should be able to retrieve
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 2)
}

func testStorePutSameCommitmentDifferentPromises(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	// create a single blob to get the same commitment
	blob := makeTestBlobV0(t, 256)

	// extract different shards from the same blob
	shard1 := makeShardFrom(t, blob, 0, 1)
	shard2 := makeShardFrom(t, blob, 2, 3)

	// first promise with rows 0, 1
	promise1 := makeTestPaymentPromise(100, blob.ID())

	// second promise with different height but same commitment, rows 2, 3
	promise2 := makeTestPaymentPromise(101, blob.ID())

	// put first promise
	err := store.Put(ctx, promise1, shard1, promise1.CreationTimestamp)
	require.NoError(t, err)

	// put second promise with same commitment but different promise
	err = store.Put(ctx, promise2, shard2, promise2.CreationTimestamp)
	require.NoError(t, err)

	// get returns only first shard found (to prevent unbounded message sizes)
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.NotNil(t, gotShard)
	require.Len(t, gotShard.Rows, 2, "should have 2 rows from first shard")

	// both promises should be retrievable individually
	hash1, err := promise1.Hash()
	require.NoError(t, err)
	gotPromise1, err := store.GetPaymentPromise(ctx, hash1)
	require.NoError(t, err)
	require.Equal(t, promise1.Height, gotPromise1.Height)

	hash2, err := promise2.Hash()
	require.NoError(t, err)
	gotPromise2, err := store.GetPaymentPromise(ctx, hash2)
	require.NoError(t, err)
	require.Equal(t, promise2.Height, gotPromise2.Height)
}

// Regression: with a fixed ".tmp" filename, concurrent same-key Puts shared
// the same tmp file and corrupted each other; one rename also failed ENOENT.
func testStorePutConcurrentSameKey(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1, 2)
	promise := makeTestPaymentPromise(100, blob.ID())

	const N = 50
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = store.Put(ctx, promise, shard, promise.CreationTimestamp)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "Put #%d", i)
	}
	got, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.Len(t, got.Rows, len(shard.Rows))
}

func testStoreGetNotFound(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	// create commitment that was never stored
	blob := makeTestBlobV0(t, 256)

	// try to get commitment that was never stored
	_, err := store.Get(ctx, blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStorePruneBeforeRemovesShardAndPromise(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	cutoffTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	err := store.Put(ctx, promise, shard, pruneAt)
	require.NoError(t, err)

	// verify exists
	promiseHash, _ := promise.Hash()
	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
	_, err = store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)

	// prune
	pruned, err := store.PruneBefore(ctx, cutoffTime)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// both promise and shard should be gone
	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.Error(t, err)
	_, err = store.Get(ctx, blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStorePruneBeforePreservesOtherPromiseShard(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)

	oldPruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	cutoffTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	newPruneAt := time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)

	// two promises with different shards for the same commitment
	oldPromise := makeTestPaymentPromise(100, blob.ID())
	newPromise := makeTestPaymentPromise(101, blob.ID())
	oldShard := makeShardFrom(t, blob, 0, 1)
	newShard := makeShardFrom(t, blob, 2, 3)

	err := store.Put(ctx, oldPromise, oldShard, oldPruneAt)
	require.NoError(t, err)
	err = store.Put(ctx, newPromise, newShard, newPruneAt)
	require.NoError(t, err)

	oldHash, _ := oldPromise.Hash()
	newHash, _ := newPromise.Hash()

	// prune old
	pruned, err := store.PruneBefore(ctx, cutoffTime)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// old promise gone
	_, err = store.GetPaymentPromise(ctx, oldHash)
	require.Error(t, err)

	// new promise and its shard still exist
	_, err = store.GetPaymentPromise(ctx, newHash)
	require.NoError(t, err)
	gotShard, err := store.Get(ctx, blob.ID().Commitment())
	require.NoError(t, err)
	require.Equal(t, uint32(2), gotShard.Rows[0].Index)
}

// testStorePruneBeforeNonUTCCutoffDoesNotPruneUnexpired is a regression test for a timezone bug
// where PruneBefore would incorrectly prune entries on non-UTC machines.
func testStorePruneBeforeNonUTCCutoffDoesNotPruneUnexpired(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())

	// entry expires at 19:08 UTC
	pruneAt := time.Date(2025, 1, 1, 19, 8, 0, 0, time.UTC)
	err := store.Put(ctx, promise, shard, pruneAt)
	require.NoError(t, err)

	promiseHash, _ := promise.Hash()

	// cutoff is 19:11 UTC+1 = 18:11 UTC, which is before the entry's expiry of 19:08 UTC.
	// before the fix, PruneBefore compared the UTC key "202501011908" against the local-formatted
	// string "202501011911", making the entry appear expired and pruning it incorrectly.
	utcPlusOne := time.FixedZone("UTC+1", 60*60)
	cutoff := time.Date(2025, 1, 1, 19, 11, 0, 0, utcPlusOne)

	pruned, err := store.PruneBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 0, pruned)

	_, err = store.GetPaymentPromise(ctx, promiseHash)
	require.NoError(t, err)
}

// Two promises sharing the same pruneAt are both pruned in one pass.
func testStorePruneBeforeIdenticalPruneAt(t *testing.T, store *fibre.Store) {
	ctx := t.Context()
	blob := makeTestBlobV0(t, 256)
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	for _, height := range []uint64{100, 101} {
		p := makeTestPaymentPromise(height, blob.ID())
		require.NoError(t, store.Put(ctx, p, makeShardFrom(t, blob, 0, 1), pruneAt))
	}
	pruned, err := store.PruneBefore(ctx, pruneAt.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, pruned)

	_, err = store.Get(ctx, blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func testStoreGetDeterministicOrdering(t *testing.T, store *fibre.Store) {
	ctx := t.Context()

	blob := makeTestBlobV0(t, 256)
	id := blob.ID()

	// store multiple shards with different row indices
	for i := range 5 {
		shard := makeShardFrom(t, blob, i*2, i*2+1)
		promise := makeTestPaymentPromise(uint64(100+i), id)
		err := store.Put(ctx, promise, shard, promise.CreationTimestamp)
		require.NoError(t, err)
	}

	// get multiple times and verify ordering is deterministic
	var firstRowIndex uint32
	for i := range 10 {
		gotShard, err := store.Get(ctx, id.Commitment())
		require.NoError(t, err)
		require.NotEmpty(t, gotShard.Rows)

		if i == 0 {
			firstRowIndex = gotShard.Rows[0].Index
		} else {
			require.Equal(t, firstRowIndex, gotShard.Rows[0].Index,
				"query ordering should be deterministic - first row index should always be the same")
		}
	}
}

// Reconcile drops staging/ leftovers on open, leaves real shards alone, and
// logs the cleanup count.
func TestStoreReconcileStaging(t *testing.T) {
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := fibre.NewStore(cfg)
	require.NoError(t, err)

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	require.NoError(t, store.Close())

	stagingDir := filepath.Join(cfg.Path, "staging")
	require.NoError(t, os.MkdirAll(stagingDir, 0o755))
	staleA := filepath.Join(stagingDir, "aaa")
	staleB := filepath.Join(stagingDir, "bbb")
	require.NoError(t, os.WriteFile(staleA, []byte("partial-a"), 0o644))
	require.NoError(t, os.WriteFile(staleB, []byte("partial-b"), 0o644))

	var buf strings.Builder
	cfg.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, err = fibre.NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	for _, p := range []string{staleA, staleB} {
		_, err := os.Stat(p)
		require.True(t, os.IsNotExist(err), "%s should be removed by reconcile", p)
	}
	st, err := os.Stat(stagingDir)
	require.NoError(t, err)
	require.True(t, st.IsDir())

	out := buf.String()
	require.Contains(t, out, "store reconcile complete")
	require.Contains(t, out, "staging_files_removed=2")

	got, err := store.Get(t.Context(), blob.ID().Commitment())
	require.NoError(t, err)
	require.Len(t, got.Rows, 2)
}

// Get drops a /shard/ marker whose backing file is missing (crash between
// pebble commit and rename) so future Gets stop paying the missed lookup.
func TestStoreGetCleansOrphanMarker(t *testing.T) {
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := fibre.NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	blob := makeTestBlobV0(t, 256)
	shard := makeShardFrom(t, blob, 0, 1)
	promise := makeTestPaymentPromise(100, blob.ID())
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	promiseHash, err := promise.Hash()
	require.NoError(t, err)
	filePath := filepath.Join(cfg.Path, "shards", blob.ID().Commitment().String()+"-"+hex.EncodeToString(promiseHash))

	// Simulate "metadata committed, file write never landed".
	require.NoError(t, os.Remove(filePath))

	_, err = store.Get(t.Context(), blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)

	// After the first Get drops the marker, a fresh Put with a different
	// promise must be the one Get finds — proving the orphan slot is gone.
	promise2 := makeTestPaymentPromise(101, blob.ID())
	shard2 := makeShardFrom(t, blob, 2, 3)
	require.NoError(t, store.Put(t.Context(), promise2, shard2, promise2.CreationTimestamp))

	got, err := store.Get(t.Context(), blob.ID().Commitment())
	require.NoError(t, err)
	require.Len(t, got.Rows, 2)
	require.Equal(t, uint32(2), got.Rows[0].Index, "Get should return the new (still-present) shard, not the orphan")
}

// When iter.First() lands on an orphan marker, Get must skip it and return
// the lex-next valid sibling for the same commit.
func TestStoreGetSkipsOrphanToSibling(t *testing.T) {
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := fibre.NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	blob := makeTestBlobV0(t, 256)
	p1 := makeTestPaymentPromise(100, blob.ID())
	p2 := makeTestPaymentPromise(101, blob.ID())
	s1 := makeShardFrom(t, blob, 0, 1)
	s2 := makeShardFrom(t, blob, 2, 3)
	require.NoError(t, store.Put(t.Context(), p1, s1, p1.CreationTimestamp))
	require.NoError(t, store.Put(t.Context(), p2, s2, p2.CreationTimestamp))

	// Delete the file for whichever promise hashes lex-first so iter.First()
	// lands on the orphan and forces the fall-through.
	h1, _ := p1.Hash()
	h2, _ := p2.Hash()
	orphan, validRow := h1, s2.Rows[0].Index
	if hex.EncodeToString(h1) > hex.EncodeToString(h2) {
		orphan, validRow = h2, s1.Rows[0].Index
	}
	require.NoError(t, os.Remove(filepath.Join(cfg.Path, "shards",
		blob.ID().Commitment().String()+"-"+hex.EncodeToString(orphan))))

	got, err := store.Get(t.Context(), blob.ID().Commitment())
	require.NoError(t, err)
	require.Equal(t, validRow, got.Rows[0].Index)
}

// All shards for a commit are orphans → Get returns NotFound (and cleans the
// markers along the way).
func TestStoreGetAllOrphans(t *testing.T) {
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := fibre.NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	blob := makeTestBlobV0(t, 256)
	for i := range 3 {
		p := makeTestPaymentPromise(uint64(100+i), blob.ID())
		require.NoError(t, store.Put(t.Context(), p, makeShardFrom(t, blob, 0, 1), p.CreationTimestamp))
		h, _ := p.Hash()
		require.NoError(t, os.Remove(filepath.Join(cfg.Path, "shards",
			blob.ID().Commitment().String()+"-"+hex.EncodeToString(h))))
	}
	_, err = store.Get(t.Context(), blob.ID().Commitment())
	require.ErrorIs(t, err, fibre.ErrStoreNotFound)
}

func makeTestStore(t *testing.T) *fibre.Store {
	t.Helper()
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := fibre.NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// makeShardFrom extracts a shard from a blob at the given row indices.
func makeShardFrom(t *testing.T, blob *fibre.Blob, indices ...int) *types.BlobShard {
	t.Helper()

	rows := make([]*types.BlobRow, len(indices))
	for i, idx := range indices {
		rowProof, err := blob.Row(idx)
		require.NoError(t, err)
		rows[i] = &types.BlobRow{
			Index: uint32(idx),
			Data:  rowProof.Row,
			Proof: rowProof.RowProof.RowProof,
		}
	}

	return &types.BlobShard{
		Rows: rows,
		Root: make([]byte, 32),
	}
}

// makeShardWithRLC extracts a shard from a blob at the given row indices,
// including RLC coefficients and root.
func makeShardWithRLC(t *testing.T, blob *fibre.Blob, indices ...int) *types.BlobShard {
	t.Helper()
	shard := makeShardFrom(t, blob, indices...)

	rlcCoeffs := blob.RLC()
	coeffBytes := make([]byte, len(rlcCoeffs)*16)
	for i, c := range rlcCoeffs {
		b := field.ToBytes128(c)
		copy(coeffBytes[i*16:(i+1)*16], b[:])
	}
	shard.Coefficients = coeffBytes

	return shard
}

var testSignerKey = secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)

// makeTestPaymentPromise creates a test payment promise for store tests.
func makeTestPaymentPromise(height uint64, blobID fibre.BlobID) *fibre.PaymentPromise {
	return &fibre.PaymentPromise{
		ChainID:           "test-chain",
		Height:            height,
		Namespace:         testNamespace,
		UploadSize:        1024,
		BlobVersion:       uint32(blobID.Version()),
		Commitment:        blobID.Commitment(),
		CreationTimestamp: time.Date(2025, 10, 21, 15, 30, 0, 0, time.UTC),
		SignerKey:         testSignerKey,
		Signature:         []byte("test-signature-64-bytes-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"),
	}
}
