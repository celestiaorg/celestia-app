package keeper

import (
	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
)

var _ types.QueryServer = Keeper{}
