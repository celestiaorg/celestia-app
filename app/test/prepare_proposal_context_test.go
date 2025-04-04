package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

// TestTimeInPrepareProposalContext checks for an edge case where the block time
// needs to be included in the sdk.Context that is being used in the
// antehandlers. If a time is not included in the context, then the second
// transaction in this test will always be filtered out, result in vesting
// accounts never being able to spend funds.
func TestTimeInPrepareProposalContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestTimeInPrepareProposalContext test in short mode.")
	}
	sendAccName := "sending"
	vestAccName := "vesting"
	cfg := testnode.DefaultConfig().WithFundedAccounts(sendAccName)
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

	type test struct {
		name    string
		msgFunc func() (msgs []sdk.Msg, signer string)
	}
	tests := []test{
		{
			name: "create continuous vesting account with a start time in the future",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				_, _, err := cctx.Keyring.NewMnemonic(vestAccName, keyring.English, "", "", hd.Secp256k1)
				require.NoError(t, err)
				sendingAccAddr := testfactory.GetAddress(cctx.Keyring, sendAccName)
				vestAccAddr := testfactory.GetAddress(cctx.Keyring, vestAccName)
				msg := vestingtypes.NewMsgCreateVestingAccount(
					sendingAccAddr,
					vestAccAddr,
					sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1000000))),
					time.Now().Unix(),
					time.Now().Add(time.Second*100).Unix(),
					false,
				)
				return []sdk.Msg{msg}, sendAccName
			},
		},
		{
			name: "send funds from the vesting account after it has been created",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				sendingAccAddr := testfactory.GetAddress(cctx.Keyring, sendAccName)
				vestAccAddr := testfactory.GetAddress(cctx.Keyring, vestAccName)
				msg := banktypes.NewMsgSend(
					vestAccAddr,
					sendingAccAddr,
					sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1))),
				)
				return []sdk.Msg{msg}, vestAccName
			},
		},
	}

	// sign and submit the transactions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, enc)
			require.NoError(t, err)
			msgs, _ := tt.msgFunc()
			res, err := txClient.SubmitTx(cctx.GoContext(), msgs, user.SetGasLimit(1000000), user.SetFee(2000))
			require.NoError(t, err)
			serviceClient := sdktx.NewServiceClient(cctx.GRPCClient)
			getTxResp, err := serviceClient.GetTx(cctx.GoContext(), &sdktx.GetTxRequest{Hash: res.TxHash})
			require.NoError(t, err)
			require.NotNil(t, res)
			assert.Equal(t, abci.CodeTypeOK, res.Code, getTxResp.TxResponse.RawLog)
		})
	}
}
