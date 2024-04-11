package packetforward_test

import (
	"strings"
	"testing"

	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v6/router/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

// TestPacketForwardMiddlewareAgainstAppUpgrades verifies that the PFM module's params are overridden during an
// upgrade from v1 -> v2.
func TestPacketForwardMiddlewareAgainstAppUpgrades(t *testing.T) {
	testApp, _ := util.SetupTestApp(t, 3)
	supportedVersions := []uint64{v1.Version, v2.Version}
	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	ctx := testApp.NewContext(true, tmproto.Header{
		Version: version.Consensus{
			App: 1,
		},
	})
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: version.Consensus{App: 1},
	}})

	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())

	// PacketForwardMiddleware should not have been set yet
	gotBefore, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: packetforwardtypes.ModuleName,
		Key:      string(packetforwardtypes.KeyFeePercentage),
	})
	require.Equal(t, "", gotBefore.Param.Value)
	require.NoError(t, err)

	// now the app version changes
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	testApp.Commit()

	require.NotNil(t, respEndBlock.ConsensusParamUpdates.Version)
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion())

	// create a new context after endBlock
	newCtx := testApp.NewContext(true, tmproto.Header{
		Version: version.Consensus{
			App: 2,
		},
	})

	got, err := testApp.ParamsKeeper.Params(newCtx, &proposal.QueryParamsRequest{
		Subspace: packetforwardtypes.ModuleName,
		Key:      string(packetforwardtypes.KeyFeePercentage),
	})
	require.NoError(t, err)

	require.NoError(t, err)
	require.Equal(t, "0.000000000000000000", strings.Trim(got.Param.Value, "\""))
}
