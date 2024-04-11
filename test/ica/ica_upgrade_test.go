package ica_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	icahosttypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

// TestICA verifies that the ICA module's params are overridden during an
// upgrade from v1 -> v2.
func TestICA(t *testing.T) {
	testApp, _ := util.SetupTestApp(t, 3)
	ctx := testApp.NewContext(true, tmproto.Header{
		Version: version.Consensus{
			App: 1,
		},
	})
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: version.Consensus{App: 1},
	}})
	require.EqualValues(t, 1, testApp.AppVersion())

	// Query the ICA host module params
	gotBefore, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: icahosttypes.SubModuleName,
		Key:      string(icahosttypes.KeyHostEnabled),
	})
	require.NoError(t, err)
	require.Equal(t, "", gotBefore.Param.Value)

	// Upgrade from v1 -> v2
	testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	testApp.Commit()
	require.EqualValues(t, 2, testApp.AppVersion())

	newCtx := testApp.NewContext(true, tmproto.Header{Version: version.Consensus{App: 2}})
	got, err := testApp.ParamsKeeper.Params(newCtx, &proposal.QueryParamsRequest{
		Subspace: icahosttypes.SubModuleName,
		Key:      string(icahosttypes.KeyHostEnabled),
	})
	require.NoError(t, err)
	require.Equal(t, "true", got.Param.Value)
}
