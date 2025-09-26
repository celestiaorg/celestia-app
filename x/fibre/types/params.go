package types

import (
	"fmt"
	"time"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyGasPerBlobByte  = []byte("GasPerBlobByte")
	KeyWithdrawalDelay = []byte("WithdrawalDelay")
	KeyPromiseTimeout  = []byte("PromiseTimeout")

	// DefaultGasPerBlobByte is the initial value of the gas per blob byte parameter.
	DefaultGasPerBlobByte uint32 = 1
	// DefaultWithdrawalDelay is the initial value of the withdrawal delay parameter.
	DefaultWithdrawalDelay = 24 * time.Hour
	// DefaultPromiseTimeout is the initial value of the promise timeout parameter.
	DefaultPromiseTimeout = 1 * time.Hour
)

// ParamKeyTable returns the param key table for the fibre module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(gasPerBlobByte uint32, withdrawalDelay, promiseTimeout time.Duration) Params {
	return Params{
		GasPerBlobByte:  gasPerBlobByte,
		WithdrawalDelay: withdrawalDelay,
		PromiseTimeout:  promiseTimeout,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(DefaultGasPerBlobByte, DefaultWithdrawalDelay, DefaultPromiseTimeout)
}

// ParamSetPairs gets the list of param key-value pairs
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyGasPerBlobByte, &p.GasPerBlobByte, validateGasPerBlobByte),
		paramtypes.NewParamSetPair(KeyWithdrawalDelay, &p.WithdrawalDelay, validateWithdrawalDelay),
		paramtypes.NewParamSetPair(KeyPromiseTimeout, &p.PromiseTimeout, validatePromiseTimeout),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := validateGasPerBlobByte(p.GasPerBlobByte); err != nil {
		return err
	}
	if err := validateWithdrawalDelay(p.WithdrawalDelay); err != nil {
		return err
	}
	return validatePromiseTimeout(p.PromiseTimeout)
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
		return fmt.Errorf("gas per blob byte cannot be 0")
	}

	return nil
}

// validateWithdrawalDelay validates the WithdrawalDelay param
func validateWithdrawalDelay(v interface{}) error {
	duration, ok := v.(*time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if duration == nil {
		return fmt.Errorf("withdrawal delay cannot be nil")
	}

	if *duration <= 0 {
		return fmt.Errorf("withdrawal delay must be positive: %s", *duration)
	}

	return nil
}

// validatePromiseTimeout validates the PromiseTimeout param
func validatePromiseTimeout(v interface{}) error {
	duration, ok := v.(*time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if duration == nil {
		return fmt.Errorf("promise timeout cannot be nil")
	}

	if *duration <= 0 {
		return fmt.Errorf("promise timeout must be positive: %s", *duration)
	}

	return nil
}
