package fibre

import (
	"context"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app-fibre/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
)

// StateClient encapsulates everything fibre server depends on to app/core node.
// The default implementation is [fibregrpc.AppClient].
type StateClient interface {
	// SetGetter is embedded to provide validator set lookups.
	validator.SetGetter

	// ChainID returns the chain ID of the state machine.
	ChainID() string
	// Verify verifies a payment promise against on-chain state
	// and returns the expiration time.
	Verify(context.Context, *types.PaymentPromise) (time.Time, error)

	// Start initializes the client (e.g. detects chain ID).
	Start(context.Context) error
	// Stop clears up underlying resources.
	Stop() error
}

var _ StateClient = (*fibregrpc.AppClient)(nil)
