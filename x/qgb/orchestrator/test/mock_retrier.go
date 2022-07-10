package test

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
)

var _ orchestrator.RetrierI = &mockRetrier{}

type mockRetrier struct {
}

func NewMockRetrier() *mockRetrier {
	return &mockRetrier{}
}

func (r mockRetrier) Retry(ctx context.Context, nonce uint64, retryMethod func(context.Context, uint64) error) error {
	return nil
}

func (r mockRetrier) RetryThenFail(ctx context.Context, nonce uint64, retryMethod func(context.Context, uint64) error) {
}
