package keeper

import (
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

var _ types.QueryServer = Keeper{}
