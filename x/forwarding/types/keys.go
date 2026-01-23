// Package types provides message types, events, errors, and supporting types
// for the forwarding module which enables cross-chain token forwarding via Hyperlane.
package types

const (
	ModuleName = "forwarding"

	// MaxTokensPerForward prevents unbounded iteration and gas exhaustion.
	MaxTokensPerForward = 20
)
