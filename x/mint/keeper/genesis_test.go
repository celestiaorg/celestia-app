package keeper_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

var cdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

// initChainWithMint runs InitChain at util.GenesisTime with the given mint genesis JSON.
func initChainWithMint(mintGenesis json.RawMessage) (*app.App, *tmtypes.ValidatorSet) {
	testApp := util.NewTestApp()
	genesisState, valSet, _ := util.GenesisStateWithSingleValidator(testApp)
	genesisState[minttypes.ModuleName] = mintGenesis
	util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	return testApp, valSet
}

// finalizeBlock executes and commits the next block and returns the minted amount, if any.
func finalizeBlock(t *testing.T, testApp *app.App, valSet *tmtypes.ValidatorSet, blockTime time.Time) (math.Int, bool) {
	resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:               blockTime,
		Height:             testApp.LastBlockHeight() + 1,
		Hash:               testApp.LastCommitID().Hash,
		NextValidatorsHash: valSet.Hash(),
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)

	for _, event := range resp.Events {
		if event.Type != minttypes.EventTypeMint {
			continue
		}
		for _, attr := range event.Attributes {
			if attr.Key == sdk.AttributeKeyAmount {
				amount, ok := math.NewIntFromString(attr.Value)
				require.True(t, ok)
				return amount, true
			}
		}
	}
	return math.Int{}, false
}

func TestImportedMinter(t *testing.T) {
	restart := util.GenesisTime
	nextBlock := restart.Add(15 * time.Second)
	staleBlockTime := restart.Add(-90 * 24 * time.Hour)
	exportedProvisions := math.LegacyNewDec(1_000_000_000_000)

	tests := []struct {
		name            string
		mintGenesisTime time.Time
		wantRate        math.LegacyDec
		keepProvisions  bool
	}{
		{
			name:            "same mint year keeps exported provisions",
			mintGenesisTime: restart.Add(-200 * 24 * time.Hour),
			wantRate:        minttypes.InitialInflationRateAsDec(),
			keepProvisions:  true,
		},
		{
			name:            "genesis time equal to block time is accepted",
			mintGenesisTime: restart,
			wantRate:        minttypes.InitialInflationRateAsDec(),
			keepProvisions:  true,
		},
		{
			name:            "restart after mint anniversary uses scheduled rate",
			mintGenesisTime: restart.Add(-oneYear - 10*24*time.Hour),
			wantRate:        math.LegacyMustNewDecFromStr("0.0249111"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			minter := minttypes.NewMinter(minttypes.InitialInflationRateAsDec(), exportedProvisions, appconsts.BondDenom)
			minter.PreviousBlockTime = &staleBlockTime
			testApp, valSet := initChainWithMint(cdc.MustMarshalJSON(&minttypes.GenesisState{
				BondDenom:   appconsts.BondDenom,
				Minter:      &minter,
				GenesisTime: &tc.mintGenesisTime,
			}))

			_, minted := finalizeBlock(t, testApp, valSet, restart)
			require.False(t, minted, "first block after import must not mint")

			ctx := testApp.NewUncachedContext(false, tmproto.Header{})
			got := testApp.MintKeeper.GetMinter(ctx)
			require.Equal(t, tc.wantRate, got.InflationRate)
			wantProvisions := tc.wantRate.MulInt(testApp.MintKeeper.StakingTokenSupply(ctx))
			if tc.keepProvisions {
				wantProvisions = exportedProvisions
			}
			require.Equal(t, wantProvisions, got.AnnualProvisions)
			require.Equal(t, tc.mintGenesisTime, *testApp.MintKeeper.GetGenesisTime(ctx).GenesisTime)

			amount, minted := finalizeBlock(t, testApp, valSet, nextBlock)
			require.True(t, minted)
			portion := math.LegacyNewDec(nextBlock.Sub(restart).Nanoseconds()).Quo(math.LegacyNewDec(minttypes.NanosecondsPerYear))
			require.Equal(t, wantProvisions.Mul(portion).TruncateInt(), amount)

			ctx = testApp.NewUncachedContext(false, tmproto.Header{})
			stored := testApp.MintKeeper.GetMinter(ctx)
			exported := testApp.MintKeeper.ExportGenesis(ctx)
			require.Nil(t, exported.Minter.PreviousBlockTime)
			require.Equal(t, stored.InflationRate, exported.Minter.InflationRate)
			require.Equal(t, stored.AnnualProvisions, exported.Minter.AnnualProvisions)
			require.Equal(t, tc.mintGenesisTime, *exported.GenesisTime)
			require.NoError(t, minttypes.ValidateGenesis(*exported))
		})
	}
}

