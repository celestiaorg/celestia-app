package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"cosmossdk.io/core/header"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ExportAppStateAndValidators exports the state of the application for a genesis
// file.
func (app *App) ExportAppStateAndValidators(
	forZeroHeight bool, jailAllowedAddrs []string, modulesToExport []string,
) (servertypes.ExportedApp, error) {
	// The context must carry the last committed height so that slashing events are accounted for.
	ctx := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight()})

	if err := app.checkExportable(ctx, modulesToExport); err != nil {
		return servertypes.ExportedApp{}, err
	}
	blockTime, err := app.lastCommitTime()
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	ctx = ctx.WithBlockTime(blockTime).WithHeaderInfo(header.Info{Time: blockTime})

	// We export at last height + 1, because that's the height at which
	// Tendermint will start InitChain.
	height := app.LastBlockHeight() + 1
	if forZeroHeight {
		height = 0
		app.prepForZeroHeightGenesis(ctx, jailAllowedAddrs)
	}

	genState, err := app.ModuleManager.ExportGenesis(ctx, app.AppCodec())
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	appState, err := json.MarshalIndent(genState, "", "  ")
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	return servertypes.ExportedApp{
		AppState:        appState,
		Validators:      validators,
		Height:          height,
		ConsensusParams: app.GetConsensusParams(ctx),
	}, err
}

// checkExportable refuses state that this binary cannot export into a usable
// genesis.
func (app *App) checkExportable(ctx sdk.Context, modulesToExport []string) error {
	plan, err := app.UpgradeKeeper.GetUpgradePlan(ctx)
	switch {
	case errors.Is(err, upgradetypes.ErrNoUpgradePlanFound):
	case err != nil:
		return err
	case plan.Height <= app.LastBlockHeight()+1:
		return fmt.Errorf("upgrade %q is scheduled for height %d: export with --height %d instead",
			plan.Name, plan.Height, app.LastBlockHeight()-1)
	}

	appVersion, err := app.AppVersion(ctx)
	if err != nil {
		return err
	}
	if appVersion != appconsts.Version {
		return fmt.Errorf("state is at app version %d but this binary exports app version %d: "+
			"use a binary for app version %d (`celestia-appd passthrough v%d export` in the multiplexer build)",
			appVersion, appconsts.Version, appVersion, appVersion)
	}

	if len(modulesToExport) > 0 {
		return errors.New("exporting a subset of modules is not supported: unset --modules-to-export")
	}
	return nil
}

// lastCommitTime returns the timestamp of the last commit. It is missing when
// this node did not write it, e.g. after a snapshot restore.
func (app *App) lastCommitTime() (time.Time, error) {
	height := app.LastBlockHeight()
	rms, ok := app.CommitMultiStore().(*rootmulti.Store)
	if !ok {
		return time.Time{}, errors.New("export requires a rootmulti store")
	}
	info, err := rms.GetCommitInfo(height)
	if err != nil {
		return time.Time{}, fmt.Errorf("no commit info for height %d (%w): export from a node that executed this height", height, err)
	}
	if ts := info.Timestamp; ts.IsZero() || ts.Unix() <= 0 {
		return time.Time{}, fmt.Errorf("no commit timestamp for height %d: export from a node that executed this height", height)
	}
	return info.Timestamp, nil
}

