package state

import (
	"context"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
)

// PaymentPromise is an alias for the protobuf PaymentPromise type.
type PaymentPromise = types.PaymentPromise

// VerifiedPromise holds the result of a successful payment promise verification.
type VerifiedPromise struct {
	// ExpiresAt is the time at which the payment promise expires.
	ExpiresAt time.Time
}

// Client encapsulates everything the fibre server depends on from app/core node.
// The default implementation is the grpc AppClient.
type Client interface {
	// SetGetter is embedded to provide validator set lookups.
	validator.SetGetter

	// ChainID returns the chain ID of the state machine.
	ChainID() string
	// VerifyPromise verifies a payment promise against on-chain state
	// and returns the verification result.
	VerifyPromise(context.Context, *PaymentPromise) (VerifiedPromise, error)

	// Start initializes the client (e.g. detects chain ID).
	Start(context.Context) error
	// Stop clears up underlying resources.
	Stop() error
}
