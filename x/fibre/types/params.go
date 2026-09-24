package types

import (
	"fmt"
	"time"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyWithdrawalDelay            = []byte("WithdrawalDelay")
	KeyPaymentPromiseTimeout      = []byte("PaymentPromiseTimeout")
	KeyPaymentPromiseHeightWindow = []byte("PaymentPromiseHeightWindow")
	KeyShardRetention             = []byte("ShardRetention")
	KeyFullStakeStorageBudget     = []byte("FullStakeStorageBudget")

	// DefaultWithdrawalDelay is the initial value of the withdrawal delay parameter.
	DefaultWithdrawalDelay = 24 * time.Hour
	// DefaultPaymentPromiseTimeout is the initial value of the payment promise timeout parameter.
	DefaultPaymentPromiseTimeout = 1 * time.Hour
	// DefaultPaymentPromiseHeightWindow is the initial value of the payment promise height window parameter.
	DefaultPaymentPromiseHeightWindow uint64 = 1000
	// DefaultShardRetention is the initial value of the shard retention parameter. It is the
	// minimum local duration validators keep uploaded shards, decoupled from PaymentPromiseTimeout.
	DefaultShardRetention = 4 * time.Hour
	// DefaultFullStakeStorageBudget caps the Fibre disk of a 100%-stake validator
	// over one ShardRetention window.
	DefaultFullStakeStorageBudget uint64 = 2 << 40 // 2 TiB (~146 MiB/s full-stake)
)

const (
	// MaxPromiseClockSkew is how far a promise's creation_timestamp may lead block
	// time. Absorbs clock drift only, so it must stay well under
	// WithdrawalDelay - PaymentPromiseTimeout.
	MaxPromiseClockSkew = 10 * time.Minute

	// MinTimeoutSettlementWindow is how long the timeout settlement path is
	// guaranteed to stay open. MsgPaymentPromiseTimeout is accepted from
	// creation_timestamp + payment_promise_timeout until the promise stops being
	// settleable at creation_timestamp + withdrawal_delay, so the delay has to
	// exceed the timeout by at least this much for a processor to submit the
	// message and get it into a block. Were the two allowed to meet, the window
	// would be empty and an expired promise could never be claimed.
	MinTimeoutSettlementWindow = 10 * time.Minute

	// MinWithdrawalDelay is the lower bound of the withdrawal delay parameter.
	// The delay is also how long a promise stays settleable after its
	// creation_timestamp, so it must cover the longest allowed
	// payment_promise_timeout or a promise could go stale before its own
	// normal-path expiry. It adds MinTimeoutSettlementWindow on top so that
	// every valid combination of the two params leaves a usable timeout
	// settlement window, without needing a cross-param check.
	MinWithdrawalDelay = MaxPaymentPromiseTimeout + MinTimeoutSettlementWindow
	// MaxWithdrawalDelay is the upper bound of the withdrawal delay parameter. It
	// caps how long escrowed funds stay locked after a withdrawal request, and it
	// keeps the retention window floor (withdrawal_delay + MaxPromiseClockSkew)
	// computed in Validate far from overflowing time.Duration.
	MaxWithdrawalDelay = 7 * 24 * time.Hour

	// MinPaymentPromiseTimeout is the lower bound of the payment promise timeout
	// parameter. A promise has to stay valid long enough for the client to upload
	// its shards to the assigned validators, collect their signatures, and get
	// MsgPayForFibre included in a block.
	MinPaymentPromiseTimeout = 10 * time.Minute
	// MaxPaymentPromiseTimeout is the upper bound of the payment promise timeout
	// parameter.
	MaxPaymentPromiseTimeout = 12 * time.Hour

	// MinShardRetention is the lower bound of the shard retention parameter.
	MinShardRetention = 10 * time.Minute
	// MaxShardRetention is the upper bound of the shard retention parameter.
	MaxShardRetention = 7 * 24 * time.Hour
)

