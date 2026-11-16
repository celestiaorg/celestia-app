package types

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// EventTypeSetFibreProviderInfo is the event type for setting fibre provider info
	EventTypeSetFibreProviderInfo = "set_fibre_provider_info"

	// AttributeKeyValidatorAddress is the attribute key for consensus address
	AttributeKeyValidatorAddress = "validator_consensus_address"
	// AttributeKeyHost is the attribute key for IP address
	AttributeKeyHost = "host"

	// MaxHostLen is the maximum length for the host field (IP address, DNS name, etc.)
	MaxHostLen = 100

	// minPort/maxPort define the valid TCP/UDP port range. Port 0 is excluded
	// because it is not a valid listening port — clients connecting to it
	// would fail at the dialer.
	minPort = 1
	maxPort = 65535
)

var _ sdk.Msg = &MsgSetFibreProviderInfo{}

// ValidateBasic performs basic validation of the MsgSetFibreProviderInfo message.
//
// Host must be in canonical `host:port` form: a non-empty host (IP literal or
// DNS name) followed by a numeric port in the range [1, 65535]. Schemes
// (http://, dns:///, unix:, etc.) and URL paths are rejected — every fibre client uses the
// same gRPC transport, so per-provider scheme variation has no use case and
// historically led to operators registering hosts that could not be dialled.
func (m *MsgSetFibreProviderInfo) ValidateBasic() error {
	if m.Signer == "" {
		return errorsmod.Wrap(ErrInvalidValidator, "validator address cannot be empty")
	}
	if _, err := sdk.ValAddressFromBech32(m.Signer); err != nil {
		return errorsmod.Wrapf(ErrInvalidValidator, "invalid validator address: %v", err)
	}
	return ValidateHost(m.Host)
}

// ValidateHost enforces the canonical `host:port` form on a fibre provider
// host string. It is exported so off-chain callers (CLI, host registry,
// test helpers) can apply the exact same check the chain enforces.
func ValidateHost(host string) error {
	if host == "" {
		return errorsmod.Wrap(ErrInvalidHostAddress, "host cannot be empty")
	}
	if len(host) > MaxHostLen {
		return errorsmod.Wrapf(ErrInvalidHostAddress,
			"host must be at most %d characters, got %d", MaxHostLen, len(host))
	}

	hostPart, portPart, err := net.SplitHostPort(host)
	if err != nil {
		return errorsmod.Wrapf(ErrInvalidHostAddress,
			"host must be in host:port form, got %q: %v", host, err)
	}
	if hostPart == "" {
		return errorsmod.Wrapf(ErrInvalidHostAddress, "host part cannot be empty in %q", host)
	}
	port, err := strconv.Atoi(portPart)
	if err != nil {
		return errorsmod.Wrapf(ErrInvalidHostAddress,
			"port must be numeric in %q: %v", host, err)
	}
	if port < minPort || port > maxPort {
		return errorsmod.Wrapf(ErrInvalidHostAddress,
			"port %d in %q out of range [%d, %d]", port, host, minPort, maxPort)
	}
	// Reject spellings that split into a valid host and port but are not the
	// canonical form, such as "[host]:80", "host:+80" or "host:080".
	if canonical := net.JoinHostPort(hostPart, strconv.Itoa(port)); canonical != host {
		return errorsmod.Wrapf(ErrInvalidHostAddress,
			"host %q is not in canonical form %q", host, canonical)
	}
	if net.ParseIP(hostPart) == nil {
		if err := validateHostName(hostPart); err != nil {
			return errorsmod.Wrapf(ErrInvalidHostAddress, "invalid host name in %q: %v", host, err)
		}
	}
	return nil
}

// grpcResolverSchemes are the gRPC target schemes that a "scheme:port" host
// string would select instead of a network host. The list is fixed so that
// validation does not depend on which resolvers a binary registers.
var grpcResolverSchemes = map[string]struct{}{
	"dns":           {},
	"passthrough":   {},
	"unix":          {},
	"unix-abstract": {},
	"xds":           {},
	"google-c2p":    {},
}

// validateHostName checks that name is a host name: letters, digits, '-',
// '_' and '.', not starting or ending with '-' or '.', and not a gRPC
// resolver scheme.
func validateHostName(name string) error {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '-', c == '_', c == '.':
		default:
			return fmt.Errorf("character %q is not allowed", c)
		}
	}
	if first, last := name[0], name[len(name)-1]; first == '-' || first == '.' || last == '-' || last == '.' {
		return fmt.Errorf("%q must not start or end with '-' or '.'", name)
	}
	if _, ok := grpcResolverSchemes[strings.ToLower(name)]; ok {
		return fmt.Errorf("%q is a gRPC resolver scheme", name)
	}
	return nil
}
