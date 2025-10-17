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
}
