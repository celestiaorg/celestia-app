package keeper

import (
	"github.com/rootulp/celestia-app/x/blob/types"
)

var _ types.QueryServer = Keeper{}
