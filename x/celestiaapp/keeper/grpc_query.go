package keeper

import (
	"github.com/celestiaorg/celestia-app/x/celestiaapp/types"
)

var _ types.QueryServer = Keeper{}
