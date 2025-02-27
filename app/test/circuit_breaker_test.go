package app_test

import (
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	v1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	signaltypes "github.com/celestiaorg/celestia-app/v4/x/signal/types"
)

const (
	granter      = "granter"
	grantee      = "grantee"
	appVersion   = v1.Version
	amountToSend = 1
)

var expiration = time.Now().Add(time.Hour)

// TestCircuitBreaker verifies that the circuit breaker prevents a nested Authz
// message that contains a MsgTryUpgrade if the MsgTryUpgrade is not supported
// in the current version.
func TestCircuitBreaker(t *testing.T) { // TODO: we need to pass a find a way to update the app version easily

	// NOTE: the below sections are commented out due to the lack of the MsgVersioningGateKeeper ante handler, this will not be required
	// once the multiplexer is implemented.

	// enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	testApp, keyRing := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), granter, grantee)
	header := tmproto.Header{Time: time.Now(), Height: 2, Version: version.Consensus{App: appVersion}}
	// signer, err := user.NewSigner(keyRing, enc.TxConfig, util.ChainID, appVersion, user.NewAccount(granter, 2, 0))
	// require.NoError(t, err)

	granterAddress := testfactory.GetAddress(keyRing, granter)
	granteeAddress := testfactory.GetAddress(keyRing, grantee)

	authorization := authz.NewGenericAuthorization(signaltypes.URLMsgTryUpgrade)
	msg, err := authz.NewMsgGrant(granterAddress, granteeAddress, authorization, &expiration)
	require.NoError(t, err)
	ctx := testApp.NewContext(true).WithBlockHeader(header)
	_, err = testApp.AuthzKeeper.Grant(ctx, msg)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "/celestia.signal.v1.Msg/TryUpgrade doesn't exist.: invalid type")

	// when the muiliplexer is introduced.

	// _, err = testApp.BeginBlocker(ctx)
	// require.NoError(t, err)

	// tryUpgradeTx := newTryUpgradeTx(t, signer, granterAddress)
	// blockResp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{Txs: [][]byte{tryUpgradeTx}, Height: 2, Time: ctx.BlockTime()})
	// require.NoError(t, err)
	// res := blockResp.TxResults[0]
	// assert.Equal(t, uint32(0x25), res.Code, res.Log)
	// assert.Contains(t, res.Log, "message type /celestia.signal.v1.MsgTryUpgrade is not supported in version 1: feature not supported")

	// nestedTx := newNestedTx(t, signer, granterAddress)
	// blockResp, err = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{Txs: [][]byte{nestedTx}})
	// require.NoError(t, err)
	// res = blockResp.TxResults[0]
	// assert.Equal(t, uint32(0x25), res.Code, res.Log)
	// assert.Contains(t, res.Log, "message type /celestia.signal.v1.MsgTryUpgrade is not supported in version 1: feature not supported")
}

// func newTryUpgradeTx(t *testing.T, signer *user.Signer, senderAddress sdk.AccAddress) coretypes.Tx {
// 	msg := signaltypes.NewMsgTryUpgrade(senderAddress)
// 	options := blobfactory.FeeTxOpts(1e9)

// 	rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, options...)
// 	require.NoError(t, err)

// 	return rawTx
// }

// func newNestedTx(t *testing.T, signer *user.Signer, granterAddress sdk.AccAddress) coretypes.Tx {
// 	innerMsg := signaltypes.NewMsgTryUpgrade(granterAddress)
// 	msg := authz.NewMsgExec(granterAddress, []sdk.Msg{innerMsg})

// 	options := blobfactory.FeeTxOpts(1e9)

// 	rawTx, _, err := signer.CreateTx([]sdk.Msg{&msg}, options...)
// 	require.NoError(t, err)

// 	return rawTx
// }
