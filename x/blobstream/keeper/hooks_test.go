package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/test/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestAfterValidatorBeginUnbonding(t *testing.T) {
	testEnv := util.CreateTestEnv(t)
	height := int64(1)

	t.Run("should be a no-op if app version is 2", func(t *testing.T) {
		ctx := testEnv.Context.WithBlockHeader(tmproto.Header{Version: version.Consensus{App: 2}, Height: height})
		err := testEnv.BlobstreamKeeper.Hooks().AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress{}, sdk.ValAddress{})
		assert.NoError(t, err)

		got := testEnv.BlobstreamKeeper.GetLatestUnBondingBlockHeight(ctx)
		assert.Equal(t, uint64(0), got)
	})
	t.Run("should set latest unboding height if app version is 1", func(t *testing.T) {
		ctx := testEnv.Context.WithBlockHeader(tmproto.Header{Version: version.Consensus{App: 1}, Height: height})
		err := testEnv.BlobstreamKeeper.Hooks().AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress{}, sdk.ValAddress{})
		assert.NoError(t, err)

		got := testEnv.BlobstreamKeeper.GetLatestUnBondingBlockHeight(ctx)
		assert.Equal(t, uint64(height), got)
	})
}

func TestAfterValidatorCreated(t *testing.T) {
	testEnv := util.CreateTestEnv(t)
	height := int64(1)
	valAddress := sdk.ValAddress([]byte("valAddress"))
	t.Run("should be a no-op if app version is 2", func(t *testing.T) {
		ctx := testEnv.Context.WithBlockHeader(tmproto.Header{Version: version.Consensus{App: 2}, Height: height})
		err := testEnv.BlobstreamKeeper.Hooks().AfterValidatorCreated(ctx, valAddress)
		assert.NoError(t, err)

		address, ok := testEnv.BlobstreamKeeper.GetEVMAddress(ctx, valAddress)
		assert.False(t, ok)
		assert.Empty(t, address)
	})
	t.Run("should set EVM address if app version is 1", func(t *testing.T) {
		ctx := testEnv.Context.WithBlockHeader(tmproto.Header{Version: version.Consensus{App: 1}, Height: height})
		err := testEnv.BlobstreamKeeper.Hooks().AfterValidatorCreated(ctx, valAddress)
		assert.NoError(t, err)

		address, ok := testEnv.BlobstreamKeeper.GetEVMAddress(ctx, valAddress)
		assert.True(t, ok)
		assert.Equal(t, common.HexToAddress("0x0000000000000000000076616C41646472657373"), address)
	})
}
