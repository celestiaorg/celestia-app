package app

import (
	"context"
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
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

			err := app.SetMinCommissionRate(sdkCtx)
			if err != nil {
				sdkCtx.Logger().Error("failed to set min commission rate", "error", err)
				return nil, err
			}

			err = app.UpdateValidatorCommissionRates(sdkCtx)
			if err != nil {
				sdkCtx.Logger().Error("failed to update validator commission rates", "error", err)
				return nil, err
			}

			sdkCtx.Logger().Info("finished to upgrade", "upgrade-name", upgradeName, "duration-sec", time.Since(start).Seconds())

			return app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
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

func (a App) SetMinCommissionRate(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := a.StakingKeeper.GetParams(ctx)
	if err != nil {
		sdkCtx.Logger().Error("failed to get staking params", "error", err)
		return err
	}

	params.MinCommissionRate = appconsts.MinCommissionRate

	sdkCtx.Logger().Info(fmt.Sprintf("Setting the staking params min commission rate to %v.\n", appconsts.MinCommissionRate))
	err = a.StakingKeeper.SetParams(ctx, params)
	if err != nil {
		sdkCtx.Logger().Error("failed to set staking params", "error", err)
		return err
	}
	return nil
}

// UpdateValidatorCommissionRates iterates over all validators and increases
// their commission rate and max commission rate if they are below the new
// minimum commission rate.
func (a App) UpdateValidatorCommissionRates(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	validators, err := a.StakingKeeper.GetAllValidators(ctx)
	if err != nil {
		sdkCtx.Logger().Error("failed to get all validators", "error", err)
		return err
	}

	for _, validator := range validators {
		if validator.Commission.Rate.GTE(appconsts.MinCommissionRate) && validator.Commission.MaxRate.GTE(appconsts.MinCommissionRate) {
			sdkCtx.Logger().Debug("validator commission rate and max commission rate are already greater than or equal to the minimum commission rate", "validator", validator.GetOperator())
			continue
		}
		rate := getMax(validator.Commission.Rate, appconsts.MinCommissionRate)
		maxRate := getMax(validator.Commission.MaxRate, appconsts.MinCommissionRate)

		valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
		if err != nil {
			sdkCtx.Logger().Error("failed to get validator address", "error", err)
			continue
		}
		if err := a.StakingKeeper.Hooks().BeforeValidatorModified(ctx, valAddr); err != nil {
			sdkCtx.Logger().Error("failed to call before validator modified hook", "error", err)
			continue
		}

		validator.Commission.Rate = rate
		validator.Commission.MaxRate = maxRate
		validator.Commission.UpdateTime = sdkCtx.BlockTime()

		sdkCtx.Logger().Info("setting validator commission", "validator", validator.GetOperator(), "rate", validator.Commission.Rate, "max rate", validator.Commission.MaxRate)
		if err = a.StakingKeeper.SetValidator(ctx, validator); err != nil {
			sdkCtx.Logger().Error("failed to set validator", "error", err)
			continue
		}
	}
	return nil
}

func getMax(a, b sdkmath.LegacyDec) sdkmath.LegacyDec {
	if a.GTE(b) {
		return a
	}
	return b
}
