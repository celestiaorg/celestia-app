package signal

import (
	"bytes"
	"context"
	"encoding/binary"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v2/x/signal/types"
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Keeper implements the MsgServer and QueryServer interfaces
var (
	_ types.MsgServer   = &Keeper{}
	_ types.QueryServer = Keeper{}

	// defaultSignalThreshold is 5/6 or approximately 83.33%
	defaultSignalThreshold = sdk.NewDec(5).Quo(sdk.NewDec(6))

	// defaultUpgradeHeightDelay is the number of blocks after a quorum has been
	// reached that the chain should upgrade to the new version. Assuming a
	// block interval of 12 seconds, this is 48 hours.
	defaultUpgradeHeightDelay = int64(3 * 7 * 24 * 60 * 60 / 12) // 3 weeks * 7 days * 24 hours * 60 minutes * 60 seconds / 12 seconds per block = 151,200 blocks.
)

// Threshold is the fraction of voting power that is required
// to signal for a version change. It is set to 5/6 as the middle point
// between 2/3 and 3/3 providing 1/6 fault tolerance to halting the
// network during an upgrade period. It can be modified through a
// hard fork change that modified the app version
func Threshold(_ uint64) sdk.Dec {
	return defaultSignalThreshold
}

type Keeper struct {
	// binaryCodec is used to marshal and unmarshal data from the store.
	binaryCodec codec.BinaryCodec

	// storeKey is key that is used to fetch the signal store from the multi
	// store.
	storeKey storetypes.StoreKey

	// stakingKeeper is used to fetch validators to calculate the total power
	// signalled to a version.
	stakingKeeper StakingKeeper
}

// NewKeeper returns a signal keeper.
func NewKeeper(
	binaryCodec codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	stakingKeeper StakingKeeper,
) Keeper {
	return Keeper{
		binaryCodec:   binaryCodec,
		storeKey:      storeKey,
		stakingKeeper: stakingKeeper,
	}
}

// SignalVersion is a method required by the MsgServer interface.
func (k Keeper) SignalVersion(ctx context.Context, req *types.MsgSignalVersion) (*types.MsgSignalVersionResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if k.isUpgradePending(sdkCtx) {
		return &types.MsgSignalVersionResponse{}, types.ErrUpgradePending.Wrapf("can not signal version")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddress)
	if err != nil {
		return nil, err
	}

	// The signalled version must be either the current version or the next
	// version.
	currentVersion := sdkCtx.BlockHeader().Version.App
	if req.Version != currentVersion && req.Version != currentVersion+1 {
		return nil, types.ErrInvalidVersion
	}

	_, found := k.stakingKeeper.GetValidator(sdkCtx, valAddr)
	if !found {
		return nil, stakingtypes.ErrNoValidatorFound
	}

	k.SetValidatorVersion(sdkCtx, valAddr, req.Version)
	return &types.MsgSignalVersionResponse{}, nil
}

// TryUpgrade is a method required by the MsgServer interface. It tallies the
// voting power that has voted on each version. If one version has reached a
// quorum, an upgrade is persisted to the store. The upgrade is used by the
// application later when it is time to upgrade to that version.
func (k *Keeper) TryUpgrade(ctx context.Context, _ *types.MsgTryUpgrade) (*types.MsgTryUpgradeResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if k.isUpgradePending(sdkCtx) {
		return &types.MsgTryUpgradeResponse{}, types.ErrUpgradePending.Wrapf("can not try upgrade")
	}

	threshold := k.GetVotingPowerThreshold(sdkCtx)
	hasQuorum, version := k.TallyVotingPower(sdkCtx, threshold.Int64())
	if hasQuorum {
		if version <= sdkCtx.BlockHeader().Version.App {
			return &types.MsgTryUpgradeResponse{}, types.ErrInvalidUpgradeVersion.Wrapf("can not upgrade to version %v because it is less than or equal to current version %v", version, sdkCtx.BlockHeader().Version.App)
		}
		upgrade := types.Upgrade{
			AppVersion:    version,
			UpgradeHeight: sdkCtx.BlockHeader().Height + defaultUpgradeHeightDelay,
		}
		k.setUpgrade(sdkCtx, upgrade)
	}
	return &types.MsgTryUpgradeResponse{}, nil
}

// VersionTally enables a client to query for the tally of voting power has
// signalled for a particular version.
func (k Keeper) VersionTally(ctx context.Context, req *types.QueryVersionTallyRequest) (*types.QueryVersionTallyResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	totalVotingPower := k.stakingKeeper.GetLastTotalPower(sdkCtx)
	currentVotingPower := sdk.NewInt(0)
	store := sdkCtx.KVStore(k.storeKey)
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		if bytes.Equal(iterator.Key(), types.UpgradeKey) {
			continue
		}
		valAddress := sdk.ValAddress(iterator.Key())
		power := k.stakingKeeper.GetLastValidatorPower(sdkCtx, valAddress)
		version := VersionFromBytes(iterator.Value())
		if version == req.Version {
			currentVotingPower = currentVotingPower.AddRaw(power)
		}
	}
	threshold := k.GetVotingPowerThreshold(sdkCtx)
	return &types.QueryVersionTallyResponse{
		VotingPower:      currentVotingPower.Uint64(),
		ThresholdPower:   threshold.Uint64(),
		TotalVotingPower: totalVotingPower.Uint64(),
	}, nil
}

