package app

import (
	"context"
	"fmt"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v6/x/minfee/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	ibcconnectiontypes "github.com/cosmos/ibc-go/v8/modules/core/03-connection/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

// RegisterUpgradeHandlers is used for registering any on-chain upgrades.
func (app App) RegisterUpgradeHandlers() {
	for _, subspace := range app.ParamsKeeper.GetSubspaces() {
		var keyTable paramstypes.KeyTable
		var set bool

		switch subspace.Name() {
		case authtypes.ModuleName:
			keyTable, set = authtypes.ParamKeyTable(), true //nolint:staticcheck
		case banktypes.ModuleName:
			keyTable, set = banktypes.ParamKeyTable(), true //nolint:staticcheck
		case stakingtypes.ModuleName:
			keyTable, set = stakingtypes.ParamKeyTable(), true //nolint:staticcheck
		case minttypes.ModuleName:
			keyTable, set = minttypes.ParamKeyTable(), true //nolint:staticcheck
		case distrtypes.ModuleName:
			keyTable, set = distrtypes.ParamKeyTable(), true //nolint:staticcheck
		case slashingtypes.ModuleName:
			keyTable, set = slashingtypes.ParamKeyTable(), true //nolint:staticcheck
		case govtypes.ModuleName:
			keyTable, set = govv1.ParamKeyTable(), true //nolint:staticcheck
		case ibcexported.ModuleName:
			keyTable, set = ibcclienttypes.ParamKeyTable(), true
			keyTable.RegisterParamSet(&ibcconnectiontypes.Params{})
		case ibctransfertypes.ModuleName:
			keyTable, set = ibctransfertypes.ParamKeyTable(), true //nolint:staticcheck
		case icahosttypes.SubModuleName:
			keyTable, set = icahosttypes.ParamKeyTable(), true //nolint:staticcheck
		case blobtypes.ModuleName:
			keyTable, set = blobtypes.ParamKeyTable(), true //nolint:staticcheck
		case minfeetypes.ModuleName:
			keyTable, set = minfeetypes.ParamKeyTable(), true //nolint:staticcheck
		default:
			set = false
		}

		if !subspace.HasKeyTable() && set {
			subspace.WithKeyTable(keyTable)
		}
	}

	upgradeName := fmt.Sprintf("v%d", appconsts.Version)
	app.UpgradeKeeper.SetUpgradeHandler(
		upgradeName,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			sdkCtx := sdk.UnwrapSDKContext(ctx)

			start := time.Now()
			sdkCtx.Logger().Info("running upgrade handler", "upgrade-name", upgradeName, "start", start)

			err := app.setICAHostParams(sdkCtx)
			if err != nil {
				sdkCtx.Logger().Error("failed to set ica/host submodule params", "error", err)
				return nil, err
			}
			// TODO: add any other migrations here.

			sdkCtx.Logger().Info("finished to upgrade", "upgrade-name", upgradeName, "duration-sec", time.Since(start).Seconds())

			return fromVM, nil
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}

	if upgradeInfo.Name == upgradeName && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) { //nolint:staticcheck
		// TODO: Apply any store upgrades here.
	}
}

// setICAHostParams sets the ICA host params to the values defined in CIP-14.
// This is needed because the ICA host params were previously stored in x/params
// and in ibc-go v8 they were migrated to use a self-managed store.
//
// NOTE: the param migrator included in ibc-go v8 does not work as expected
// because it sets the params to the default values which do not match the
// values defined in CIP-14.
func (a App) setICAHostParams(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := icahosttypes.Params{
		HostEnabled:   true,
		AllowMessages: IcaAllowMessages(),
	}
	a.ICAHostKeeper.SetParams(sdkCtx, params)
	return nil
}
