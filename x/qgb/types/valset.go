package types

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ AttestationRequestI = &Valset{}

// NewValset returns a new valset.
func NewValset(nonce, height uint64, members InternalBridgeValidators) (*Valset, error) {
	if err := members.ValidateBasic(); err != nil {
		return nil, sdkerrors.Wrap(err, "invalid members")
	}
	members.Sort()
	mem := make([]BridgeValidator, 0)
	for _, val := range members {
		mem = append(mem, val.ToExternal())
	}
	vs := Valset{Nonce: nonce, Members: mem, Height: height}
	return &vs, nil
}

func (v *Valset) TwoThirdsThreshold() uint64 {
	totalPower := uint64(0)
	for _, member := range v.Members {
		totalPower += member.Power
	}

	// todo: fix to be more precise
	oneThird := (totalPower / 3) + 1 // +1 to round up
	return 2 * oneThird
}

func (v *Valset) Type() AttestationType {
	return ValsetRequestType
}
