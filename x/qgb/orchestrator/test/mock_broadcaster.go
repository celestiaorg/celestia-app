package test

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ orchestrator.BroadcasterI = &mockBroadcaster{}

type mockBroadcaster struct {
	broadcasted []sdk.Msg
}

func NewMockBroadcaster() *mockBroadcaster {
	return &mockBroadcaster{}
}

func (m *mockBroadcaster) BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error) {
	m.broadcasted = append(m.broadcasted, msg)
	return "", nil
}
