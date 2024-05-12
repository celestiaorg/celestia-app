package minfee

import (
	"fmt"

	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const ModuleName = "minfee"

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyGlobalMinGasPrice     = []byte("GlobalMinGasPrice")
	DefaultGlobalMinGasPrice sdk.Dec
)

func init() {
	DefaultGlobalMinGasPriceDec, err := sdk.NewDecFromStr(fmt.Sprintf("%f", v2.GlobalMinGasPrice))
	if err != nil {
		panic(err)
	}
	DefaultGlobalMinGasPrice = DefaultGlobalMinGasPriceDec
}

type Params struct {
	GlobalMinGasPrice sdk.Dec
}

// RegisterMinFeeParamTable attaches a key table to the provided subspace if it
// doesn't have one.
func RegisterMinFeeParamTable(subspace paramtypes.Subspace) paramtypes.Subspace {
	if !subspace.HasKeyTable() {
		return subspace.WithKeyTable(ParamKeyTable())
	}
	return subspace
}

// ParamKeyTable returns the param key table for the global min gas price module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// ParamSetPairs gets the param key-value pair
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyGlobalMinGasPrice, &p.GlobalMinGasPrice, ValidateMinGasPrice),
	}
}

// Validate validates the param type
func ValidateMinGasPrice(i interface{}) error {
	_, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}
