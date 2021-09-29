package keeper

import (
	"github.com/celestiaorg/celestia-app/x/payment/types"
)

var _ types.QueryServer = Keeper{}
