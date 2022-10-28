package types

import (
	"fmt"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// DefaultParamspace defines the default qgb module parameter subspace
const (
	DefaultParamspace = ModuleName
)

// ParamsStoreKeyDataCommitmentWindow
var ParamsStoreKeyDataCommitmentWindow = []byte("DataCommitmentWindow")

// DefaultGenesis returns the default Capability genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: &Params{
			DataCommitmentWindow: 400,
		},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	// this line is used by starport scaffolding # genesis/types/validate
	if err := gs.Params.ValidateBasic(); err != nil {
		return sdkerrors.Wrap(err, "params")
	}
	return nil
}

// ParamKeyTable for qgb module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// ParamSetPairs implements the ParamSet interface and returns all the key/value
// pairs of auth module's parameters.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(ParamsStoreKeyDataCommitmentWindow, &p.DataCommitmentWindow, validateDataCommitmentWindow),
	}
}

func validateDataCommitmentWindow(i interface{}) error {
	val, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	} else if val < 100 {
		return fmt.Errorf("invalid average EVM block time, too short for latency limitations")
	}
	return nil
}

// ValidateBasic checks that the parameters have valid values.
func (p Params) ValidateBasic() error {
	if err := validateDataCommitmentWindow(p.DataCommitmentWindow); err != nil {
		return sdkerrors.Wrap(err, "data commitment window")
	}
	return nil
}
