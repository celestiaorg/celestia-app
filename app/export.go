package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctypes "github.com/cosmos/ibc-go/v8/modules/core/types"
)

// ExportAppStateAndValidators exports the state of the application for a genesis
// file.
func (app *App) ExportAppStateAndValidators(
	forZeroHeight bool, jailAllowedAddrs []string, _ []string,
) (servertypes.ExportedApp, error) {
	// The context must carry the last committed height so that slashing events are accounted for.
	ctx := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight()})

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
	if err := dropLocalhostClient(app.AppCodec(), genState); err != nil {
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
		// Commission left unwithdrawn stays in the validator's outstanding rewards,
		// which the scraps donation below hands to the community pool instead of the
		// operator. Having nothing to withdraw is the only acceptable failure.
		_, err = app.DistrKeeper.WithdrawValidatorCommission(ctx, valBz)
		if err != nil && !errors.Is(err, distributiontypes.ErrNoValidatorCommission) {
			panic(fmt.Errorf("withdrawing commission for validator %s: %w", val.GetOperator(), err))
		}
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
		// As with commission above: rewards that fail to withdraw here are donated to
		// the community pool a few lines down, so a delegator would silently lose them.
		// Fail the export instead of exporting a genesis that misallocates funds.
		if _, err := app.DistrKeeper.WithdrawDelegationRewards(ctx, delAddr, valAddr); err != nil {
			panic(fmt.Errorf("withdrawing rewards for delegator %s from validator %s: %w",
				delegation.DelegatorAddress, delegation.ValidatorAddress, err))
		}
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
		jail := applyAllowedAddrs && !allowedAddrsMap[addr.String()]
		if jail {
			validator.Jailed = true
		}

		err = app.StakingKeeper.SetValidator(ctx, validator)
		if err != nil {
			panic(errors.New("couldn't set validator"))
		}

		// A jailed validator must not be left in the power index:
		// ApplyAndReturnValidatorSetUpdates below panics on any jailed validator it
		// finds there. This mirrors what staking's own jailValidator does.
		if jail {
			if err := app.StakingKeeper.DeleteValidatorByPowerIndex(ctx, validator); err != nil {
				panic(err)
			}
		}
		counter++
	}

	iter.Close()

	// Run the validator-set update at height zero so that any validators unbonded
	// by this call receive an unbonding height of zero instead of the exported
	// chain's height. On import, staking rebuilds the unbonding queue from this
	// height. Retaining the old height would keep those validators unbonding until
	// the restarted chain reached the exported chain's height.
	_, err = app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx.WithBlockHeight(0))
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

// dropLocalhostClient removes the 09-localhost IBC client from an exported
// genesis. The client is per-chain runtime state that ibc's own InitGenesis
// recreates at the height the importing chain starts from, and celestia does not
// list 09-localhost in allowed_clients (see DefaultGenesis in default_overrides.go),
// so leaving it in produces a genesis file that panics on import.
func dropLocalhostClient(cdc codec.Codec, genState map[string]json.RawMessage) error {
	raw, ok := genState[ibcexported.ModuleName]
	if !ok {
		return nil
	}

	var ibcGenesis ibctypes.GenesisState
	if err := cdc.UnmarshalJSON(raw, &ibcGenesis); err != nil {
		return err
	}

	clients := make([]ibcclienttypes.IdentifiedClientState, 0, len(ibcGenesis.ClientGenesis.Clients))
	for _, client := range ibcGenesis.ClientGenesis.Clients {
		if client.ClientId != ibcexported.LocalhostClientID {
			clients = append(clients, client)
		}
	}
	metadata := make([]ibcclienttypes.IdentifiedGenesisMetadata, 0, len(ibcGenesis.ClientGenesis.ClientsMetadata))
	for _, entry := range ibcGenesis.ClientGenesis.ClientsMetadata {
		if entry.ClientId != ibcexported.LocalhostClientID {
			metadata = append(metadata, entry)
		}
	}
	consensus := make(ibcclienttypes.ClientsConsensusStates, 0, len(ibcGenesis.ClientGenesis.ClientsConsensus))
	for _, entry := range ibcGenesis.ClientGenesis.ClientsConsensus {
		if entry.ClientId != ibcexported.LocalhostClientID {
			consensus = append(consensus, entry)
		}
	}
	ibcGenesis.ClientGenesis.Clients = clients
	ibcGenesis.ClientGenesis.ClientsMetadata = metadata
	ibcGenesis.ClientGenesis.ClientsConsensus = consensus

	updated, err := cdc.MarshalJSON(&ibcGenesis)
	if err != nil {
		return err
	}
	genState[ibcexported.ModuleName] = updated

	return nil
}
