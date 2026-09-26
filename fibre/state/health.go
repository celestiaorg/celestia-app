package state

import (
	"context"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	core "github.com/cometbft/cometbft/types"
)

// NodeStatus is a fresh snapshot of the connected app node's status.
type NodeStatus struct {
	ChainID    string
	Height     uint64
	BlockTime  time.Time
	CatchingUp bool
}

// ProviderRegistration is the on-chain Fibre provider registration of a validator.
type ProviderRegistration struct {
	Found bool
	Host  string // valid only when Found
}

// HealthClient performs fresh, uncached queries for the server health checks,
// so request-path caches cannot mask outages or registration changes.
type HealthClient interface {
	NodeStatus(context.Context) (NodeStatus, error)
	ValidatorSetAt(context.Context, uint64) (validator.Set, error)
	ProviderRegistration(context.Context, core.Address) (ProviderRegistration, error)
}
