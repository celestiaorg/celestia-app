package payment_test

import (
	"testing"

	keepertest "github.com/celestiaorg/celestia-app/testutil/keeper"
	"github.com/celestiaorg/celestia-app/testutil/nullify"
	"github.com/celestiaorg/celestia-app/x/payment"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	k, ctx := keepertest.PaymentKeeper(t)
	payment.InitGenesis(ctx, *k, genesisState)
	got := payment.ExportGenesis(ctx, *k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)
}
