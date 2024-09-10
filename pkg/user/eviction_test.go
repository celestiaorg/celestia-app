package user_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestEvictions2(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	defaultTmConfig := testnode.DefaultTendermintConfig()
	defaultTmConfig.Mempool.TTLDuration = 1 * time.Nanosecond
	testnodeConfig := testnode.DefaultConfig().
		WithTendermintConfig(defaultTmConfig).
		WithFundedAccounts("a", "b", "c").
		WithAppCreator(testnode.CustomAppCreator("0utia"))
	ctx, _, _ := testnode.NewNetwork(t, testnodeConfig)
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)
	txClient, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, encCfg, user.WithGasMultiplier(1.2))
	require.NoError(t, err)

	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)

	// Keep submitting the transaction until we get the eviction error
	sender := txClient.Signer().Account(txClient.DefaultAccountName())
	msg := bank.NewMsgSend(sender.Address(), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 10)))
	var seqBeforeEviction uint64
	// loop five times until the tx is evicted
	for i := 0; i < 5; i++ {
		seqBeforeEviction = sender.Sequence()
		resp, err := txClient.BroadcastTx(ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
		require.NoError(t, err)
		_, err = txClient.ConfirmTx(ctx.GoContext(), resp.TxHash)
		if err != nil {
			if err.Error() == "tx was evicted from the mempool" {
				break
			}
		}
	}

	seqAfterEviction := sender.Sequence()
	require.Equal(t, seqBeforeEviction, seqAfterEviction)

	// Increase ttl again
	testnodeConfig.TmConfig.Mempool.TTLDuration = 120 * time.Second
	// reinitialise the network
	// newCtx, _, _ := testnode.NewNetwork(t, testnodeConfig)

	// Resubmit with the same sender
	// resp, err := txClient.BroadcastTx(newCtx.GoContext(), []sdk.Msg{msg}, fee, gas)
	// require.NoError(t, err)
	// confirmTxResp, err := txClient.ConfirmTx(newCtx.GoContext(), resp.TxHash)
	// require.NoError(t, err)
	// require.Equal(t, abci.CodeTypeOK, confirmTxResp.Code)
	// seqAfterSuccessfulTx := sender.Sequence()
	// require.Equal(t, seqAfterSuccessfulTx, seqAfterEviction+1)
	// require.True(t, wasRemovedFromTxTracker(resp.TxHash, txClient))
}
