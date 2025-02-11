package app

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	tmcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icagenesistypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/genesis/types"
	"github.com/stretchr/testify/assert"
)

// Test_newGovModule verifies that the gov module's genesis state has defaults
// overridden.
func Test_newGovModule(t *testing.T) {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	day := time.Hour * 24
	oneWeek := day * 7

	gm := govModule{}
	raw := gm.DefaultGenesis(encCfg.Codec)
	govGenesisState := govtypes.GenesisState{}

	encCfg.Codec.MustUnmarshalJSON(raw, &govGenesisState)

	want := []types.Coin{{
		Denom:  BondDenom,
		Amount: math.NewInt(10_000_000_000),
	}}

	assert.Equal(t, want, govGenesisState.Params.MinDeposit)
	assert.Equal(t, oneWeek, *govGenesisState.Params.MaxDepositPeriod)
	assert.Equal(t, oneWeek, *govGenesisState.Params.VotingPeriod)
}

func TestDefaultAppConfig(t *testing.T) {
	cfg := DefaultAppConfig()

	assert.False(t, cfg.API.Enable)
	assert.False(t, cfg.GRPC.Enable)
	assert.False(t, cfg.GRPCWeb.Enable)

	assert.Equal(t, uint64(1500), cfg.StateSync.SnapshotInterval)
	assert.Equal(t, uint32(2), cfg.StateSync.SnapshotKeepRecent)
	assert.Equal(t, "0.002utia", cfg.MinGasPrices)

	assert.Equal(t, 20*mebibyte, cfg.GRPC.MaxRecvMsgSize)
}

func TestDefaultConsensusConfig(t *testing.T) {
	got := DefaultConsensusConfig()

	t.Run("mempool overrides", func(t *testing.T) {
		want := tmcfg.MempoolConfig{
			// defaults
			Broadcast:             tmcfg.DefaultMempoolConfig().Broadcast,
			CacheSize:             tmcfg.DefaultMempoolConfig().CacheSize,
			KeepInvalidTxsInCache: tmcfg.DefaultMempoolConfig().KeepInvalidTxsInCache,
			Recheck:               tmcfg.DefaultMempoolConfig().Recheck,
			RootDir:               tmcfg.DefaultMempoolConfig().RootDir,
			Size:                  tmcfg.DefaultMempoolConfig().Size,
			WalPath:               tmcfg.DefaultMempoolConfig().WalPath,
			RecheckTimeout:        1_000_000_000,

			// Overrides
			MaxTxBytes:  2 * mebibyte,
			MaxTxsBytes: 80 * mebibyte,
			// TTLDuration:  75 * time.Second, // TODO: check if priority mempool is needed
			// TTLNumBlocks: 12,
			Type: "flood",
		}
		assert.Equal(t, want, *got.Mempool)
	})

	t.Run("p2p overrides", func(t *testing.T) {
		const mebibyte = 1048576
		assert.Equal(t, int64(10*mebibyte), got.P2P.SendRate)
		assert.Equal(t, int64(10*mebibyte), got.P2P.RecvRate)
	})
}

func Test_icaDefaultGenesis(t *testing.T) {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	ica := icaModule{}
	raw := ica.DefaultGenesis(encCfg.Codec)
	got := icagenesistypes.GenesisState{}
	encCfg.Codec.MustUnmarshalJSON(raw, &got)

	assert.Equal(t, got.HostGenesisState.Params.AllowMessages, icaAllowMessages())
	assert.True(t, got.HostGenesisState.Params.HostEnabled)
	assert.False(t, got.ControllerGenesisState.Params.ControllerEnabled)
}
