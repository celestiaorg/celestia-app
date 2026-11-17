package app

import (
	"context"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRejectProofQueries(t *testing.T) {
	var called bool
	next := func(context.Context, *abci.RequestQuery) (*abci.ResponseQuery, error) {
		called = true
		return &abci.ResponseQuery{Value: []byte("ok")}, nil
	}
	queryFn := rejectProofQueries(next)

	for _, path := range []string{
		"custom/txInclusionProof/0",
		"/custom/shareInclusionProof/0/1",
	} {
		t.Run(path, func(t *testing.T) {
			called = false
			res, err := queryFn(context.Background(), &abci.RequestQuery{Path: path, Data: []byte{1}})
			require.Error(t, err)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Nil(t, res)
			require.False(t, called, "proof query must not reach the app")
		})
	}

	t.Run("other paths pass through", func(t *testing.T) {
		called = false
		res, err := queryFn(context.Background(), &abci.RequestQuery{Path: "custom/bank/balance"})
		require.NoError(t, err)
		require.Equal(t, []byte("ok"), res.Value)
		require.True(t, called)
	})

	t.Run("nil request passes through", func(t *testing.T) {
		called = false
		_, err := queryFn(context.Background(), nil)
		require.NoError(t, err)
		require.True(t, called)
	})
}
