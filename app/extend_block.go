package app

import (
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	square "github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/rsmt2d"
	coretypes "github.com/tendermint/tendermint/types"
)

// ExtendBlock extends the given block data into a data square for a given app
// version.
func ExtendBlock(data coretypes.Data, appVersion uint64, chainID string) (*rsmt2d.ExtendedDataSquare, error) {
	// Construct the data square from the block's transactions
	dataSquare, err := square.Construct(
		data.Txs.ToSliceOfBytes(),
		appconsts.SquareSizeUpperBound(chainID, appVersion),
		appconsts.SubtreeRootThreshold(appVersion),
	)
	if err != nil {
		return nil, err
	}

	return da.ExtendShares(share.ToBytes(dataSquare))
}

// IsEmptyBlock returns true if the given block data is considered empty by the
// application at a given version.
//
// Deprecated: Use IsEmptyBlockRef for better performance with large data structures.
func IsEmptyBlock(data coretypes.Data, _ uint64) bool {
	return len(data.Txs) == 0
}

// IsEmptyBlockRef returns true if the application considers the given block data
// empty at a given version.
// This method passes the block data by reference for improved performance.
func IsEmptyBlockRef(data *coretypes.Data, _ uint64) bool {
	return len(data.Txs) == 0
}
