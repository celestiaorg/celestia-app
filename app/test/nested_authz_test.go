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
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestNestedAuthz(t *testing.T) {
	senderAccount := "sender"
	receiverAccount := "receiver"
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), senderAccount, receiverAccount)
	appVersion := uint64(1)
	info := testApp.Info(abci.RequestInfo{})
	require.Equal(t, appVersion, info.AppVersion)
	require.Equal(t, appVersion, testApp.AppVersion())

	signer, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, 1, user.NewAccount(senderAccount, 1, 0))
	require.NoError(t, err)

	rawTx := sendTx(t, keyRing, signer, senderAccount, receiverAccount, 1)

	check := testApp.CheckTx(abci.RequestCheckTx{Type: abci.CheckTxType_New, Tx: rawTx})
	assert.Equal(t, abci.CodeTypeOK, check.Code, check.Log)

	header := tmproto.Header{Version: version.Consensus{App: appVersion}}
	ctx := testApp.NewContext(true, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: rawTx})

	assert.Equal(t, 0, res.Code)
	assert.NotEmpty(t, res.Log)
}

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
