package version

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/upgrade/exported"
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

	vcfg := NewChainVersionConfig(
		map[uint64]int64{
			1: 1,
			2: 2,
		},
	)
	vsetter := &testVersionSetter{}
	configs := map[string]ChainVersionConfig{
		testChainID: vcfg,
	}
	keeper := NewKeeper(vsetter, configs)

	resp := abci.ResponseEndBlock{ConsensusParamUpdates: &abci.ConsensusParams{}}
	resp = EndBlocker(ctx, keeper, resp)
	require.Nil(t, resp.ConsensusParamUpdates.Version)

	ctx = ctx.WithBlockHeight(2)
	resp = abci.ResponseEndBlock{ConsensusParamUpdates: &abci.ConsensusParams{}}
	resp = EndBlocker(ctx, keeper, resp)
	require.NotNil(t, resp.ConsensusParamUpdates.Version)
	require.Equal(t, uint64(2), resp.ConsensusParamUpdates.Version.AppVersion)
	require.Equal(t, uint64(2), vsetter.version)

	ctx = ctx.WithBlockHeight(9999)
	resp = abci.ResponseEndBlock{ConsensusParamUpdates: &abci.ConsensusParams{}}
	resp = EndBlocker(ctx, keeper, resp)
	require.NotNil(t, resp.ConsensusParamUpdates.Version)
	require.Equal(t, uint64(2), resp.ConsensusParamUpdates.Version.AppVersion)
	require.Equal(t, uint64(2), vsetter.version)
}

var _ exported.ProtocolVersionSetter = &testVersionSetter{}

type testVersionSetter struct {
	version uint64
}

func (vs *testVersionSetter) SetProtocolVersion(v uint64) {
	vs.version = v
}
