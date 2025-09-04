package types

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// HyperlaneKeeper defines the expected hyperlane keeper interface.
type HyperlaneKeeper interface {
	IsmRouter() *util.Router[util.InterchainSecurityModule]
}

type StakingKeeper interface {
	GetHistoricalInfo(ctx context.Context, height int64) (stakingtypes.HistoricalInfo, error)
}
