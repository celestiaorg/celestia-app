package keeper

import (
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
)

var _ types.QueryServer = Keeper{}
