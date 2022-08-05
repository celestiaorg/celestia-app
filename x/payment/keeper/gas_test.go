package keeper

import (
	"testing"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestPayForDataGas(t *testing.T) {
	app := simapp.Setup(t, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	k := Keeper{}

	messageSize := uint64(100)

	msg := types.MsgPayForData{
		MessageSize: messageSize,
	}

	_, err := k.PayForData(sdk.WrapSDKContext(ctx), &msg)
	require.NoError(t, err)
	require.Equal(t, messageSize, ctx.GasMeter().GasConsumed())
}
