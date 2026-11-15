package keeper

import (
	"github.com/celestiaorg/celestia-app/v11/x/blob/types"
)

var _ types.QueryServer = Keeper{}
