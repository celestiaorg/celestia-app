package keeper

import (
	"github.com/celestiaorg/celestia-app/x/blob/types"
)

var _ types.QueryServer = Keeper{}
