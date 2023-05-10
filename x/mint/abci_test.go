package mint_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/mint"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

// func TestInflationRate(t *testing.T) {
// 	app, _ := util.SetupTestAppWithGenesisValSet()
// 	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

// 	ctx = ctx.WithBlockHeight(0)
// 	ctx = ctx.WithBlockTime(time.Unix(0, 0).UTC())
// 	mint.BeginBlocker(ctx, app.MintKeeper)
// 	got, err := app.MintKeeper.InflationRate(ctx, &minttypes.QueryInflationRateRequest{})
// 	assert.NoError(t, err)
// 	assert.Equal(t, sdk.NewDecWithPrec(8, 2), got.InflationRate)

// 	ctx = ctx.WithBlockHeight(1)
// 	ctx = ctx.WithBlockTime(time.Now())
// 	mint.BeginBlocker(ctx, app.MintKeeper)
// 	got, err = app.MintKeeper.InflationRate(ctx, &minttypes.QueryInflationRateRequest{})
// 	assert.NoError(t, err)
// 	assert.Equal(t, sdk.NewDecWithPrec(8, 2), got.InflationRate)
// }

func TestGenesisTime(t *testing.T) {
	app, _ := util.SetupTestAppWithGenesisValSet()
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

	unixEpoch := time.Unix(0, 0).UTC()
	ctx = ctx.WithBlockHeight(0)
	ctx = ctx.WithBlockTime(time.Unix(0, 0).UTC())
	mint.BeginBlocker(ctx, app.MintKeeper)
	got, err := app.MintKeeper.GenesisTime(ctx, &minttypes.QueryGenesisTimeRequest{})
	assert.NoError(t, err)
	assert.Equal(t, &unixEpoch, got.GenesisTime)

	fixedTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	ctx = ctx.WithBlockHeight(1)
	ctx = ctx.WithBlockTime(fixedTime)
	mint.BeginBlocker(ctx, app.MintKeeper)
	got, err = app.MintKeeper.GenesisTime(ctx, &minttypes.QueryGenesisTimeRequest{})
	assert.NoError(t, err)
	assert.Equal(t, &fixedTime, got.GenesisTime)

	ctx = ctx.WithBlockHeight(2)
	ctx = ctx.WithBlockTime(fixedTime)
	mint.BeginBlocker(ctx, app.MintKeeper)
	got, err = app.MintKeeper.GenesisTime(ctx, &minttypes.QueryGenesisTimeRequest{})
	assert.NoError(t, err)
	assert.Equal(t, &fixedTime, got.GenesisTime)
}
