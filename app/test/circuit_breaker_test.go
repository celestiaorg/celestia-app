package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
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
		a            = "a"
		b            = "b"
		c            = "c"
		appVersion   = v1.Version
		amountToSend = 1
	)
	var (
		now        = time.Now()
		expiration = now.Add(time.Hour)
	)

	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), a, b, c)
	info := testApp.Info(abci.RequestInfo{})
	require.Equal(t, appVersion, info.AppVersion)

	aSigner, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, appVersion, user.NewAccount(a, 1, 0))
	require.NoError(t, err)
	bSigner, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, appVersion, user.NewAccount(b, 1, 0))
	require.NoError(t, err)

	aAddress := getAddress(t, a, keyRing)
	bAddress := getAddress(t, b, keyRing)
	cAddress := getAddress(t, c, keyRing)
	sendTx := newSendTx(t, aSigner, aAddress, bAddress, amountToSend)

	header := tmproto.Header{Height: 2, Version: version.Consensus{App: appVersion}}
	ctx := testApp.NewContext(true, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: sendTx})
	assert.Equal(t, uint32(0), res.Code, res.Log)
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()

	authorization := banktypes.NewSendAuthorization(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(10))))
	msg, err := authz.NewMsgGrant(aAddress, bAddress, authorization, &expiration)
	require.NoError(t, err)
	testApp.AuthzKeeper.Grant(ctx, msg)
	newSendTx := newSendTx(t, bSigner, aAddress, cAddress, 5)
	header = tmproto.Header{Height: 3, Version: version.Consensus{App: appVersion}}
	ctx = testApp.NewContext(true, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res = testApp.DeliverTx(abci.RequestDeliverTx{Tx: newSendTx})
	assert.Equal(t, uint32(0), res.Code, res.Log)
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()
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

func getAddress(t *testing.T, account string, keyRing keyring.Keyring) sdk.AccAddress {
	record, err := keyRing.Key(account)
	require.NoError(t, err)

	address, err := record.GetAddress()
	require.NoError(t, err)

	return address
}
