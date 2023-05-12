package types

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyGasPerBlobByte            = []byte("GasPerBlobByte")
	DefaultGasPerBlobByte uint32 = appconsts.DefaultGasPerBlobByte
)

// ParamKeyTable returns the param key table for the blob module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(GasPerBlobByte uint32) Params {
	return Params{
		GasPerBlobByte: GasPerBlobByte,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(DefaultGasPerBlobByte)
}

// ParamSetPairs gets the list of param key-value pairs
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyGasPerBlobByte, &p.GasPerBlobByte, validateGasPerBlobByte),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	return validateGasPerBlobByte(p.GasPerBlobByte)
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// validateGasPerBlobByte validates the GasPerBlobByte param
func validateGasPerBlobByte(v interface{}) error {
	GasPerBlobByte, ok := v.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if GasPerBlobByte == 0 {
		return fmt.Errorf("gas per blob byte cannot be 0")
	}

	return nil
}
