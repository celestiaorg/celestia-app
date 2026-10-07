//go:build benchmarks

package benchmarks_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/fibrefactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

const (
	pffPerBlock       = 5800
	pffValidatorCount = 100
	pffSignatures     = 67
)

// BenchmarkProcessProposal_PFF measures the validator path for a full PayForFibre block.
func BenchmarkProcessProposal_PFF(b *testing.B) {
	app.NodeHome = b.TempDir()
	fixture := fibrefactory.NewFixture(b, pffPerBlock, pffValidatorCount)
	require.Len(b, fixture.ValPrivs, pffValidatorCount)
	rawTxs := fixture.Txs(b, pffSignatures)
	var totalBytes int
	for _, tx := range rawTxs {
		totalBytes += len(tx)
	}
	require.LessOrEqual(b, totalBytes, 32*1024*1024)

	height := fixture.App.LastBlockHeight() + 1
	prepared, err := fixture.App.PrepareProposal(&abci.RequestPrepareProposal{
		Height: height,
		Time:   fixture.BlockTime,
		Txs:    rawTxs,
	})
	require.NoError(b, err)
	if len(prepared.Txs) != pffPerBlock {
		b.Fatalf("PrepareProposal included %d of %d PayForFibre txs", len(prepared.Txs), pffPerBlock)
	}
	require.Equal(b, uint64(appconsts.DefaultGovMaxSquareSize), prepared.SquareSize)
	req := &abci.RequestProcessProposal{
		Height:       height,
		Time:         fixture.BlockTime,
		Txs:          prepared.Txs,
		SquareSize:   prepared.SquareSize,
		DataRootHash: prepared.DataRootHash,
	}
	// Consume the proposer's local proposal artifacts before measuring a validator.
	acceptPFFProposal(b, fixture.App, req)

	b.Run("cold", func(b *testing.B) {
		for b.Loop() {
			b.StopTimer()
			fixture.App.PurgeNodeCaches()
			b.StartTimer()
			acceptPFFProposal(b, fixture.App, req)
		}
	})
	b.Run("warm", func(b *testing.B) {
		fixture.App.PurgeNodeCaches()
		for _, tx := range rawTxs {
			resp, err := fixture.App.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
			if err != nil || resp.Code != 0 {
				b.Fatalf("CheckTx rejected PFF: %v: %s", err, resp.Log)
			}
		}
		for b.Loop() {
			acceptPFFProposal(b, fixture.App, req)
		}
	})
}

func acceptPFFProposal(b *testing.B, testApp *app.App, req *abci.RequestProcessProposal) {
	b.Helper()
	resp, err := testApp.ProcessProposal(req)
	if err != nil {
		b.Fatal(err)
	}
	if resp.Status != abci.ResponseProcessProposal_ACCEPT {
		b.Fatalf("ProcessProposal rejected the block: %v", resp.Status)
	}
}