func TestInitGenesisInvalid(t *testing.T) {
	tests := []struct {
		name string
		// modify breaks a valid exported mint genesis.
		modify func(gs *minttypes.GenesisState)
		// raw replaces the genesis JSON when modify cannot express the case.
		raw string
		// wantValidateErr is empty when ValidateGenesis cannot catch the error.
		wantValidateErr string
		wantPanic       string
	}{
		{
			name:            "minter without genesis time",
			modify:          func(gs *minttypes.GenesisState) { gs.GenesisTime = nil },
			wantValidateErr: "minter and genesis time must be set together",
		},
		{
			name:            "genesis time without minter",
			modify:          func(gs *minttypes.GenesisState) { gs.Minter = nil },
			wantValidateErr: "minter and genesis time must be set together",
		},
		{
			name:            "missing inflation rate",
			raw:             `{"bond_denom":"utia","minter":{"annual_provisions":"0","bond_denom":"utia"},"genesis_time":"2023-01-01T00:00:00Z"}`,
			wantValidateErr: "minter inflation rate and annual provisions must be set",
		},
		{
			name:            "empty bond denom",
			raw:             `{"bond_denom":""}`,
			wantValidateErr: "bond denom cannot be empty",
		},
		{
			name:            "missing annual provisions",
			raw:             `{"bond_denom":"utia","minter":{"inflation_rate":"0.08","bond_denom":"utia"},"genesis_time":"2023-01-01T00:00:00Z"}`,
			wantValidateErr: "minter inflation rate and annual provisions must be set",
		},
		{
			name:            "negative inflation rate",
			modify:          func(gs *minttypes.GenesisState) { gs.Minter.InflationRate = math.LegacyNewDec(-1) },
			wantValidateErr: "inflation rate -1.000000000000000000 should be positive",
		},
		{
			name:            "negative annual provisions",
			modify:          func(gs *minttypes.GenesisState) { gs.Minter.AnnualProvisions = math.LegacyNewDec(-1) },
			wantValidateErr: "annual provisions -1.000000000000000000 should be positive",
		},
		{
			name:            "bond denom mismatch",
			modify:          func(gs *minttypes.GenesisState) { gs.Minter.BondDenom = "other" },
			wantValidateErr: `minter bond denom "other" does not match bond denom "utia"`,
		},
		{
			name: "genesis time after block time",
			modify: func(gs *minttypes.GenesisState) {
				later := util.GenesisTime.Add(time.Second)
				gs.GenesisTime = &later
			},
			wantPanic: "invalid mint genesis: genesis time 2023-01-01 01:01:02.000000001 +0000 UTC is after block time 2023-01-01 01:01:01.000000001 +0000 UTC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(tc.raw)
			if tc.modify != nil {
				minter := minttypes.DefaultMinter()
				genesisTime := util.GenesisTime.Add(-time.Hour)
				gs := &minttypes.GenesisState{BondDenom: minter.BondDenom, Minter: &minter, GenesisTime: &genesisTime}
				tc.modify(gs)
				raw = cdc.MustMarshalJSON(gs)
			}
			var decoded minttypes.GenesisState
			cdc.MustUnmarshalJSON(raw, &decoded)

			wantPanic := tc.wantPanic
			if tc.wantValidateErr != "" {
				require.EqualError(t, minttypes.ValidateGenesis(decoded), tc.wantValidateErr)
				wantPanic = "invalid mint genesis: " + tc.wantValidateErr
			} else {
				require.NoError(t, minttypes.ValidateGenesis(decoded))
			}
			require.PanicsWithError(t, wantPanic, func() { initChainWithMint(raw) })
		})
	}
}
