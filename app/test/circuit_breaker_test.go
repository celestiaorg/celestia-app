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
	signaltypes "github.com/celestiaorg/celestia-app/v2/x/signal/types"
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
	var (
	// now        = time.Now()
	// expiration = now.Add(time.Hour)
	)

	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), sender, receiver)
	info := testApp.Info(abci.RequestInfo{})
	require.Equal(t, appVersion, info.AppVersion)

	signer, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, appVersion, user.NewAccount(sender, 1, 0))
	require.NoError(t, err)

	senderAddress := getAddress(t, sender, keyRing)
	receiverAddress := getAddress(t, receiver, keyRing)

	// Create a sendTx from sender to receiver.
	sendTx := newSendTx(t, signer, senderAddress, receiverAddress, amountToSend)

	// Verify that the sendTx can be included in a block.
	header := tmproto.Header{Height: 2, Version: version.Consensus{App: appVersion}}
	ctx := testApp.NewContext(false, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: sendTx})
	assert.Equal(t, uint32(0), res.Code, res.Log)
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()

	// Create a TryUpgrade tryUpgradeTx.
	tryUpgradeTx := newTryUpgradeTx(t, signer, senderAddress)

	// Verify that the TryUpgrade tx can be included in a block
	header = tmproto.Header{Height: 3, Version: version.Consensus{App: appVersion}}
	ctx = testApp.NewContext(false, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res = testApp.DeliverTx(abci.RequestDeliverTx{Tx: tryUpgradeTx})
	assert.Equal(t, uint32(0), res.Code, res.Log)
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()

	// Create a nested TryUpgrade tx.
	// authorization := authz.NewGenericAuthorization(signaltypes.URLMsgTryUpgrade)
	// msg, err := authz.NewMsgGrant(senderAddress, receiverAddress, authorization, &expiration)
	// require.NoError(t, err)
	// testApp.AuthzKeeper.Grant(ctx, msg)

	// TODO: Verify that the TryUpgrade tx doesn't get executed.
}

// func nestedAuthzTx(t *testing.T) coretypes.Tx {
// 	nestedBankSend := authz.NewMsgExec(sdktypes.AccAddress{}, []sdktypes.Msg{&banktypes.MsgSend{}})
// 	return nestedBankSend
// }

func newSendTx(t *testing.T, signer *user.Signer, senderAddress sdk.AccAddress, receiverAddress sdk.AccAddress, amount uint64) coretypes.Tx {
	msg := banktypes.NewMsgSend(senderAddress, receiverAddress, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(amount))))
	options := blobfactory.FeeTxOpts(1e9)

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, options...)
	require.NoError(t, err)

	return rawTx
}

func newTryUpgradeTx(t *testing.T, signer *user.Signer, senderAddress sdk.AccAddress) coretypes.Tx {
	msg := signaltypes.NewMsgTryUpgrade(senderAddress)
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
