//go:build benchmarks

package benchmarks_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	"github.com/celestiaorg/celestia-app/v10/test/util/fibrefactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

const pffStageCount = 2000

// BenchmarkPFFABCI_2000 measures each ABCI stage on the same full PFF block.
func BenchmarkPFFABCI_2000(b *testing.B) {
	app.NodeHome = b.TempDir()
	fixture := fibrefactory.NewFixture(b, pffStageCount, pffValidatorCount)
	rawTxs := fixture.Txs(b, pffSignatures)
	height := fixture.App.LastBlockHeight() + 1
	prepareReq := &abci.RequestPrepareProposal{
		Height:     height,
		Time:       fixture.BlockTime,
		Txs:        rawTxs,
		MaxTxBytes: 32 << 20,
	}
	prepared, err := fixture.App.PrepareProposal(prepareReq)
	require.NoError(b, err)
	require.Len(b, prepared.Txs, pffStageCount)
	require.Equal(b, uint64(appconsts.DefaultGovMaxSquareSize), prepared.SquareSize)
	processReq := &abci.RequestProcessProposal{
		Height:       height,
		Time:         fixture.BlockTime,
		Txs:          prepared.Txs,
		SquareSize:   prepared.SquareSize,
		DataRootHash: prepared.DataRootHash,
	}

	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		b.Run("PrepareProposal/"+name, func(b *testing.B) {
			for range b.N {
				b.StopTimer()
				testApp := fixture.NewApp(b)
				if warm {
					admitPFFs(b, testApp, rawTxs)
				} else {
					testApp.PurgeNodeCaches()
				}
				b.StartTimer()
				resp, err := testApp.PrepareProposal(prepareReq)
				b.StopTimer()
				require.NoError(b, err)
				require.Len(b, resp.Txs, pffStageCount)
				require.Equal(b, prepared.DataRootHash, resp.DataRootHash)
			}
		})
		b.Run("ProcessProposal/"+name, func(b *testing.B) {
			for range b.N {
				b.StopTimer()
				testApp := fixture.NewApp(b)
				if warm {
					admitPFFs(b, testApp, rawTxs)
				} else {
					testApp.PurgeNodeCaches()
				}
				b.StartTimer()
				resp, err := testApp.ProcessProposal(processReq)
				b.StopTimer()
				require.NoError(b, err)
				require.Equal(b, abci.ResponseProcessProposal_ACCEPT, resp.Status)
			}
		})
	}

	b.Run("CheckTx/new-batch", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			testApp := fixture.NewApp(b)
			b.StartTimer()
			admitPFFs(b, testApp, rawTxs)
			b.StopTimer()
		}
	})

	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		b.Run("FinalizeBlock/"+name, func(b *testing.B) {
			for range b.N {
				b.StopTimer()
				testApp := fixture.NewApp(b)
				if warm {
					admitPFFs(b, testApp, rawTxs)
				} else {
					testApp.PurgeNodeCaches()
				}
				finalizeReq := &abci.RequestFinalizeBlock{
					Height: height,
					Time:   fixture.BlockTime,
					Hash:   testApp.LastCommitID().Hash,
					Txs:    prepared.Txs,
				}
				b.StartTimer()
				resp, err := testApp.FinalizeBlock(finalizeReq)
				b.StopTimer()
				require.NoError(b, err)
				require.Len(b, resp.TxResults, pffStageCount)
				for i, result := range resp.TxResults {
					require.Equalf(b, abci.CodeTypeOK, result.Code, "tx %d: %s", i, result.Log)
				}
			}
		})
	}
}

func admitPFFs(b *testing.B, testApp *app.App, rawTxs [][]byte) {
	b.Helper()
	for i, tx := range rawTxs {
		resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
		if err != nil || resp.Code != abci.CodeTypeOK {
			b.Fatalf("CheckTx rejected PFF %d: %v: %s", i, err, resp.Log)
		}
	}
}

// BenchmarkPFFSquare_2000 separates classification, square layout, erasure
// encoding, and DAH hashing for the same 256-square proposal.
func BenchmarkPFFSquare_2000(b *testing.B) {
	app.NodeHome = b.TempDir()
	fixture := fibrefactory.NewFixture(b, pffStageCount, pffValidatorCount)
	txs := fixture.Txs(b, pffSignatures)
	maxSquareSize := appconsts.DefaultGovMaxSquareSize

	b.Run("classify", func(b *testing.B) {
		for range b.N {
			_, err := fibretypes.ClassifyTxsForProposal(txs)
			require.NoError(b, err)
		}
	})
	classified, err := fibretypes.ClassifyTxsForProposal(txs)
	require.NoError(b, err)
	b.Run("construct", func(b *testing.B) {
		for range b.N {
			_, err := square.Construct(classified, maxSquareSize, appconsts.SubtreeRootThreshold)
			require.NoError(b, err)
		}
	})
	dataSquare, err := square.Construct(classified, maxSquareSize, appconsts.SubtreeRootThreshold)
	require.NoError(b, err)
	size, err := dataSquare.Size()
	require.NoError(b, err)
	require.Equal(b, maxSquareSize, size)
	shares := share.ToBytes(dataSquare)
	b.Run("extend", func(b *testing.B) {
		for range b.N {
			_, err := da.ExtendSharesWithTreePool(shares, fixture.App.TreePool())
			require.NoError(b, err)
		}
	})
	eds, err := da.ExtendSharesWithTreePool(shares, fixture.App.TreePool())
	require.NoError(b, err)
	b.Run("dah", func(b *testing.B) {
		for range b.N {
			_, err := da.NewDataAvailabilityHeader(eds)
			require.NoError(b, err)
		}
	})
}
