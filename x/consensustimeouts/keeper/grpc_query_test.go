package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	"github.com/stretchr/testify/require"
)

// TestQueryParams_ReturnsCurrent sets non-default params, queries them via the
// gRPC handler, and asserts the response matches the stored value.
func TestQueryParams_ReturnsCurrent(t *testing.T) {
	f := newTestFixture(t)
	want := modifiedParams()
	f.keeper.SetParams(f.ctx, want)

	resp, err := f.keeper.Params(f.ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, want, resp.Params)
}
