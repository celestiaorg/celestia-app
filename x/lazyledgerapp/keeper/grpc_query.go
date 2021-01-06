package keeper

import (
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
)

var _ types.QueryServer = Keeper{}
