package e2e_test

import (
	"encoding/json"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v10/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestFibreWithdrawalLifecycle covers the escrow-exit path
func TestFibreWithdrawalLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fibre withdrawal e2e test in short mode")
	}

	const withdrawalDelay = 5 * time.Second
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(fibre.DefaultKeyName).
		WithDelayedPrecommitTimeout(500 * time.Millisecond).
		WithModifiers(setFibreWithdrawalDelay(ecfg.Codec, withdrawalDelay))

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	ctx := cctx.GoContext()
	_, err := cctx.WaitForHeight(1)
	require.NoError(t, err)

	txClient, err := user.SetupTxClient(ctx, cctx.Keyring, cctx.GRPCClient, ecfg, user.WithDefaultAccount(fibre.DefaultKeyName))
	require.NoError(t, err)
	signer := txClient.DefaultAddress().String()

	fibreQ := fibretypes.NewQueryClient(cctx.GRPCClient)
	bankQ := banktypes.NewQueryClient(cctx.GRPCClient)

	bankBalance := func() sdk.Coin {
		resp, err := bankQ.Balance(ctx, &banktypes.QueryBalanceRequest{Address: signer, Denom: appconsts.BondDenom})
		require.NoError(t, err)
		return *resp.Balance
	}
	escrow := func() *fibretypes.EscrowAccount {
		resp, err := fibreQ.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{Signer: signer})
		require.NoError(t, err)
		require.True(t, resp.Found)
		return resp.EscrowAccount
	}
	pendingWithdrawals := func() []fibretypes.Withdrawal {
		resp, err := fibreQ.Withdrawals(ctx, &fibretypes.QueryWithdrawalsRequest{Signer: signer})
		require.NoError(t, err)
		return resp.Withdrawals
	}

	deposit := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(50_000_000))
	withdraw := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(20_000_000))

	depositResp, err := txClient.SubmitTx(ctx,
		[]sdk.Msg{&fibretypes.MsgDepositToEscrow{Signer: signer, Amount: deposit}},
		user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), depositResp.Code)

	acc := escrow()
	require.Equal(t, deposit, acc.Balance)
	require.Equal(t, deposit, acc.AvailableBalance)

	reqResp, err := txClient.SubmitTx(ctx,
		[]sdk.Msg{&fibretypes.MsgRequestWithdrawal{Signer: signer, Amount: withdraw}},
		user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), reqResp.Code)

	acc = escrow()
	require.Equal(t, deposit, acc.Balance, "total balance stays locked until the withdrawal executes")
	require.Equal(t, deposit.Sub(withdraw), acc.AvailableBalance, "available balance drops by the requested amount")

	pending := pendingWithdrawals()
	require.Len(t, pending, 1, "one pending withdrawal should be recorded")
	require.Equal(t, withdraw, pending[0].Amount)

	balanceBeforeExec := bankBalance()

	require.Eventually(t, func() bool {
		return len(pendingWithdrawals()) == 0
	}, 60*time.Second, 500*time.Millisecond, "withdrawal should be auto-executed after the delay")

	acc = escrow()
	require.Equal(t, deposit.Sub(withdraw), acc.Balance, "total balance should drop by the withdrawn amount after execution")
	require.Equal(t, deposit.Sub(withdraw), acc.AvailableBalance)

	require.Equal(t, balanceBeforeExec.Add(withdraw), bankBalance(),
		"withdrawn funds should return to the signer's bank account")
}

// setFibreWithdrawalDelay overrides the fibre module's withdrawal delay in genesis
func setFibreWithdrawalDelay(cdc codec.Codec, delay time.Duration) genesis.Modifier {
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
		gs := fibretypes.DefaultGenesis()
		gs.Params.WithdrawalDelay = delay
		state[fibretypes.ModuleName] = cdc.MustMarshalJSON(gs)
		return state
	}
}
