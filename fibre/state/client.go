package state

import (
	"context"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
)

// PaymentPromise is an alias for the protobuf PaymentPromise type.
type PaymentPromise = types.PaymentPromise

// VerifiedPromise holds the result of a successful payment promise verification.
type VerifiedPromise struct {
	// ExpiresAt is the time at which the payment promise expires.
	ExpiresAt time.Time
	// ShardRetention is the on-chain minimum local retention for the uploaded shard.
	ShardRetention time.Duration
}

// Client encapsulates everything the fibre server and client depend on from app/core node.
// The default implementation is the grpc AppClient.
type Client interface {
	// SetGetter is embedded to provide validator set lookups.
	validator.SetGetter
	// HostRegistry is embedded to provide validator host resolution.
	validator.HostRegistry

	// ChainID returns the chain ID of the state machine.
	ChainID() string
	// VerifyPromise verifies a payment promise against on-chain state
	// and returns the verification result.
	VerifyPromise(context.Context, *PaymentPromise) (VerifiedPromise, error)
	// FullStakeStorageBudget returns the FullStakeStorageBudget governance
	// parameter in bytes.
	FullStakeStorageBudget(context.Context) (int64, error)
	// NodeStatus returns the app node's current status. Health checks use it.
	NodeStatus(context.Context) (NodeStatus, error)
	// ProviderRegistration returns the on-chain Fibre provider registration of
	// the validator with the given consensus address. Health checks use it.
	ProviderRegistration(context.Context, core.Address) (ProviderRegistration, error)

	// Start initializes the client (e.g. detects chain ID).
	Start(context.Context) error
	// Stop clears up underlying resources.
	Stop(context.Context) error
}

// NodeStatus is a snapshot of the app node's status.
type NodeStatus struct {
	ChainID    string
	Height     uint64
	BlockTime  time.Time
	CatchingUp bool
}

// ProviderRegistration is the on-chain Fibre provider registration of a validator.
type ProviderRegistration struct {
	Found bool
	Host  string // set only when Found
}
