package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
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

func TestCircuitBreaker(t *testing.T) {
	const (
		sender       = "sender"
		receiver     = "receiver"
		appVersion   = v1.Version
		amountToSend = 1
	)

	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), sender, receiver)
	info := testApp.Info(abci.RequestInfo{})
	require.Equal(t, appVersion, info.AppVersion)

	signer, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, appVersion, user.NewAccount(sender, 0, 0))
	require.NoError(t, err)

	rawTx := sendTx(t, keyRing, signer, sender, receiver, amountToSend)

	check := testApp.CheckTx(abci.RequestCheckTx{Type: abci.CheckTxType_New, Tx: rawTx})
	assert.Equal(t, abci.CodeTypeOK, check.Code, check.Log)

	header := tmproto.Header{Version: version.Consensus{App: appVersion}}
	ctx := testApp.NewContext(true, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: rawTx})

	assert.Equal(t, uint32(0), res.Code, res.Log)
}

// func nestedAuthzTx(t *testing.T) coretypes.Tx {
// 	nestedBankSend := authz.NewMsgExec(sdktypes.AccAddress{}, []sdktypes.Msg{&banktypes.MsgSend{}})
// 	return nestedBankSend
// }

func sendTx(t *testing.T, keyRing keyring.Keyring, signer *user.Signer, senderAccount string, receiverAccount string, amount uint64) coretypes.Tx {
	senderAddress := getAddress(t, senderAccount, keyRing)
	receiverAddress := getAddress(t, receiverAccount, keyRing)

	msg := banktypes.NewMsgSend(senderAddress, receiverAddress, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(amount))))
	options := blobfactory.FeeTxOpts(1e9)

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, options...)
	require.NoError(t, err)

	return rawTx
}

func getAddress(t *testing.T, account string, keyRing keyring.Keyring) sdk.AccAddress {
	record, err := keyRing.Key(account)
	require.NoError(t, err)

	address, err := record.GetAddress()
	require.NoError(t, err)

	return address
}
