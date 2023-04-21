package version

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Keeper struct {
	chainAppVersions map[string]ChainVersionConfig
}

func NewKeeper(nonStandardVersions map[string]ChainVersionConfig) Keeper {
	vs := StandardChainVersions()
	for k, v := range nonStandardVersions {
		vs[k] = v
	}
	return Keeper{
		chainAppVersions: vs,
	}
}

func (k Keeper) GetVersion(ctx sdk.Context) uint64 {
	vs, has := k.chainAppVersions[ctx.ChainID()]
	if !has {
		return ctx.BlockHeader().Version.App
	}
	return vs.GetVersion(ctx.BlockHeight())
}
