package types

import (
	"fmt"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	math "math"
	"math/big"
	"sort"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ToInternal transforms a BridgeValidator into its fully validated internal type
func (b BridgeValidator) ToInternal() (*InternalBridgeValidator, error) {
	return NewInternalBridgeValidator(b)
}

// BridgeValidators is the sorted set of validator data for Ethereum bridge MultiSig set
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

// Bridge Validator but with validated EthereumAddress
type InternalBridgeValidator struct {
	Power           uint64
	EthereumAddress stakingtypes.EthAddress
}

func NewInternalBridgeValidator(bridgeValidator BridgeValidator) (*InternalBridgeValidator, error) {
	validatorEthAddr, err := stakingtypes.NewEthAddress(bridgeValidator.EthereumAddress)
	if err != nil {
		return nil, err
	}
	i := &InternalBridgeValidator{
		Power:           bridgeValidator.Power,
		EthereumAddress: *validatorEthAddr,
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
	if err := i.EthereumAddress.ValidateBasic(); err != nil {
		return sdkerrors.Wrap(err, "ethereum address")
	}
	return nil
}

func (i InternalBridgeValidator) ToExternal() BridgeValidator {
	return BridgeValidator{
		Power:           i.Power,
		EthereumAddress: i.EthereumAddress.GetAddress(),
	}
}

// InternalBridgeValidators is the sorted set of validator data for Ethereum bridge MultiSig set
type InternalBridgeValidators []*InternalBridgeValidator

func (i InternalBridgeValidators) ToExternal() BridgeValidators {
	bridgeValidators := make([]BridgeValidator, len(i))
	for b := range bridgeValidators {
		bridgeValidators[b] = i[b].ToExternal()
	}

	return BridgeValidators(bridgeValidators)
}

// Sort sorts the validators by power
func (b InternalBridgeValidators) Sort() {
	sort.Slice(b, func(i, j int) bool {
		if b[i].Power == b[j].Power {
			// Secondary sort on eth address in case powers are equal
			return stakingtypes.EthAddrLessThan(b[i].EthereumAddress, b[j].EthereumAddress)
		}
		return b[i].Power > b[j].Power
	})
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
func (b InternalBridgeValidators) PowerDiff(c InternalBridgeValidators) float64 {
	powers := map[string]int64{}
	// loop over b and initialize the map with their powers
	for _, bv := range b {
		powers[bv.EthereumAddress.GetAddress()] = int64(bv.Power)
	}

	// subtract c powers from powers in the map, initializing
	// uninitialized keys with negative numbers
	for _, bv := range c {
		if val, ok := powers[bv.EthereumAddress.GetAddress()]; ok {
			powers[bv.EthereumAddress.GetAddress()] = val - int64(bv.Power)
		} else {
			powers[bv.EthereumAddress.GetAddress()] = -int64(bv.Power)
		}
	}

	var delta float64
	for _, v := range powers {
		// NOTE: we care about the absolute value of the changes
		delta += math.Abs(float64(v))
	}

	return math.Abs(delta / float64(math.MaxUint32))
}

// TotalPower returns the total power in the bridge validator set
func (b InternalBridgeValidators) TotalPower() (out uint64) {
	for _, v := range b {
		out += v.Power
	}
	return
}

// HasDuplicates returns true if there are duplicates in the set
func (b InternalBridgeValidators) HasDuplicates() bool {
	m := make(map[string]struct{}, len(b))
	// creates a hashmap then ensures that the hashmap and the array
	// have the same length, this acts as an O(n) duplicates check
	for i := range b {
		m[b[i].EthereumAddress.GetAddress()] = struct{}{}
	}
	return len(m) != len(b)
}

// GetPowers returns only the power values for all members
func (b InternalBridgeValidators) GetPowers() []uint64 {
	r := make([]uint64, len(b))
	for i := range b {
		r[i] = b[i].Power
	}
	return r
}

// ValidateBasic performs stateless checks
func (b InternalBridgeValidators) ValidateBasic() error {
	if len(b) == 0 {
		return ErrEmpty
	}
	for i := range b {
		if err := b[i].ValidateBasic(); err != nil {
			return sdkerrors.Wrapf(err, "member %d", i)
		}
	}
	if b.HasDuplicates() {
		return sdkerrors.Wrap(ErrDuplicate, "addresses")
	}
	return nil
}

//////////////////////////////////////
//             VALSETS              //
//////////////////////////////////////

// NewValset returns a new valset
func NewValset(nonce, height uint64, members InternalBridgeValidators) (*Valset, error) {
	if err := members.ValidateBasic(); err != nil {
		return nil, sdkerrors.Wrap(err, "invalid members")
	}
	members.Sort()
	mem := make([]BridgeValidator, 0)
	for _, val := range members {
		mem = append(mem, val.ToExternal())
	}
	vs := Valset{Nonce: nonce, Members: mem, Height: height}
	return &vs, nil
}

// CopyValset returns a new valset from an existing one
func CopyValset(v Valset) (*Valset, error) {
	vs := Valset{Nonce: v.Nonce, Members: v.Members, Height: v.Height}
	return &vs, nil
}

// WithoutEmptyMembers returns a new Valset without member that have 0 power or an empty Ethereum address.
func (v *Valset) WithoutEmptyMembers() *Valset {
	if v == nil {
		return nil
	}
	r := Valset{
		Nonce:   v.Nonce,
		Members: make([]BridgeValidator, 0, len(v.Members)),
		Height:  0,
	}
	for i := range v.Members {
		if _, err := v.Members[i].ToInternal(); err == nil {
			r.Members = append(r.Members, v.Members[i])
		}
	}
	return &r
}

// SignBytes produces the bytes that celestia validators are required to sign
// over when the validator set changes.
func (v *Valset) SignBytes(bridgeID ethcmn.Hash) (ethcmn.Hash, error) {
	vsHash, err := v.Hash()
	if err != nil {
		return ethcmn.Hash{}, err
	}

	// the word 'checkpoint' needs to be the same as the 'name' above in the checkpointAbiJson
	// but other than that it's a constant that has no impact on the output. This is because
	// it gets encoded as a function name which we must then discard.
	bytes, err := InternalQGBabi.Pack(
		"domainSeparateValidatorSetHash",
		bridgeID,
		VsDomainSeparator,
		big.NewInt(int64(v.Nonce)),
		big.NewInt(int64(v.TwoThirdsThreshold())),
		vsHash,
	)
	// this should never happen outside of test since any case that could crash on encoding
	// should be filtered above.
	if err != nil {
		panic(fmt.Sprintf("Error packing checkpoint! %s/n", err))
	}

	hash := crypto.Keccak256Hash(bytes[4:])
	return hash, nil
}

// Hash mimics the 'computeValsetHash' function used the qgb contracts by using
// a Valset to compute the hash of the abi encoded validator set.
func (v *Valset) Hash() (ethcmn.Hash, error) {
	ethVals := make([]wrapper.Validator, len(v.Members))
	for i, val := range v.Members {
		ethVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EthereumAddress),
			Power: big.NewInt(int64(val.Power)),
		}
	}

	encodedVals, err := InternalQGBabi.Pack("computeValidatorSetHash", ethVals)
	if err != nil {
		return ethcmn.Hash{}, err
	}

	return crypto.Keccak256Hash(encodedVals[4:]), nil
}

func (v *Valset) TwoThirdsThreshold() uint64 {
	totalPower := uint64(0)
	for _, member := range v.Members {
		totalPower += member.Power
	}

	// todo: fix to be more precise
	oneThird := (totalPower / 3) + 1 // +1 to round up
	return 2 * oneThird
}

// Valsets is a collection of valset
type Valsets []Valset

func (v Valsets) Len() int {
	return len(v)
}

func (v Valsets) Less(i, j int) bool {
	return v[i].Nonce > v[j].Nonce
}

func (v Valsets) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}
