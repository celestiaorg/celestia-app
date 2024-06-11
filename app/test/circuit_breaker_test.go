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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestCircuitBreaker(t *testing.T) {
	const (
		granter      = "granter"
		grantee      = "grantee"
		appVersion   = v1.Version
		amountToSend = 1
	)
	var (
	// now        = time.Now()
	// expiration = now.Add(time.Hour)
	)

	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), granter, grantee)
	info := testApp.Info(abci.RequestInfo{})
	require.Equal(t, appVersion, info.AppVersion)

	signer, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, appVersion, user.NewAccount(granter, 1, 0))
	require.NoError(t, err)

	granterAddress := getAddress(t, granter, keyRing)

	// Create a try upgrade transaction.
	tryUpgradeTx := newTryUpgradeTx(t, signer, granterAddress)

	// Verify that the try upgrade transaction can be included in a block.
	header := tmproto.Header{Height: 3, Version: version.Consensus{App: appVersion}}
	ctx := testApp.NewContext(true, header)
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})
	res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: tryUpgradeTx})
	assert.Equal(t, uint32(0x25), res.Code, res.Log)
	assert.Contains(t, res.Log, "message type /celestia.signal.v1.MsgTryUpgrade is not supported in version 1: feature not supported")
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()

	// Create a nested TryUpgrade tx.
	// authorization := authz.NewGenericAuthorization(signaltypes.URLMsgTryUpgrade)
	// msg, err := authz.NewMsgGrant(senderAddress, receiverAddress, authorization, &expiration)
	// require.NoError(t, err)
	// testApp.AuthzKeeper.Grant(ctx, msg)

	// TODO: Verify that the TryUpgrade tx doesn't get executed.
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
