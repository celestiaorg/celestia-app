package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// TestCheckTxChargesGasPerSDKMessage checks that a tx pays gas for every
// message it counts against MaxSDKMessages beyond the first. Without the charge,
// one cheap tx whose messages fail at execution can take the whole per-block
// SDK message budget and keep every other SDK tx out of the block.
func TestCheckTxChargesGasPerSDKMessage(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := []string{"a", "b"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	seedICAHostChannel(t, testApp)

	// Enough gas for everything but the per-message charge.
	const gasLimit = 1_000_000

	t.Run("MsgExec wrapping empty messages", func(t *testing.T) {
		fetchedAcc := testutil.DirectQueryAccount(testApp, testfactory.GetAddress(kr, accounts[0]))
		signer := createSigner(t, kr, accounts[0], enc.TxConfig, fetchedAcc.GetAccountNumber())
		addr := signer.Account(accounts[0]).Address()

		// Six MsgExec with 99 empty MsgSends each weigh MaxSDKMessages.
		msgs := make([]sdk.Msg, 6)
		for i := range msgs {
			inner := make([]sdk.Msg, 99)
			for j := range inner {
				inner[j] = &banktypes.MsgSend{}
			}
			exec := authz.NewMsgExec(addr, inner)
			msgs[i] = &exec
		}
		rawTx, _, err := signer.CreateTx(msgs, user.SetGasLimitAndGasPrice(gasLimit, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{Type: abci.CheckTxType_New, Tx: rawTx})
		require.NoError(t, err)
		require.Equal(t, sdkerrors.ErrOutOfGas.ABCICode(), resp.Code, resp.Log)
	})

	t.Run("ICA packet with junk entries", func(t *testing.T) {
		fetchedAcc := testutil.DirectQueryAccount(testApp, testfactory.GetAddress(kr, accounts[1]))
		signer := createSigner(t, kr, accounts[1], enc.TxConfig, fetchedAcc.GetAccountNumber())
		addr := signer.Account(accounts[1]).Address()

		// Each entry is a few bytes, yet counts as one SDK message.
		entries := make([]*codectypes.Any, appconsts.MaxSDKMessages-1)
		for i := range entries {
			entries[i] = &codectypes.Any{TypeUrl: "x"}
		}
		payload, err := enc.Codec.Marshal(&icatypes.CosmosTx{Messages: entries})
		require.NoError(t, err)
		data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: payload}
		packet := channeltypes.NewPacket(data.GetBytes(), 1, "icacontroller-test", "channel-0", icatypes.HostPortID, icaChannelID, clienttypes.NewHeight(1, 1000000), 0)
		msg := channeltypes.NewMsgRecvPacket(packet, []byte("proof"), clienttypes.NewHeight(1, 1), addr.String())

		rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimitAndGasPrice(gasLimit, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{Type: abci.CheckTxType_New, Tx: rawTx})
		require.NoError(t, err)
		require.Equal(t, sdkerrors.ErrOutOfGas.ABCICode(), resp.Code, resp.Log)
	})
}
