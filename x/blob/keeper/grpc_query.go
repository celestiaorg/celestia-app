package keeper

import (
	"github.com/celestiaorg/celestia-app/v5/x/blob/types"
)

var _ types.QueryServer = Keeper{}
