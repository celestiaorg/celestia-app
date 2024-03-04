package app

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/go-square/shares"
	"github.com/celestiaorg/go-square/square"
	"github.com/celestiaorg/rsmt2d"
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// ExtendBlock extends the given block data into a data square for a given app
// version.
func (app *App) ExtendBlock(data coretypes.Data, sdkCtx sdk.Context) (*rsmt2d.ExtendedDataSquare, error) {
	appVersion := app.GetBaseApp().AppVersion(sdkCtx)
	// Construct the data square from the block's transactions
	dataSquare, err := square.Construct(
		data.Txs.ToSliceOfBytes(),
		app.GovSquareSizeUpperBound(sdkCtx),
		appconsts.SubtreeRootThreshold(appVersion),
	)
	if err != nil {
		return nil, err
	}

	return da.ExtendShares(shares.ToBytes(dataSquare))
}

// EmptyBlock returns true if the given block data is considered empty by the
// application at a given version.
func IsEmptyBlock(data coretypes.Data, _ uint64) bool {
	return len(data.Txs) == 0
}
