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
	signaltypes "github.com/celestiaorg/celestia-app/v2/x/signal/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	granter      = "granter"
	grantee      = "grantee"
	appVersion   = v1.Version
	amountToSend = 1
)

var (
	expiration = time.Now().Add(time.Hour)
)

// TestCircuitBreaker verifies that the circuit breaker prevents a nested Authz
// message that contains a MsgTryUpgrade if the MsgTryUpgrade is not supported
// in the current version.
func TestCircuitBreaker(t *testing.T) {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), granter, grantee)
	header := tmproto.Header{Height: 2, Version: version.Consensus{App: appVersion}}

	signer, err := user.NewSigner(keyRing, config.TxConfig, testutil.ChainID, appVersion, user.NewAccount(granter, 1, 0))
	require.NoError(t, err)

	granterAddress := getAddress(t, granter, keyRing)
	granteeAddress := getAddress(t, grantee, keyRing)

	authorization := authz.NewGenericAuthorization(signaltypes.URLMsgTryUpgrade)
	msg, err := authz.NewMsgGrant(granterAddress, granteeAddress, authorization, &expiration)
	require.NoError(t, err)
	ctx := testApp.NewContext(true, header)
	_, err = testApp.AuthzKeeper.Grant(ctx, msg)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "/celestia.signal.v1.Msg/TryUpgrade doesn't exist.: invalid type")

	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{Header: header})

	tryUpgradeTx := newTryUpgradeTx(t, signer, granterAddress)
	res := testApp.DeliverTx(abci.RequestDeliverTx{Tx: tryUpgradeTx})
	assert.Equal(t, uint32(0x25), res.Code, res.Log)
	assert.Contains(t, res.Log, "message type /celestia.signal.v1.MsgTryUpgrade is not supported in version 1: feature not supported")

	nestedTx := newNestedTx(t, signer, granterAddress)
	res = testApp.DeliverTx(abci.RequestDeliverTx{Tx: nestedTx})
	assert.Equal(t, uint32(0x1), res.Code, res.Log)
	assert.Contains(t, res.Log, "circuit breaker disables execution of this message: /celestia.signal.v1.MsgTryUpgrade")
}

func newTryUpgradeTx(t *testing.T, signer *user.Signer, senderAddress sdk.AccAddress) coretypes.Tx {
	msg := signaltypes.NewMsgTryUpgrade(senderAddress)
	options := blobfactory.FeeTxOpts(1e9)

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, options...)
	require.NoError(t, err)

	return rawTx
}

func newNestedTx(t *testing.T, signer *user.Signer, granterAddress sdk.AccAddress) coretypes.Tx {
	innerMsg := signaltypes.NewMsgTryUpgrade(granterAddress)
	msg := authz.NewMsgExec(granterAddress, []sdk.Msg{innerMsg})

	options := blobfactory.FeeTxOpts(1e9)

	rawTx, err := signer.CreateTx([]sdk.Msg{&msg}, options...)
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
