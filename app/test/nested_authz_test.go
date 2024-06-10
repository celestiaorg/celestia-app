package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestNestedAuthz(t *testing.T) {
	senderAccount := "sender"
	receiverAccount := "receiver"
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), senderAccount, receiverAccount)
	require.Equal(t, uint64(1), testApp.AppVersion())

	// records, err := keyRing.List()
	// require.NoError(t, err)
	// require.Len(t, records, 2)
	// sender := records[0]
	// reciever := records[1]

	signer, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, 1, user.NewAccount(senderAccount, 0, 0))
	require.NoError(t, err)

	rawTx := sendTx(t, keyRing, signer, senderAccount, receiverAccount, 1)

	resp := testApp.CheckTx(abci.RequestCheckTx{Type: abci.CheckTxType_New, Tx: rawTx})
	assert.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)

	// tx := nestedAuthzTx(t)
	// tx := sendTx(t)

	// ctx := testApp.NewContext(true, tmproto.Header{Height: 4})
	// testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: tmproto.Header{}})
	// res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: []byte{}})
	// assert.Equal(t, 0, res.Code)
	// assert.Empty(t, res.Log)

	// _, err = testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
	// 	Subspace: blobstreamtypes.ModuleName,
	// 	Key:      string(blobstreamtypes.ParamsStoreKeyDataCommitmentWindow),
	// })
}

// func sendTx(t *testing.T) Tx {
// 	return banktypes.NewMsgSend(
// 		sdktypes.AccAddress{},
// 		sdktypes.AccAddress{},
// 		sdktypes.NewCoins(sdktypes.NewInt64Coin("stake", 100)),
// 	)
// }

// func nestedAuthzTx(t *testing.T) coretypes.Tx {
// 	nestedBankSend := authz.NewMsgExec(sdktypes.AccAddress{}, []sdktypes.Msg{&banktypes.MsgSend{}})
// 	return nestedBankSend
// }

func getAddress(account string, kr keyring.Keyring) sdk.AccAddress {
	rec, err := kr.Key(account)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	return addr
}

func sendTx(t *testing.T, keyRing keyring.Keyring, signer *user.Signer, senderAccount string, receiverAccount string, amount uint64) coretypes.Tx {
	senderAddress := getAddress(senderAccount, keyRing)
	receiverAddress := getAddress(receiverAccount, keyRing)

	msg := banktypes.NewMsgSend(senderAddress, receiverAddress, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(amount))))
	options := blobfactory.FeeTxOpts(1e9)

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, options...)
	require.NoError(t, err)

	return rawTx
}
