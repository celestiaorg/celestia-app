package version

import sdk "github.com/cosmos/cosmos-sdk/types"

type Keeper struct {
	getters map[string]VersionGetter
}

func NewKeeper() Keeper {
	return Keeper{
		getters: vg,
	}
}

func (k Keeper) GetVersion(ctx sdk.Context, height int64) uint64 {
	return k.vg.GetVersion(height)
}