// ParamKeyTable returns the param key table for the fibre module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(withdrawalDelay, paymentPromiseTimeout time.Duration, paymentPromiseHeightWindow uint64, shardRetention time.Duration, storageBudget uint64) Params {
	return Params{
		WithdrawalDelay:            withdrawalDelay,
		PaymentPromiseTimeout:      paymentPromiseTimeout,
		PaymentPromiseHeightWindow: paymentPromiseHeightWindow,
		ShardRetention:             shardRetention,
		FullStakeStorageBudget:     storageBudget,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(DefaultWithdrawalDelay, DefaultPaymentPromiseTimeout, DefaultPaymentPromiseHeightWindow, DefaultShardRetention, DefaultFullStakeStorageBudget)
}

// PaymentPromiseRetentionWindow is how long a processed-payment record is kept
// before pruning. It is derived, not configured: a promise stays settleable for
// WithdrawalDelay + MaxPromiseClockSkew after creation, so the record must
// outlive that span or a pruned promise could be replayed.
func (p Params) PaymentPromiseRetentionWindow() time.Duration {
	return p.WithdrawalDelay + MaxPromiseClockSkew
}

// ParamSetPairs gets the list of param key-value pairs
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyWithdrawalDelay, &p.WithdrawalDelay, validateWithdrawalDelay),
		paramtypes.NewParamSetPair(KeyPaymentPromiseTimeout, &p.PaymentPromiseTimeout, validatePaymentPromiseTimeout),
		paramtypes.NewParamSetPair(KeyPaymentPromiseHeightWindow, &p.PaymentPromiseHeightWindow, validatePaymentPromiseHeightWindow),
		paramtypes.NewParamSetPair(KeyShardRetention, &p.ShardRetention, validateShardRetention),
		paramtypes.NewParamSetPair(KeyFullStakeStorageBudget, &p.FullStakeStorageBudget, validateFullStakeStorageBudget),
	}
}

// Validate validates the set of params. PaymentPromiseRetentionWindow is derived
// from WithdrawalDelay, not stored, so there is nothing to validate for it.
func (p Params) Validate() error {
	if err := validateWithdrawalDelay(&p.WithdrawalDelay); err != nil {
		return err
	}
	if err := validatePaymentPromiseTimeout(&p.PaymentPromiseTimeout); err != nil {
		return err
	}
	if err := validatePaymentPromiseHeightWindow(p.PaymentPromiseHeightWindow); err != nil {
		return err
	}
	if err := validateShardRetention(&p.ShardRetention); err != nil {
		return err
	}
	if err := validateFullStakeStorageBudget(p.FullStakeStorageBudget); err != nil {
		return err
	}
	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
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

	if *duration < MinWithdrawalDelay {
		return fmt.Errorf("withdrawal delay must be at least %s: %s", MinWithdrawalDelay, *duration)
	}

	if *duration > MaxWithdrawalDelay {
		return fmt.Errorf("withdrawal delay must be at most %s: %s", MaxWithdrawalDelay, *duration)
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

	if *duration < MinPaymentPromiseTimeout {
		return fmt.Errorf("payment promise timeout must be at least %s: %s", MinPaymentPromiseTimeout, *duration)
	}

	if *duration > MaxPaymentPromiseTimeout {
		return fmt.Errorf("payment promise timeout must be at most %s: %s", MaxPaymentPromiseTimeout, *duration)
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

// validateShardRetention validates the ShardRetention param
func validateShardRetention(v any) error {
	duration, ok := v.(*time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if duration == nil {
		return fmt.Errorf("shard retention cannot be nil")
	}

	if *duration < MinShardRetention {
		return fmt.Errorf("shard retention must be at least %s: %s", MinShardRetention, *duration)
	}

	if *duration > MaxShardRetention {
		return fmt.Errorf("shard retention must be at most %s: %s", MaxShardRetention, *duration)
	}

	return nil
}

func validateFullStakeStorageBudget(v any) error {
	budget, ok := v.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", v)
	}

	if budget == 0 {
		return fmt.Errorf("full stake storage budget must be positive; disable the limiter locally with the server --unlimited-budget flag instead")
	}

	// No upper bound: any positive budget is a valid governance choice; the
	// per-node budget derivation is overflow-safe on its own.
	return nil
}
