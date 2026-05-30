package fibre

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/fibre/internal/row"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

const (
	testK       = 8
	testN       = 8
	testRowSize = 256
)

// newTestDownload returns a download primed for one encoded data set, plus
// the matching row proofs and RLC. Validators are synthesized with the given
// per-validator ExpectedRows.
func newTestDownload(t *testing.T, expected ...int) (*download, []*rsema1d.RowProof, rlc.Vector) {
	t.Helper()
	cfg := &rsema1d.Config{K: testK, N: testN, RowSize: testRowSize, WorkerCount: 1}
	coder, err := rsema1d.NewCoder(cfg)
	require.NoError(t, err)

	// Write a valid blob header at the start of data[0] so the post-Reconstruct
	// decode in download.Blob succeeds; the remaining bytes are random.
	data := make([][]byte, testK)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range data {
		data[i] = make([]byte, testRowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	hdr := newBlobHeaderV0(testK*testRowSize - blobHeaderLen)
	hdr.marshalTo(data[0])
	ed, commitment, rlc, err := rsema1d.Encode(data, cfg)
	require.NoError(t, err)

	proofs := make([]*rsema1d.RowProof, testK+testN)
	for i := range proofs {
		p, err := ed.GenerateRowProof(i)
		require.NoError(t, err)
		proofs[i] = p
	}

	selected := make([]validator.SelectedValidator, len(expected))
	for i, n := range expected {
		priv := cmted25519.GenPrivKey()
		selected[i] = validator.SelectedValidator{
			Validator:    &core.Validator{Address: priv.PubKey().Address(), PubKey: priv.PubKey(), VotingPower: 1},
			ExpectedRows: n,
		}
	}

	blobCfg := BlobConfig{
		OriginalRows: testK,
		ParityRows:   testN,
		MaxDataSize:  testK*testRowSize - blobHeaderLen,
		Coder:        coder,
		DataPool:     row.NewPool(testRowSize, testK),
	}
	d, err := newDownload(blobCfg, NewBlobID(0, commitment), selected)
	require.NoError(t, err)
	t.Cleanup(d.freeSlab)
	return d, proofs, rlc
}

// First validator's reservation overshoots K; the K-budget gate prevents
// dispatching the second.
func TestDownload_OverReservationGatesFurtherDispatch(t *testing.T) {
	d, proofs, rlc := newTestDownload(t, testK+5, testK/2)
	for from := range d.ShardSources(context.Background()) {
		require.NoError(t, d.AddShard(from, proofs[:testK], rlc))
	}
	blob, err := d.Blob(context.Background())
	require.NoError(t, err)
	require.NotNil(t, blob)
	require.Equal(t, testK, d.RowsCount())
}

// Both validators deliver the same row indices; dedup keeps rowsHave counting
// unique rows.
func TestDownload_DuplicateRowsDoNotDoubleCount(t *testing.T) {
	d, proofs, rlc := newTestDownload(t, testK/2, testK/2)
	for from := range d.ShardSources(context.Background()) {
		require.NoError(t, d.AddShard(from, proofs[:testK/2], rlc))
	}
	_, err := d.Blob(context.Background())
	require.ErrorIs(t, err, ErrNotEnoughShards)
	require.Equal(t, testK/2, d.RowsCount())
}

// ShardSources must dispatch only as many workers as the K-row budget needs,
// and exactly that many more when early workers fail — never the whole set.
// Workers run concurrently (goroutine per yield, as Client.downloadBlob does),
// so this exercises the pick gate (Want() <= inflight) with multiple in-flight
// reservations: the burst fills the budget, parks, and only re-dispatches as
// completing/skipping workers free capacity.
func TestDownload_DispatchCount(t *testing.T) {
	const chunk = 2              // ExpectedRows each validator reserves and delivers
	const needed = testK / chunk // workers required to cover K (=4)

	for _, tc := range []struct {
		name     string
		failures int
		want     int
	}{
		{"minimal_on_happy_path", 0, needed},
		{"escalates_by_failure_count", 3, needed + 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := make([]int, testK) // more validators available than needed
			for i := range expected {
				expected[i] = chunk
			}
			d, proofs, rlc := newTestDownload(t, expected...)

			yields, delivered := 0, 0
			for from := range d.ShardSources(context.Background()) {
				yields++
				if yields <= tc.failures {
					d.SkipShard(from) // worker failed: release without rows
					continue
				}
				shard := proofs[delivered*chunk : delivered*chunk+chunk]
				delivered++
				go func() { _ = d.AddShard(from, shard, rlc) }()
			}
			// ShardSources returns only once inflight==0 — every dispatched
			// worker has stored and released — so Blob is safe to read s.rows
			// without a WaitGroup. A failed AddShard surfaces as missing rows
			// (ErrNotEnoughShards from Blob), not a silent pass.
			require.Equal(t, tc.want, yields, "dispatched worker count")

			blob, err := d.Blob(context.Background())
			require.NoError(t, err)
			require.NotNil(t, blob)
		})
	}
}
