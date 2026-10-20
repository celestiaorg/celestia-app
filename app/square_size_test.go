package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestMaxEffectiveSquareSize(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), testfactory.GenerateAccounts(1)...)
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: testApp.LastBlockHeight() + 1})

	chainIDs := []string{
		appconsts.TestChainID,
		appconsts.CortoChainID,
		appconsts.MochaChainID,
		appconsts.MainnetChainID,
		appconsts.TalisChainID,
	}
	testCases := []struct {
		name      string
		govMax    uint64
		wantLimit int
	}{
		{"gov max of one", 1, 1},
		{"gov max below hard bound", 256, 256},
		{"gov max equal to hard bound", 512, 512},
		{"gov max above hard bound", 1024, 512},
	}
	for _, chainID := range chainIDs {
		for _, tc := range testCases {
			t.Run(chainID+"/"+tc.name, func(t *testing.T) {
				params := testApp.BlobKeeper.GetParams(ctx)
				params.GovMaxSquareSize = tc.govMax
				testApp.BlobKeeper.SetParams(ctx, params)

				got := testApp.MaxEffectiveSquareSize(ctx.WithChainID(chainID))
				require.Equal(t, tc.wantLimit, got)
			})
		}
	}
}
