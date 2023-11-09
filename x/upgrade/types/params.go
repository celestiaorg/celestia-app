package types

import (
	"fmt"
	"math"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var KeySignalQuorum = []byte("SignalQuorum")

// ParamKeyTable returns the param key table for the blob module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func NewParams(signalQuorum uint32) Params {
	return Params{
		SignalQuorum: signalQuorum,
	}
}

func DefaultParams() *Params {
	return &Params{
		SignalQuorum: MinSignalQuorum,
	}
}

// 2/3
const MinSignalQuorum = uint32(math.MaxUint32 * 2 / 3)

func (p Params) Validate() error {
	return validateSignalQuorum(p.SignalQuorum)
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeySignalQuorum, &p.SignalQuorum, validateSignalQuorum),
	}
}

func validateSignalQuorum(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v < MinSignalQuorum {
		return fmt.Errorf("quorum must be at least %d (2/3), got %d", MinSignalQuorum, v)
	}
	return nil
}