// SetValidatorVersion saves a signalled version for a validator.
func (k Keeper) SetValidatorVersion(ctx sdk.Context, valAddress sdk.ValAddress, version uint64) {
	store := ctx.KVStore(k.storeKey)
	store.Set(valAddress, VersionToBytes(version))
}

// DeleteValidatorVersion deletes a signalled version for a validator.
func (k Keeper) DeleteValidatorVersion(ctx sdk.Context, valAddress sdk.ValAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(valAddress)
}

// TallyVotingPower tallies the voting power for each version and returns true
// and the version if any version has reached the quorum in voting power.
// Returns false and 0 otherwise.
func (k Keeper) TallyVotingPower(ctx sdk.Context, threshold int64) (bool, uint64) {
	versionToPower := make(map[uint64]int64)
	store := ctx.KVStore(k.storeKey)
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		if bytes.Equal(iterator.Key(), types.UpgradeKey) {
			continue
		}
		valAddress := sdk.ValAddress(iterator.Key())
		// check that the validator is still part of the bonded set
		val, found := k.stakingKeeper.GetValidator(ctx, valAddress)
		if !found {
			// if it no longer exists, delete the version
			k.DeleteValidatorVersion(ctx, valAddress)
		}
		// if the validator is not bonded, skip it's voting power
		if !found || !val.IsBonded() {
			continue
		}
		power := k.stakingKeeper.GetLastValidatorPower(ctx, valAddress)
		version := VersionFromBytes(iterator.Value())
		if _, ok := versionToPower[version]; !ok {
			versionToPower[version] = power
		} else {
			versionToPower[version] += power
		}
		if versionToPower[version] >= threshold {
			return true, version
		}
	}
	return false, 0
}

// GetVotingPowerThreshold returns the voting power threshold required to
// upgrade to a new version.
func (k Keeper) GetVotingPowerThreshold(ctx sdk.Context) sdkmath.Int {
	totalVotingPower := k.stakingKeeper.GetLastTotalPower(ctx)
	thresholdFraction := Threshold(ctx.BlockHeader().Version.App)
	return thresholdFraction.MulInt(totalVotingPower).Ceil().TruncateInt()
}

// ShouldUpgrade returns whether the signalling mechanism has concluded that the
// network is ready to upgrade and the version to upgrade to. It returns false
// and 0 if no version has reached quorum.
func (k *Keeper) ShouldUpgrade(ctx sdk.Context) (isQuorumVersion bool, version uint64) {
	upgrade, ok := k.getUpgrade(ctx)
	if !ok {
		return false, 0
	}

	hasUpgradeHeightBeenReached := ctx.BlockHeight() >= upgrade.UpgradeHeight
	if hasUpgradeHeightBeenReached {
		return true, upgrade.AppVersion
	}
	return false, 0
}

// ResetTally resets the tally after a version change. It iterates over the
// store and deletes all versions. It also resets the quorumVersion and
// upgradeHeight to 0.
func (k *Keeper) ResetTally(ctx sdk.Context) {
	store := ctx.KVStore(k.storeKey)
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	// delete all signals
	for ; iterator.Valid(); iterator.Next() {
		if bytes.Equal(iterator.Key(), types.UpgradeKey) {
			// skip over the upgrade key
			continue
		}
		store.Delete(iterator.Key())
	}
	// delete the upgrade value
	store.Delete(types.UpgradeKey)
}

func VersionToBytes(version uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, version)
}

func VersionFromBytes(version []byte) uint64 {
	return binary.BigEndian.Uint64(version)
}

// isUpgradePending returns true if an app version has reached quorum and the
// chain should upgrade to the app version at the upgrade height. While the
// keeper has an upgrade pending the SignalVersion and TryUpgrade messages will
// be rejected.
func (k *Keeper) isUpgradePending(ctx sdk.Context) bool {
	_, ok := k.getUpgrade(ctx)
	return ok
}

// getUpgrade returns the current upgrade information from the store.
// If an upgrade is found, it returns the upgrade object and true.
// If no upgrade is found, it returns an empty upgrade object and false.
func (k *Keeper) getUpgrade(ctx sdk.Context) (upgrade types.Upgrade, ok bool) {
	store := ctx.KVStore(k.storeKey)
	value := store.Get(types.UpgradeKey)
	if value == nil {
		return types.Upgrade{}, false
	}
	k.binaryCodec.MustUnmarshal(value, &upgrade)
	return upgrade, true
}

// setUpgrade sets the upgrade in the store.
func (k *Keeper) setUpgrade(ctx sdk.Context, upgrade types.Upgrade) {
	store := ctx.KVStore(k.storeKey)
	value := k.binaryCodec.MustMarshal(&upgrade)
	store.Set(types.UpgradeKey, value)
}
