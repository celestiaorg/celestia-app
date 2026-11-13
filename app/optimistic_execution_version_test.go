package app_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	blobproto "github.com/celestiaorg/go-square/v4/proto/blob/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestOptimisticExecutionUsesBlockAppVersion checks that optimistic execution
// unwraps BlobTxs under the rules of the app version ProcessProposal ran with.
func TestOptimisticExecutionUsesBlockAppVersion(t *testing.T) {
	inner := []byte("inner tx")
	// A BlobTx under the legacy rules only: v11 rejects its share version.
	legacyOnly, err := proto.Marshal(&blobproto.BlobTx{
		Tx:     inner,
		Blobs:  []*blobproto.BlobProto{{NamespaceId: make([]byte, share.NamespaceIDSize), Data: []byte{1}, ShareVersion: 7}},
		TypeId: blobtx.ProtoBlobTxTypeID,
	})
	require.NoError(t, err)

	for appVersion, want := range map[uint64][]byte{10: inner, appconsts.Version: legacyOnly} {
		t.Run(fmt.Sprintf("v%d", appVersion), func(t *testing.T) {
			testApp := app.New(log.NewNopLogger(), tmdb.NewMemDB(), nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
			cparams := app.DefaultConsensusParams()
			cparams.Version.App = appVersion
			genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
			testApp = util.InitialiseTestAppWithGenesis(testApp, cparams, genesisState)
			runStateFixBlock(t, testApp, 1, false)

			req := stateFixBlock(2)
			prop, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{Height: req.Height, Time: req.Time})
			require.NoError(t, err)
			pp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Txs: prop.Txs, SquareSize: prop.SquareSize, DataRootHash: prop.DataRootHash,
				Hash: []byte("block-2"), Height: req.Height, Time: req.Time, Header: req.Header,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, pp.Status)
			require.Equal(t, [][]byte{want}, testApp.OptimisticExecutionTxs([][]byte{legacyOnly}))
		})
	}
}
