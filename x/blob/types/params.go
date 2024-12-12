package types

import (
	"errors"
	"fmt"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/go-square/v2"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyGasPerBlobByte              = []byte("GasPerBlobByte")
	DefaultGasPerBlobByte   uint32 = appconsts.DefaultGasPerBlobByte
	KeyGovMaxSquareSize            = []byte("GovMaxSquareSize")
	DefaultGovMaxSquareSize uint64 = appconsts.DefaultGovMaxSquareSize
)

// ParamKeyTable returns the param key table for the blob module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(gasPerBlobByte uint32, govMaxSquareSize uint64) Params {
	return Params{
		GasPerBlobByte:   gasPerBlobByte,
		GovMaxSquareSize: govMaxSquareSize,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(DefaultGasPerBlobByte, appconsts.DefaultGovMaxSquareSize)
}

// ParamSetPairs gets the list of param key-value pairs
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyGasPerBlobByte, &p.GasPerBlobByte, validateGasPerBlobByte),
		paramtypes.NewParamSetPair(KeyGovMaxSquareSize, &p.GovMaxSquareSize, validateGovMaxSquareSize),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	err := validateGasPerBlobByte(p.GasPerBlobByte)
	if err != nil {
		return err
	}
	return validateGovMaxSquareSize(p.GovMaxSquareSize)
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// validateGasPerBlobByte validates the GasPerBlobByte param
func validateGasPerBlobByte(v interface{}) error {
	gasPerBlobByte, ok := v.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if gasPerBlobByte == 0 {
		return errors.New("gas per blob byte cannot be 0")
	}

	return nil
}

// validateGovMaxSquareSize validates the GovMaxSquareSize param
func validateGovMaxSquareSize(v interface{}) error {
	govMaxSquareSize, ok := v.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if govMaxSquareSize == 0 {
		return errors.New("gov max square size cannot be zero")
	}

	if !square.IsPowerOfTwo(govMaxSquareSize) {
		return fmt.Errorf(
			"gov max square size must be a power of two: %d",
			govMaxSquareSize,
		)
	}

	return nil
}
