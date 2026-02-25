package keeper

import (
	"github.com/celestiaorg/celestia-app/v8/x/blob/types"
)

var _ types.QueryServer = Keeper{}
