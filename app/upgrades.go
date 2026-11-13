package app

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobkeeper "github.com/celestiaorg/celestia-app/v11/x/blob/keeper"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	teeismtypes "github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
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
			sdkCtx.Logger().Info("running upgrade handler", "upgrade-name", upgradeName)
			// Guarantee the blob params store key so that
			// BlobKeeper.GetParams needs no legacy subspace fallback.
			blobMigrator := blobkeeper.NewMigrator(app.BlobKeeper)
			if err := blobMigrator.EnsureParams(sdkCtx); err != nil {
				return nil, fmt.Errorf("failed to ensure blob params: %w", err)
			}
			versionMap, err := app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
			if err != nil {
				return nil, err
			}
			// After migrations, so no migration can overwrite the rescaled params.
			if err := app.rescaleForBlockInterval(sdkCtx); err != nil {
				return nil, fmt.Errorf("failed to rescale block-count parameters: %w", err)
			}
			return versionMap, nil
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}

	if upgradeInfo.Name == upgradeName && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) { //nolint:staticcheck
		app.SetStoreLoader(v11StoreLoader(upgradeInfo.Height))
	}
}

// v11StoreLoader adds the teeism store at the v11 upgrade height. A database
// that already has the store, such as one this binary wrote from genesis,
// loads as is.
func v11StoreLoader(upgradeHeight int64) baseapp.StoreLoader {
	return func(ms storetypes.CommitMultiStore) error {
		version := ms.LastCommitID().Version
		if upgradeHeight != version+1 || hasStore(ms, version, teeismtypes.StoreKey) {
			return baseapp.DefaultStoreLoader(ms)
		}
		return ms.LoadLatestVersionAndUpgrade(&storetypes.StoreUpgrades{
			Added: []string{teeismtypes.StoreKey},
		})
	}
}

// hasStore reports whether the commit at version includes the named store.
func hasStore(ms storetypes.CommitMultiStore, version int64, name string) bool {
	rs, ok := ms.(interface {
		GetCommitInfo(int64) (*storetypes.CommitInfo, error)
	})
	if !ok {
		return false
	}
	info, err := rs.GetCommitInfo(version)
	if err != nil {
		return false
	}
	for _, store := range info.StoreInfos {
		if store.Name == name {
			return true
		}
	}
	return false
}

// rescaleForBlockInterval sets the parameters whose meaning depends on the
// block time to their values for v11's 0.8s blocks.
func (app App) rescaleForBlockInterval(ctx sdk.Context) error {
	consensusParams, err := app.ConsensusKeeper.ParamsStore.Get(ctx)
	if err != nil {
		return err
	}
	consensusParams.Evidence.MaxAgeNumBlocks = appconsts.MaxAgeNumBlocks
	if err := app.ConsensusKeeper.ParamsStore.Set(ctx, consensusParams); err != nil {
		return err
	}

	slashingParams, err := app.SlashingKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	slashingParams.SignedBlocksWindow = appconsts.SignedBlocksWindow
	if err := app.SlashingKeeper.SetParams(ctx, slashingParams); err != nil {
		return err
	}

	stakingParams, err := app.StakingKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	stakingParams.HistoricalEntries = appconsts.HistoricalEntries
	if err := app.StakingKeeper.SetParams(ctx, stakingParams); err != nil {
		return err
	}

	fibreParams := app.FibreKeeper.GetParams(ctx)
	fibreParams.PaymentPromiseHeightWindow = fibretypes.DefaultPaymentPromiseHeightWindow
	app.FibreKeeper.SetParams(ctx, fibreParams)

	connectionParams := app.IBCKeeper.ConnectionKeeper.GetParams(ctx)
	connectionParams.MaxExpectedTimePerBlock = uint64(appconsts.MaxExpectedTimePerBlock.Nanoseconds())
	app.IBCKeeper.ConnectionKeeper.SetParams(ctx, connectionParams)

	ctx.Logger().Info("rescaled block-count parameters for 0.8s blocks",
		"max_age_num_blocks", appconsts.MaxAgeNumBlocks,
		"signed_blocks_window", appconsts.SignedBlocksWindow,
		"historical_entries", appconsts.HistoricalEntries,
		"payment_promise_height_window", fibretypes.DefaultPaymentPromiseHeightWindow,
		"max_expected_time_per_block", appconsts.MaxExpectedTimePerBlock)
	return nil
}
