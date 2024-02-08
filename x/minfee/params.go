package minfee

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyMinGasPrice = []byte("GlobalMinGasPrice")
)

func RegisterMinFeeParamTable(ps paramtypes.Subspace) {
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(ParamKeyTable())
	}
}

// ParamKeyTable returns the param key table for the blob module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

type Params struct {
	MinGasPrice sdk.Dec
}

func (p Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyMinGasPrice, &p.MinGasPrice, validateMinGasPrice),
	}
}

func validateMinGasPrice(i interface{}) error {
	_, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}