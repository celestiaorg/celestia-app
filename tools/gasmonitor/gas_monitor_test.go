package gasmonitor

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMonitoredGasMeter(t *testing.T) {
	ctx := sdk.NewContext(nil, types.Header{}, false, nil)
	gasMon, _ := NewMonitoredGasMeter(ctx)
	amount, desc := uint64(100), "gas"
	gasMon.ConsumeGas(amount, desc)
	require.Equal(t, Reading{Description: desc, Amount: amount}, gasMon.Readings[0])
}
