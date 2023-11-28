package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var KeySignalQuorum = []byte("SignalQuorum")

// ParamKeyTable returns the param key table for the upgrade module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func NewParams(signalQuorum sdk.Dec) Params {
	return Params{
		SignalQuorum: signalQuorum,
	}
}

func DefaultParams() Params {
	return Params{
		SignalQuorum: DefaultSignalQuorum,
	}
}

var (
	// 2/3
	MinSignalQuorum = sdk.NewDec(2).Quo(sdk.NewDec(3))
	// 5/6
	DefaultSignalQuorum = sdk.NewDec(5).Quo(sdk.NewDec(6))
)

func (p Params) Validate() error {
	return validateSignalQuorum(p.SignalQuorum)
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeySignalQuorum, &p.SignalQuorum, validateSignalQuorum),
	}
}

func validateSignalQuorum(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v.LT(MinSignalQuorum) {
		return fmt.Errorf("quorum must be at least %s (2/3), got %s", MinSignalQuorum, v)
	}

	if v.GT(sdk.OneDec()) {
		return fmt.Errorf("quorum must be less than or equal to 1, got %d", v)
	}

	return nil
}
