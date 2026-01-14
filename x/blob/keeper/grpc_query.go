package keeper

import (
	"github.com/celestiaorg/celestia-app/v7/x/blob/types"
)

var _ types.QueryServer = Keeper{}
