package types

import (
	"fmt"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyMinSquareSize = []byte("MinSquareSize")
	// TODO: Determine the default value
	DefaultMinSquareSize int32 = 0
)

var (
	KeyMaxSquareSize = []byte("MaxSquareSize")
	// TODO: Determine the default value
	DefaultMaxSquareSize int32 = 0
)

// ParamKeyTable the param key table for launch module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(
	minSquareSize int32,
	maxSquareSize int32,
) Params {
	return Params{
		MinSquareSize: minSquareSize,
		MaxSquareSize: maxSquareSize,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(
		DefaultMinSquareSize,
		DefaultMaxSquareSize,
	)
}

// ParamSetPairs get the params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyMinSquareSize, &p.MinSquareSize, validateMinSquareSize),
		paramtypes.NewParamSetPair(KeyMaxSquareSize, &p.MaxSquareSize, validateMaxSquareSize),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := validateMinSquareSize(p.MinSquareSize); err != nil {
		return err
	}

	if err := validateMaxSquareSize(p.MaxSquareSize); err != nil {
		return err
	}

	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// validateMinSquareSize validates the MinSquareSize param
func validateMinSquareSize(v interface{}) error {
	minSquareSize, ok := v.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	// TODO implement validation
	_ = minSquareSize

	return nil
}

// validateMaxSquareSize validates the MaxSquareSize param
func validateMaxSquareSize(v interface{}) error {
	maxSquareSize, ok := v.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	// TODO implement validation
	_ = maxSquareSize

	return nil
}
