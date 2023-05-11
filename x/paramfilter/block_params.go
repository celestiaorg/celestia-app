package paramfilter

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/cosmos/cosmos-sdk/baseapp"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// DefaultConsensusParams returns a default ConsensusParams.
func DefaultConsensusParams(maxSquareSize uint64) *tmproto.ConsensusParams {
	return &tmproto.ConsensusParams{
		Block:     DefaultBlockParams(maxSquareSize),
		Evidence:  coretypes.DefaultEvidenceParams(),
		Validator: coretypes.DefaultValidatorParams(),
		Version:   coretypes.DefaultVersionParams(),
	}
}

// DefaultBlockParams returns a default BlockParams.
func DefaultBlockParams(maxSquareSize uint64) tmproto.BlockParams {
	return tmproto.BlockParams{
		MaxBytes:   square.EstimateMaxBlockBytes(maxSquareSize),
		MaxGas:     -1,
		TimeIotaMs: 1000, // 1s
	}
}

// ConsensusParamsKeyTable returns an x/params module keyTable to be used in the
// BaseApp's ParamStore. The KeyTable registers the types along with the their
// validation functions. Note that this replaces the standard block params
// validation with a custom function.
func ConsensusParamsKeyTable(maxSquareSize uint64) paramtypes.KeyTable {
	return paramtypes.NewKeyTable(
		paramtypes.NewParamSetPair(
			baseapp.ParamStoreKeyBlockParams, abci.BlockParams{}, newBlockParamsValidator(maxSquareSize),
		),
		paramtypes.NewParamSetPair(
			baseapp.ParamStoreKeyEvidenceParams, tmproto.EvidenceParams{}, baseapp.ValidateEvidenceParams,
		),
		paramtypes.NewParamSetPair(
			baseapp.ParamStoreKeyValidatorParams, tmproto.ValidatorParams{}, baseapp.ValidateValidatorParams,
		),
	)
}

// validateBlockParams defines a stateless validation on BlockParams. This function
// is called whenever the parameters are updated or stored.
func newBlockParamsValidator(squareSize uint64) func(i interface{}) error {
	return func(i interface{}) error {
		v, ok := i.(abci.BlockParams)
		if !ok {
			return fmt.Errorf("invalid parameter type: %T", i)
		}

		if v.MaxBytes <= 0 {
			return fmt.Errorf("block maximum bytes must be positive: %d", v.MaxBytes)
		}

		maxBlockBytes := square.EstimateMaxBlockBytes(squareSize)
		if v.MaxBytes > maxBlockBytes {
			return fmt.Errorf("block maximum bytes must be less than or equal to %d: %d", maxBlockBytes, v.MaxBytes)
		}

		if v.MaxGas < -1 {
			return fmt.Errorf("block maximum gas must be greater than or equal to -1: %d", v.MaxGas)
		}

		return nil
	}
}
