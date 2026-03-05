package validator

import (
	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
)

// Set wraps [*core.ValidatorSet] with the height at which it is valid.
type Set struct {
	*core.ValidatorSet
	Height uint64
}

// GetByAddress finds a validator in the set by address.
// Returns the validator pointer from ValidatorSet.Validators (required for shard map lookups)
// and true if found, or nil and false if not found.
func (s Set) GetByAddress(address crypto.Address) (*core.Validator, bool) {
	for _, val := range s.Validators {
		if val.Address.String() == address.String() {
			return val, true
		}
	}
	return nil, false
}
