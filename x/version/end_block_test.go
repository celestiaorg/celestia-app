package version

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestEndBlock(t *testing.T) {
	testChainID := "test"
	ctx := sdk.Context{}.
		WithBlockHeader(coretypes.Header{
			ChainID: testChainID,
			Version: version.Consensus{App: 1},
			Height:  1,
		}).
		WithChainID(testChainID)

	vcfg, err := NewChainVersionConfig(
		map[uint64]int64{
			1: 1,
			2: 2,
		},
	)
	require.NoError(t, err)
	configs := map[string]ChainVersionConfig{
		testChainID: vcfg,
	}
	keeper := NewKeeper(configs)

	resp := abci.ResponseEndBlock{ConsensusParamUpdates: &abci.ConsensusParams{}}
	resp = EndBlocker(ctx, keeper, resp)
	require.Nil(t, resp.ConsensusParamUpdates.Version)

	ctx = ctx.WithBlockHeight(2)
	resp = abci.ResponseEndBlock{ConsensusParamUpdates: &abci.ConsensusParams{}}
	resp = EndBlocker(ctx, keeper, resp)
	require.NotNil(t, resp.ConsensusParamUpdates.Version)
	require.Equal(t, uint64(2), resp.ConsensusParamUpdates.Version.AppVersion)

	ctx = ctx.WithBlockHeight(9999)
	resp = abci.ResponseEndBlock{ConsensusParamUpdates: &abci.ConsensusParams{}}
	resp = EndBlocker(ctx, keeper, resp)
	require.NotNil(t, resp.ConsensusParamUpdates.Version)
	require.Equal(t, uint64(2), resp.ConsensusParamUpdates.Version.AppVersion)
}
