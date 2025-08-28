package app

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	tmcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icagenesistypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/genesis/types"
	"github.com/stretchr/testify/assert"
)

// Test_newGovModule verifies that the gov module's genesis state has defaults
// overridden.
func Test_newGovModule(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	day := time.Hour * 24
	oneWeek := day * 7

	gm := govModule{}
	raw := gm.DefaultGenesis(enc.Codec)
	govGenesisState := govtypes.GenesisState{}

	enc.Codec.MustUnmarshalJSON(raw, &govGenesisState)

	wantMinDeposit := []types.Coin{{
		Denom:  params.BondDenom,
		Amount: math.NewInt(10_000_000_000),
	}}
	wantExpeditedMinDeposit := []types.Coin{{
		Denom:  params.BondDenom,
		Amount: math.NewInt(50_000_000_000),
	}}

	assert.Equal(t, wantMinDeposit, govGenesisState.Params.MinDeposit)
	assert.Equal(t, oneWeek, *govGenesisState.Params.MaxDepositPeriod)
	assert.Equal(t, oneWeek, *govGenesisState.Params.VotingPeriod)
	assert.Equal(t, wantExpeditedMinDeposit, govGenesisState.Params.ExpeditedMinDeposit)
}

func TestDefaultAppConfig(t *testing.T) {
	cfg := DefaultAppConfig()

	assert.False(t, cfg.API.Enable)
	assert.False(t, cfg.GRPC.Enable)
	assert.False(t, cfg.GRPCWeb.Enable)

	assert.Equal(t, uint64(1500), cfg.StateSync.SnapshotInterval)
	assert.Equal(t, uint32(2), cfg.StateSync.SnapshotKeepRecent)
	assert.Equal(t, "", cfg.MinGasPrices)

	assert.Equal(t, appconsts.DefaultUpperBoundMaxBytes*2, cfg.GRPC.MaxRecvMsgSize)
}

func TestDefaultConsensusConfig(t *testing.T) {
	got := DefaultConsensusConfig()

	t.Run("RPC overrides", func(t *testing.T) {
		want := tmcfg.DefaultRPCConfig()
		want.TimeoutBroadcastTxCommit = 50 * time.Second
		want.MaxBodyBytes = appconsts.MempoolSize + (32 * mebibyte)
		want.GRPCListenAddress = "tcp://127.0.0.1:9098"

		assert.Equal(t, want, got.RPC)
	})

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
			MaxTxBytes:     appconsts.MaxTxSize,
			MaxTxsBytes:    appconsts.MempoolSize,
			TTLDuration:    0 * time.Second,
			TTLNumBlocks:   12,
			Type:           tmcfg.MempoolTypeCAT,
			MaxGossipDelay: time.Second * 60,
		}
		assert.Equal(t, want, *got.Mempool)
	})

	t.Run("p2p overrides", func(t *testing.T) {
		const mebibyte = 1048576
		assert.Equal(t, int64(24*mebibyte), got.P2P.SendRate)
		assert.Equal(t, int64(24*mebibyte), got.P2P.RecvRate)
	})
}

func Test_icaDefaultGenesis(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	ica := icaModule{}
	raw := ica.DefaultGenesis(enc.Codec)
	got := icagenesistypes.GenesisState{}
	enc.Codec.MustUnmarshalJSON(raw, &got)

	assert.Equal(t, got.HostGenesisState.Params.AllowMessages, IcaAllowMessages())
	assert.True(t, got.HostGenesisState.Params.HostEnabled)
	assert.False(t, got.ControllerGenesisState.Params.ControllerEnabled)
}

func TestEvidenceParams(t *testing.T) {
	got := EvidenceParams()
	mebibyte := int64(1048576)

	assert.Equal(t, appconsts.MaxAgeDuration, got.MaxAgeDuration)
	assert.Equal(t, int64(appconsts.MaxAgeNumBlocks), got.MaxAgeNumBlocks)
	assert.Equal(t, mebibyte, got.MaxBytes)
}
