package validator

import (
	"context"

	core "github.com/cometbft/cometbft/types"
)

// Host represents a validator's network address as a string.
// It can be a DNS name, IPv4 address, or IPv6 address, optionally with a port.
type Host string

// String returns the string representation of the host.
func (h Host) String() string {
	return string(h)
}

// HostRegistry provides a mapping from validators to their network hosts.
type HostRegistry interface {
	// GetHost returns the network host for a given validator.
	// Returns an error if the validator's host cannot be determined.
	GetHost(context.Context, *core.Validator) (Host, error)

	// RefreshHost re-queries state for the validator's host, rate-limited to
	// once per block time per validator. It returns:
	//   - changed: the on-chain host differs from the cached one (the cache is
	//     updated to the new value, valid or not);
	//   - isValid: the new host passes host validation;
	//   - err: the query failed (changed and isValid are false).
	// When rate-limited, it returns (false, false, nil) without querying.
	// Callers should retry against the validator only when changed && isValid.
	RefreshHost(context.Context, *core.Validator) (changed bool, isValid bool, err error)
}
