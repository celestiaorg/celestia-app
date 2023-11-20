package upgrade

import (
	"context"
	"encoding/binary"
	"math"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/x/upgrade/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Keeper implements the MsgServer and QueryServer interfaces
var (
	_ types.MsgServer   = Keeper{}
	_ types.QueryServer = Keeper{}
)

type Keeper struct {
	// we use the same upgrade store key so existing IBC client state can
	// safely be ported over without any migration
	storeKey storetypes.StoreKey

	// in memory copy of the upgrade height if any. This is local per node
	// and configured from the config. Used just for V2
	upgradeHeight int64

	// quorumVersion is the version that has received a quorum of validators
	// to signal for it. This variable is relevant just for the scope of the
	// lifetime of the block
	quorumVersion uint64

	// staking keeper is used to fetch validators to calculate the total power
	// signalled to a version
	stakingKeeper StakingKeeper

	// paramStore provides access to the signal quorum param
	paramStore paramtypes.Subspace
}

// NewKeeper constructs an upgrade keeper
func NewKeeper(
	storeKey storetypes.StoreKey,
	upgradeHeight int64,
	stakingKeeper StakingKeeper,
	paramStore paramtypes.Subspace,
) Keeper {
	return Keeper{
		storeKey:      storeKey,
		upgradeHeight: upgradeHeight,
		stakingKeeper: stakingKeeper,
		paramStore:    paramStore,
	}
}

// SignalVersion is a method required by the MsgServer interface
func (k Keeper) SignalVersion(ctx context.Context, req *types.MsgSignalVersion) (*types.MsgSignalVersionResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddress)
	if err != nil {
		return nil, err
	}

	// check that the validator exists
	power := k.stakingKeeper.GetLastValidatorPower(sdkCtx, valAddr)
	if power <= 0 {
		return nil, stakingtypes.ErrNoValidatorFound
	}

	// the signalled version must be either the current version (for cancelling an upgrade)
	// or the very next version (for accepting an upgrade)
	currentVersion := sdkCtx.BlockHeader().Version.App
	if req.Version != currentVersion && req.Version != currentVersion+1 {
		return nil, types.ErrInvalidVersion
	}

	k.SetValidatorVersion(sdkCtx, valAddr, req.Version)
	return &types.MsgSignalVersionResponse{}, nil
}

// VersionTally is a method required by the QueryServer interface
func (k Keeper) VersionTally(ctx context.Context, req *types.QueryVersionTallyRequest) (*types.QueryVersionTallyResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	totalVotingPower := k.stakingKeeper.GetLastTotalPower(sdkCtx)
	currentVotingPower := sdk.NewInt(0)
	store := sdkCtx.KVStore(k.storeKey)
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		valAddress := sdk.ValAddress(iterator.Key()[1:])
		power := k.stakingKeeper.GetLastValidatorPower(sdkCtx, valAddress)
		version := VersionFromBytes(iterator.Value())
		if version == req.Version {
			currentVotingPower = currentVotingPower.AddRaw(power)
		}
	}

	return &types.QueryVersionTallyResponse{
		TotalVotingPower: totalVotingPower.Uint64(),
		VotingPower:      currentVotingPower.Uint64(),
	}, nil
}

// Params is a method required by the QueryServer interface
func (k Keeper) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(sdkCtx)
	return &types.QueryParamsResponse{Params: &params}, nil
}

// SetValidatorVersion saves a signalled version for a validator using the keeper's store key
func (k Keeper) SetValidatorVersion(ctx sdk.Context, valAddress sdk.ValAddress, version uint64) {
	store := ctx.KVStore(k.storeKey)
	store.Set(valAddress, VersionToBytes(version))
}

// DeleteValidatorVersion deletes a signalled version for a validator using the keeper's store key
func (k Keeper) DeleteValidatorVersion(ctx sdk.Context, valAddress sdk.ValAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(valAddress)
}

// EndBlock is called at the end of every block. It tallies the voting power that has
// voted on each version. If one version has quorum, it is set as the quorum version
// which the application can use as signal to upgrade to that version.
func (k Keeper) EndBlock(ctx sdk.Context) {
	threshold := k.GetVotingPowerThreshold(ctx)
	hasQuorum, version := k.TallyVotingPower(ctx, threshold)
	if hasQuorum {
		k.quorumVersion = version
	}
}

// TallyVotingPower tallies the voting power for each version and returns true if
// any version has reached the quorum in voting power
func (k Keeper) TallyVotingPower(ctx sdk.Context, quorum int64) (bool, uint64) {
	output := make(map[uint64]int64)
	store := ctx.KVStore(k.storeKey)
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		valAddress := sdk.ValAddress(iterator.Key()[1:])
		// check that the validator is still part of the bonded set
		if val, found := k.stakingKeeper.GetValidator(ctx, valAddress); !found || !val.IsBonded() {
			k.DeleteValidatorVersion(ctx, valAddress)
			continue
		}
		power := k.stakingKeeper.GetLastValidatorPower(ctx, valAddress)
		version := VersionFromBytes(iterator.Value())
		if _, ok := output[version]; !ok {
			output[version] = power
		} else {
			output[version] += power
		}
		if output[version] > quorum {
			return true, version
		}
	}
	return false, 0
}

// GetVotingPowerThreshold returns the voting power threshold required to
// upgrade to a new version. It converts the signal quorum parameter which
// is a number between 0 and math.MaxUint32 representing a fraction and
// then multiplies it by the total voting power
func (k Keeper) GetVotingPowerThreshold(ctx sdk.Context) int64 {
	quorum := sdkmath.NewInt(int64(k.GetParams(ctx).SignalQuorum))
	totalVotingPower := k.stakingKeeper.GetLastTotalPower(ctx)
	return totalVotingPower.Mul(quorum).QuoRaw(math.MaxUint32).Int64()
}

// ShouldUpgradeToV2 returns true if the current height is one before
// the locally provided upgrade height that is passed as a flag
// NOTE: This is only used to upgrade to v2 and should be deprecated
// in v3
func (k Keeper) ShouldUpgradeToV2(height int64) bool {
	return k.upgradeHeight == height+1
}

// ShouldUpgrade returns true if the signalling mechanism has concluded
// that the network is ready to upgrade. It also returns the version
// that the node should upgrade to.
func (k *Keeper) ShouldUpgrade() (bool, uint64) {
	return k.quorumVersion != 0, k.quorumVersion
}

func VersionToBytes(version uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, version)
}

func VersionFromBytes(version []byte) uint64 {
	return binary.BigEndian.Uint64(version)
}

// ShouldUpgrade returns true if the current height is one before
// the locally provided upgrade height that is passed as a flag
func (k Keeper) ShouldUpgrade(height int64) bool {
	return k.upgradeHeight == height+1
}

// ShouldUpgrade returns true if the current height is one before
// the locally provided upgrade height that is passed as a flag
func (k Keeper) ShouldUpgrade(height int64) bool {
	return k.upgradeHeight == height+1
}
