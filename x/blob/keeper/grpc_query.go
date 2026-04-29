package keeper

import (
	"github.com/celestiaorg/celestia-app/v9/x/blob/types"
)

var _ types.QueryServer = Keeper{}
