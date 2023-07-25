package gasmonitor_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/tools/gasmonitor"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMonitoredGasMeter(t *testing.T) {
	// create a test send transaction
	const (
		acc  = "signer"
		acc2 = "signer2"
	)
	kr := testfactory.GenerateKeyring(acc)
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	tx := util.SendTxWithManualSequence(t, nil, kr, acc, acc2, 1000000, "chainid", 1, 1)
	ctx := sdk.NewContext(nil, types.Header{}, false, nil)
	sdkTx, err := ecfg.TxConfig.TxDecoder()(tx)
	require.NoError(t, err)

	gasMon, _ := gasmonitor.NewGasConsumptionTrace(ctx, sdkTx)
	amount, desc := uint64(100), "gas"
	gasMon.ConsumeGas(amount, desc)
	require.Equal(t, gasmonitor.Reading{Description: desc, Amount: amount}, gasMon.Readings[0])
}