// prepare for fresh start at zero height
// NOTE zero height genesis is a temporary feature which will be deprecated
// in favour of export at a block height
func (app *App) prepForZeroHeightGenesis(ctx sdk.Context, jailAllowedAddrs []string) {
	applyAllowedAddrs := len(jailAllowedAddrs) > 0

	// check if there is an allowed address list

	allowedAddrsMap := make(map[string]bool)

	for _, addr := range jailAllowedAddrs {
		_, err := sdk.ValAddressFromBech32(addr)
		if err != nil {
			log.Fatal(err)
		}
		allowedAddrsMap[addr] = true
	}
	/* Handle fee distribution state. */

	// withdraw all validator commission
	err := app.StakingKeeper.IterateValidators(ctx, func(_ int64, val stakingtypes.ValidatorI) (stop bool) {
		valBz, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(val.GetOperator())
		if err != nil {
			panic(err)
		}
		_, _ = app.DistrKeeper.WithdrawValidatorCommission(ctx, valBz)
		return false
	})
	if err != nil {
		panic(err)
	}

	// withdraw all delegator rewards
	dels, err := app.StakingKeeper.GetAllDelegations(ctx)
	if err != nil {
		panic(err)
	}

	for _, delegation := range dels {
		valAddr, err := sdk.ValAddressFromBech32(delegation.ValidatorAddress)
		if err != nil {
			panic(err)
		}

		delAddr, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
		if err != nil {
			panic(err)
		}
		_, _ = app.DistrKeeper.WithdrawDelegationRewards(ctx, delAddr, valAddr)
	}

	// clear validator slash events
	app.DistrKeeper.DeleteAllValidatorSlashEvents(ctx)

	// clear validator historical rewards
	app.DistrKeeper.DeleteAllValidatorHistoricalRewards(ctx)

	// set context height to zero
	height := ctx.BlockHeight()
	ctx = ctx.WithBlockHeight(0)

	// reinitialize all validators
	err = app.StakingKeeper.IterateValidators(ctx, func(_ int64, val stakingtypes.ValidatorI) (stop bool) {
		valBz, err := sdk.ValAddressFromBech32(val.GetOperator())
		if err != nil {
			panic(err)
		}
		// donate any unwithdrawn outstanding reward fraction tokens to the community pool
		scraps, err := app.DistrKeeper.GetValidatorOutstandingRewardsCoins(ctx, valBz)
		if err != nil {
			panic(err)
		}
		feePool, err := app.DistrKeeper.FeePool.Get(ctx)
		if err != nil {
			panic(err)
		}
		feePool.CommunityPool = feePool.CommunityPool.Add(scraps...)
		if err := app.DistrKeeper.FeePool.Set(ctx, feePool); err != nil {
			panic(err)
		}

		if err := app.DistrKeeper.Hooks().AfterValidatorCreated(ctx, valBz); err != nil {
			panic(err)
		}
		return false
	})
	if err != nil {
		panic(err)
	}

	// reinitialize all delegations
	for _, del := range dels {
		valAddr, err := sdk.ValAddressFromBech32(del.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		delAddr, err := sdk.AccAddressFromBech32(del.DelegatorAddress)
		if err != nil {
			panic(err)
		}
		err = app.DistrKeeper.Hooks().BeforeDelegationCreated(ctx, delAddr, valAddr)
		if err != nil {
			panic(err)
		}
		err = app.DistrKeeper.Hooks().AfterDelegationModified(ctx, delAddr, valAddr)
		if err != nil {
			panic(err)
		}
	}

	// reset context height
	ctx = ctx.WithBlockHeight(height)

	/* Handle staking state. */

	// iterate through redelegations, reset creation height
	err = app.StakingKeeper.IterateRedelegations(ctx, func(_ int64, red stakingtypes.Redelegation) (stop bool) {
		for i := range red.Entries {
			red.Entries[i].CreationHeight = 0
		}
		err = app.StakingKeeper.SetRedelegation(ctx, red)
		if err != nil {
			panic(err)
		}
		return false
	})
	if err != nil {
		panic(err)
	}

	// iterate through unbonding delegations, reset creation height
	err = app.StakingKeeper.IterateUnbondingDelegations(ctx, func(_ int64, ubd stakingtypes.UnbondingDelegation) (stop bool) {
		for i := range ubd.Entries {
			ubd.Entries[i].CreationHeight = 0
		}
		err = app.StakingKeeper.SetUnbondingDelegation(ctx, ubd)
		if err != nil {
			panic(err)
		}
		return false
	})
	if err != nil {
		panic(err)
	}

	// Iterate through validators by power descending, reset bond heights, and
	// update bond intra-tx counters.
	store := ctx.KVStore(app.keys[stakingtypes.StoreKey])
	iter := storetypes.KVStoreReversePrefixIterator(store, stakingtypes.ValidatorsKey)
	counter := int16(0)

	for ; iter.Valid(); iter.Next() {
		addr := sdk.ValAddress(stakingtypes.AddressFromValidatorsKey(iter.Key()))
		validator, err := app.StakingKeeper.GetValidator(ctx, addr)
		if err != nil {
			panic(errors.New("expected validator, not found"))
		}

		validator.UnbondingHeight = 0
		if applyAllowedAddrs && !allowedAddrsMap[addr.String()] {
			validator.Jailed = true
		}

		err = app.StakingKeeper.SetValidator(ctx, validator)
		if err != nil {
			panic(errors.New("couldn't set validator"))
		}
		counter++
	}

	iter.Close()

	_, err = app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
	if err != nil {
		log.Fatal(err)
	}

	/* Handle slashing state. */

	// reset start height on signing infos
	err = app.SlashingKeeper.IterateValidatorSigningInfos(
		ctx,
		func(addr sdk.ConsAddress, info slashingtypes.ValidatorSigningInfo) (stop bool) {
			info.StartHeight = 0
			err = app.SlashingKeeper.SetValidatorSigningInfo(ctx, addr, info)
			if err != nil {
				panic(err)
			}
			return false
		},
	)
	if err != nil {
		log.Fatal(err)
	}
}
