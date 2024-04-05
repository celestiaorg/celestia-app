package keeper

import (
	"github.com/celestiaorg/celestia-app/v2/x/blob/types"
)

var _ types.QueryServer = Keeper{}
