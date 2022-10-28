package types

import (
	"fmt"
	"math/big"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var _ AttestationRequestI = &Valset{}

// NewValset returns a new valset.
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

// Hash mimics the 'computeValsetHash' function used by the qgb contracts by using
// a Valset to compute the hash of the abi encoded validator set.
func (v *Valset) Hash() (ethcmn.Hash, error) {
	ethVals := make([]wrapper.Validator, len(v.Members))
	for i, val := range v.Members {
		ethVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EvmAddress),
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

func (v Valset) Type() AttestationType {
	return ValsetRequestType
}
