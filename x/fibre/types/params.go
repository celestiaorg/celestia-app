package types

import (
	"fmt"
	"time"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyGasPerBlobByte                = []byte("GasPerBlobByte")
	KeyWithdrawalDelay               = []byte("WithdrawalDelay")
	KeyPaymentPromiseTimeout         = []byte("PaymentPromiseTimeout")
	KeyPaymentPromiseRetentionWindow = []byte("PaymentPromiseRetentionWindow")
	KeyPaymentPromiseHeightWindow    = []byte("PaymentPromiseHeightWindow")

	// DefaultGasPerBlobByte is the initial value of the gas per blob byte parameter.
	DefaultGasPerBlobByte uint32 = 1
	// DefaultWithdrawalDelay is the initial value of the withdrawal delay parameter.
	DefaultWithdrawalDelay = 24 * time.Hour
	// DefaultPaymentPromiseTimeout is the initial value of the payment promise timeout parameter.
	DefaultPaymentPromiseTimeout = 1 * time.Hour
	// DefaultPaymentPromiseRetentionWindow is the initial value of the payment promise retention window parameter.
	DefaultPaymentPromiseRetentionWindow = 24 * time.Hour
	// DefaultPaymentPromiseHeightWindow is the initial value of the payment promise height window parameter.
	DefaultPaymentPromiseHeightWindow uint64 = 1000
)

// ParamKeyTable returns the param key table for the fibre module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(gasPerBlobByte uint32, withdrawalDelay, paymentPromiseTimeout, paymentPromiseRetentionWindow time.Duration, paymentPromiseHeightWindow uint64) Params {
	return Params{
		GasPerBlobByte:                gasPerBlobByte,
		WithdrawalDelay:               withdrawalDelay,
		PaymentPromiseTimeout:         paymentPromiseTimeout,
		PaymentPromiseRetentionWindow: paymentPromiseRetentionWindow,
		PaymentPromiseHeightWindow:    paymentPromiseHeightWindow,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(DefaultGasPerBlobByte, DefaultWithdrawalDelay, DefaultPaymentPromiseTimeout, DefaultPaymentPromiseRetentionWindow, DefaultPaymentPromiseHeightWindow)
}

// ParamSetPairs gets the list of param key-value pairs
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyGasPerBlobByte, &p.GasPerBlobByte, validateGasPerBlobByte),
		paramtypes.NewParamSetPair(KeyWithdrawalDelay, &p.WithdrawalDelay, validateWithdrawalDelay),
		paramtypes.NewParamSetPair(KeyPaymentPromiseTimeout, &p.PaymentPromiseTimeout, validatePaymentPromiseTimeout),
		paramtypes.NewParamSetPair(KeyPaymentPromiseRetentionWindow, &p.PaymentPromiseRetentionWindow, validatePaymentPromiseRetentionWindow),
		paramtypes.NewParamSetPair(KeyPaymentPromiseHeightWindow, &p.PaymentPromiseHeightWindow, validatePaymentPromiseHeightWindow),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := validateGasPerBlobByte(p.GasPerBlobByte); err != nil {
		return err
	}
	if err := validateWithdrawalDelay(&p.WithdrawalDelay); err != nil {
		return err
	}
	if err := validatePaymentPromiseTimeout(&p.PaymentPromiseTimeout); err != nil {
		return err
	}
	if err := validatePaymentPromiseRetentionWindow(&p.PaymentPromiseRetentionWindow); err != nil {
		return err
	}
	if err := validatePaymentPromiseHeightWindow(p.PaymentPromiseHeightWindow); err != nil {
		return err
	}
	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// validateGasPerBlobByte validates the GasPerBlobByte param
func validateGasPerBlobByte(v any) error {
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
func validateWithdrawalDelay(v any) error {
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

// validatePaymentPromiseTimeout validates the PaymentPromiseTimeout param
func validatePaymentPromiseTimeout(v any) error {
	duration, ok := v.(*time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if duration == nil {
		return fmt.Errorf("payment promise timeout cannot be nil")
	}

	if *duration <= 0 {
		return fmt.Errorf("payment promise timeout must be positive: %s", *duration)
	}

	return nil
}

// validatePaymentPromiseRetentionWindow validates the PaymentPromiseRetentionWindow param
func validatePaymentPromiseRetentionWindow(v any) error {
	duration, ok := v.(*time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if duration == nil {
		return fmt.Errorf("payment promise retention window cannot be nil")
	}

	if *duration <= 0 {
		return fmt.Errorf("payment promise retention window must be positive: %s", *duration)
	}

	return nil
}

// validatePaymentPromiseHeightWindow validates the PaymentPromiseHeightWindow param
func validatePaymentPromiseHeightWindow(v any) error {
	heightWindow, ok := v.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if heightWindow == 0 {
		return fmt.Errorf("payment promise height window cannot be 0")
	}

	return nil
}
