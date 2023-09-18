package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// TestTimeInPrepareProposalContext checks for an edge case where the block time
// needs to be included in the sdk.Context that is being used in the
// antehandlers. If a time is not included in the context, then the second
// transaction in this test will always be filtered out, result in vesting
// accounts never being able to spend funds. The test ensures that the time is
// being used correctly by first sending fund to a vesting account, then
// attempting to send an amount that is expected to pass, then sends an amount
// over the spendable amount which is expected to fail.
func TestTimeInPrepareProposalContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestTimeInPrepareProposalContext test in short mode.")
	}
	accounts := make([]string, 35)
	for i := 0; i < len(accounts); i++ {
		accounts[i] = tmrand.Str(9)
	}
	// use a network with roughly 1 second blocks to allow for this test to query
	cfg := testnode.DefaultConfig().WithAccounts(accounts).WithTimeoutCommit(time.Second)
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	vestAccName := "vesting"

	type test struct {
		name         string
		msgFunc      func() (msgs []sdk.Msg, signer string)
		expectedCode uint32
	}
	tests := []test{
		{
			name: "create continuous vesting account with a start time in the future",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				_, _, err := cctx.Keyring.NewMnemonic(vestAccName, keyring.English, "", "", hd.Secp256k1)
				require.NoError(t, err)
				sendAcc := accounts[0]
				sendingAccAddr := testfactory.GetAddress(cctx.Keyring, sendAcc)
				vestAccAddr := testfactory.GetAddress(cctx.Keyring, vestAccName)
				msg := vestingtypes.NewMsgCreateVestingAccount(
					sendingAccAddr,
					vestAccAddr,
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1_000_000_000))),
					time.Now().Unix(),
					time.Now().Add(time.Second*30).Unix(),
					false,
				)
				return []sdk.Msg{msg}, sendAcc
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "send funds from the vesting account after it has been created",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				sendAcc := accounts[1]
				sendingAccAddr := testfactory.GetAddress(cctx.Keyring, sendAcc)
				vestAccAddr := testfactory.GetAddress(cctx.Keyring, vestAccName)

				spendable := querySpendableBalance(t, cctx, vestAccAddr)
				require.True(t, spendable.IsGTE(sdk.NewCoin(app.BondDenom, sdk.NewInt(1))))

				// send all of the spendable coins except for 1 utia for the
				// fee. Combined with the below test case that should fail, this
				// ensures that the correct time is being used.
				sendAmount := spendable.SubAmount(sdk.NewInt(1))

				msg := banktypes.NewMsgSend(
					vestAccAddr,
					sendingAccAddr,
					sdk.NewCoins(sendAmount),
				)
				return []sdk.Msg{msg}, vestAccName
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "attempt to send slightly too much funds from the vesting account",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				sendAcc := accounts[1]
				sendingAccAddr := testfactory.GetAddress(cctx.Keyring, sendAcc)
				vestAccAddr := testfactory.GetAddress(cctx.Keyring, vestAccName)

				spendable := querySpendableBalance(t, cctx, vestAccAddr)
				require.True(t, spendable.IsGTE(sdk.NewCoin(app.BondDenom, sdk.NewInt(1))))

				// attempt to spend more than the spendable amount, which should
				// be expected to fail if the correct time is being used
				sendAmount := spendable.AddAmount(sdk.NewInt(1_000_000_000))

				msg := banktypes.NewMsgSend(
					vestAccAddr,
					sendingAccAddr,
					sdk.NewCoins(sendAmount),
				)
				return []sdk.Msg{msg}, vestAccName
			},
			expectedCode: sdkerrors.ErrInsufficientFunds.ABCICode(),
		},
	}

	// sign and submit the transactions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, account := tt.msgFunc()
			addr := testfactory.GetAddress(cctx.Keyring, account)
			signer, err := user.SetupSigner(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, addr, ecfg)
			require.NoError(t, err)
			res, err := signer.SubmitTx(cctx.GoContext(), msgs, user.SetGasLimit(1000000), user.SetFee(1))
			if tt.expectedCode != abci.CodeTypeOK {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, res)
			assert.Equal(t, tt.expectedCode, res.Code, res.RawLog)
		})
	}
}

func querySpendableBalance(t *testing.T, cctx testnode.Context, address sdk.AccAddress) (c sdk.Coin) {
	res, err := banktypes.NewQueryClient(cctx.GRPCClient).SpendableBalances(cctx.GoContext(), &banktypes.QuerySpendableBalancesRequest{
		Address: address.String(),
	})
	require.NoError(t, err)
	return res.Balances[0]
}
