package paramfilter

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/cosmos/cosmos-sdk/baseapp"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// DefaultConsensusParams returns a ConsensusParams with a MaxBytes
// determined using a goal square size.
func DefaultConsensusParams() *tmproto.ConsensusParams {
	return &tmproto.ConsensusParams{
		Block:     DefaultBlockParams(),
		Evidence:  coretypes.DefaultEvidenceParams(),
		Validator: coretypes.DefaultValidatorParams(),
		Version:   coretypes.DefaultVersionParams(), // TODO: set the default version to 1
	}
}

// DefaultBlockParams returns a default BlockParams with a MaxBytes determined
// using a goal square size.
func DefaultBlockParams() tmproto.BlockParams {
	return tmproto.BlockParams{
		// since the max square size is already enforced as a hard cap, the only
		// benefit we are getting here is stopping governance proposals from
		// proposing a useless proposal. Its possible that this value is not
		// as efficient as a larger value.
		MaxBytes:   square.EstimateMaxBlockBytes(appconsts.MaxSquareSize),
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
