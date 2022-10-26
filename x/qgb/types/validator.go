package types

import (
	"bytes"
	math "math"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ToInternal transforms a BridgeValidator into its fully validated internal type.
func (b BridgeValidator) ToInternal() (*InternalBridgeValidator, error) {
	return NewInternalBridgeValidator(b)
}

// BridgeValidators is the sorted set of validator data for EVM bridge MultiSig set.
type BridgeValidators []BridgeValidator

func (b BridgeValidators) ToInternal() (*InternalBridgeValidators, error) {
	ret := make(InternalBridgeValidators, len(b))
	for i := range b {
		ibv, err := NewInternalBridgeValidator(b[i])
		if err != nil {
			return nil, sdkerrors.Wrapf(err, "member %d", i)
		}
		ret[i] = ibv
	}
	return &ret, nil
}

// Bridge Validator but with validated EVMAddress.
type InternalBridgeValidator struct {
	Power      uint64
	EVMAddress common.Address
}

func NewInternalBridgeValidator(bridgeValidator BridgeValidator) (*InternalBridgeValidator, error) {
	if !common.IsHexAddress(bridgeValidator.EvmAddress) {
		return nil, stakingtypes.ErrEVMAddressNotHex
	}
	validatorEVMAddr := common.HexToAddress(bridgeValidator.EvmAddress)
	i := &InternalBridgeValidator{
		Power:      bridgeValidator.Power,
		EVMAddress: validatorEVMAddr,
	}
	if err := i.ValidateBasic(); err != nil {
		return nil, sdkerrors.Wrap(err, "invalid bridge validator")
	}
	return i, nil
}

func (i InternalBridgeValidator) ValidateBasic() error {
	if i.Power == 0 {
		return sdkerrors.Wrap(ErrEmpty, "power")
	}
	return nil
}

func (i InternalBridgeValidator) ToExternal() BridgeValidator {
	return BridgeValidator{
		Power:      i.Power,
		EvmAddress: i.EVMAddress.Hex(),
	}
}

// InternalBridgeValidators is the sorted set of validator data for EVM bridge MultiSig set.
type InternalBridgeValidators []*InternalBridgeValidator

func (ibv InternalBridgeValidators) ToExternal() BridgeValidators {
	bridgeValidators := make([]BridgeValidator, len(ibv))
	for b := range bridgeValidators {
		bridgeValidators[b] = ibv[b].ToExternal()
	}

	return BridgeValidators(bridgeValidators)
}

// Sort sorts the validators by power.
func (ibv InternalBridgeValidators) Sort() {
	sort.Slice(ibv, func(i, j int) bool {
		if ibv[i].Power == ibv[j].Power {
			// Secondary sort on EVM address in case powers are equal
			return EVMAddrLessThan(ibv[i].EVMAddress, ibv[j].EVMAddress)
		}
		return ibv[i].Power > ibv[j].Power
	})
}

// EVMAddrLessThan migrates the EVM address less than function.
func EVMAddrLessThan(e common.Address, o common.Address) bool {
	return bytes.Compare([]byte(e.Hex())[:], []byte(o.Hex())[:]) == -1
}

// PowerDiff returns the difference in power between two bridge validator sets
// note this is Gravity bridge power *not* Cosmos voting power. Cosmos voting
// power is based on the absolute number of tokens in the staking pool at any given
// time Gravity bridge power is normalized using the equation.
//
// validators cosmos voting power / total cosmos voting power in this block = gravity bridge power / u32_max
//
// As an example if someone has 52% of the Cosmos voting power when a validator set is created their Gravity
// bridge voting power is u32_max * .52
//
// Normalized voting power dramatically reduces how often we have to produce new validator set updates. For example
// if the total on chain voting power increases by 1% due to inflation, we shouldn't have to generate a new validator
// set, after all the validators retained their relative percentages during inflation and normalized Gravity bridge power
// shows no difference.
func (ibv InternalBridgeValidators) PowerDiff(c InternalBridgeValidators) float64 {
	powers := map[string]int64{}
	// loop over ibv and initialize the map with their powers
	for _, bv := range ibv {
		powers[bv.EVMAddress.Hex()] = int64(bv.Power)
	}

	// subtract c powers from powers in the map, initializing
	// uninitialized keys with negative numbers
	for _, bv := range c {
		if val, ok := powers[bv.EVMAddress.Hex()]; ok {
			powers[bv.EVMAddress.Hex()] = val - int64(bv.Power)
		} else {
			powers[bv.EVMAddress.Hex()] = -int64(bv.Power)
		}
	}

	var delta float64
	for _, v := range powers {
		// NOTE: we care about the absolute value of the changes
		delta += math.Abs(float64(v))
	}

	return math.Abs(delta / float64(math.MaxUint32))
}

// TotalPower returns the total power in the bridge validator set.
func (ibv InternalBridgeValidators) TotalPower() (out uint64) {
	for _, v := range ibv {
		out += v.Power
	}
	return
}

// HasDuplicates returns true if there are duplicates in the set.
func (ibv InternalBridgeValidators) HasDuplicates() bool {
	m := make(map[string]struct{}, len(ibv))
	// creates a hashmap then ensures that the hashmap and the array
	// have the same length, this acts as an O(n) duplicates check
	for i := range ibv {
		m[ibv[i].EVMAddress.Hex()] = struct{}{}
	}
	return len(m) != len(ibv)
}

// GetPowers returns only the power values for all members.
func (ibv InternalBridgeValidators) GetPowers() []uint64 {
	r := make([]uint64, len(ibv))
	for i := range ibv {
		r[i] = ibv[i].Power
	}
	return r
}

// ValidateBasic performs stateless checks.
func (ibv InternalBridgeValidators) ValidateBasic() error {
	if len(ibv) == 0 {
		return ErrEmpty
	}
	for i := range ibv {
		if err := ibv[i].ValidateBasic(); err != nil {
			return sdkerrors.Wrapf(err, "member %d", i)
		}
	}
	if ibv.HasDuplicates() {
		return sdkerrors.Wrap(ErrDuplicate, "addresses")
	}
	return nil
}
