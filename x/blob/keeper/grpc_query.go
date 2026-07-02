package keeper

import (
	"github.com/celestiaorg/celestia-app/v10/x/blob/types"
)

var _ types.QueryServer = Keeper{}
