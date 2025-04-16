package app

import (
	"context"
	"time"

	storetypes "cosmossdk.io/store/types"
	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	cmttypes "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
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

	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

// UpgradeName defines the on-chain upgrade name from v3 to v4.
// IMPORTANT: UpgradeName must be formated as `v`+ app version.
const UpgradeName = "v4"

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

	baseAppLegacySS := app.ParamsKeeper.Subspace(baseapp.Paramspace).WithKeyTable(paramstypes.ConsensusParamsKeyTable())

	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeName,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			sdkCtx := sdk.UnwrapSDKContext(ctx)

			start := time.Now()
			sdkCtx.Logger().Info("running upgrade handler", "upgrade-name", UpgradeName, "start", start)

			// migrate consensus params from the legacy params keeper to consensus params module
			oldConsensusParams := baseapp.GetConsensusParams(sdkCtx, baseAppLegacySS)
			if oldConsensusParams != nil {
				if oldConsensusParams.Version == nil {
					oldConsensusParams.Version = &cmttypes.VersionParams{
						App: 3,
					}
				}

				if err := app.ConsensusKeeper.ParamsStore.Set(ctx, *oldConsensusParams); err != nil {
					return nil, err
				}
			}

			// block by default msg upgrade proposal from circuit breaker
			if err := app.CircuitKeeper.DisableList.Set(ctx, sdk.MsgTypeURL(&upgradetypes.MsgSoftwareUpgrade{})); err != nil {
				return nil, err
			}
			if err := app.CircuitKeeper.DisableList.Set(ctx, sdk.MsgTypeURL(&upgradetypes.MsgCancelUpgrade{})); err != nil {
				return nil, err
			}
			if err := app.CircuitKeeper.DisableList.Set(ctx, sdk.MsgTypeURL(&ibcclienttypes.MsgIBCSoftwareUpgrade{})); err != nil {
				return nil, err
			}

			// run module migrations
			vm, err := app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
			if err != nil {
				return nil, err
			}

			sdkCtx.Logger().Info("finished to upgrade", "upgrade-name", UpgradeName, "duration-sec", time.Since(start).Seconds())

			return vm, nil
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}

	if upgradeInfo.Name == UpgradeName && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{
				circuittypes.StoreKey,
				consensustypes.StoreKey,
				hyperlanetypes.ModuleName,
				warptypes.ModuleName,
				minfeetypes.StoreKey,
			},
			Deleted: []string{
				crisistypes.StoreKey,
				"blobstream",
			},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}
}
