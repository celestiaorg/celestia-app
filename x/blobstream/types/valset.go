package types

import (
	"fmt"
	"math/big"
	"time"

	"cosmossdk.io/errors"
	wrapper "github.com/celestiaorg/blobstream-contracts/v3/wrappers/Blobstream.sol"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var _ AttestationRequestI = &Valset{}

// NewValset returns a new valset.
func NewValset(nonce, height uint64, members InternalBridgeValidators, blockTime time.Time) (*Valset, error) {
	if err := members.ValidateBasic(); err != nil {
		return nil, errors.Wrap(err, "invalid members")
	}
	members.Sort()
	mem := make([]BridgeValidator, 0)
	for _, val := range members {
		mem = append(mem, val.ToExternal())
	}
	vs := Valset{Nonce: nonce, Members: mem, Height: height, Time: blockTime}
	return &vs, nil
}

// SignBytes produces the bytes that celestia validators are required to sign
// over when the validator set changes.
func (v *Valset) SignBytes() (ethcmn.Hash, error) {
	vsHash, err := v.Hash()
	if err != nil {
		return ethcmn.Hash{}, err
	}

	// the word 'checkpoint' needs to be the same as the 'name' above in the
	// checkpointAbiJson but other than that it's a constant that has no impact
	// on the output. This is because it gets encoded as a function name which
	// we must then discard.
	bytes, err := InternalBlobstreamABI.Pack(
		"domainSeparateValidatorSetHash",
		VsDomainSeparator,
		big.NewInt(int64(v.Nonce)),
		big.NewInt(int64(v.TwoThirdsThreshold())),
		vsHash,
	)
	// this should never happen outside of test since any case that could crash
	// on encoding should be filtered above.
	if err != nil {
		panic(fmt.Sprintf("Error packing checkpoint! %s/n", err))
	}

	hash := crypto.Keccak256Hash(bytes[4:])
	return hash, nil
}

// Hash mimics the 'computeValsetHash' function used by the Blobstream contracts by
// using a Valset to compute the hash of the abi encoded validator set.
func (v *Valset) Hash() (ethcmn.Hash, error) {
	ethVals := make([]wrapper.Validator, len(v.Members))
	for i, val := range v.Members {
		ethVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EvmAddress),
			Power: big.NewInt(int64(val.Power)),
		}
	}

	encodedVals, err := InternalBlobstreamABI.Pack("computeValidatorSetHash", ethVals)
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

func (v *Valset) BlockTime() time.Time {
	return v.Time
}
