package orchestrator

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

type AppClient interface {
	SubscribeValset(ctx context.Context) (<-chan types.Valset, error)
	SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error)
}
