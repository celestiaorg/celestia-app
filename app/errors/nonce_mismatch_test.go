package errors_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/v4/app/errors"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	blob "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

// This will detect any changes to the DeductFeeDecorator which may cause a
// different error message that does not match the regexp.
func TestNonceMismatchIntegration(t *testing.T) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	minGasPrice, err := sdk.ParseDecCoins(fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, params.BondDenom))
	require.NoError(t, err)
	ctx := testApp.NewContext(true).WithMinGasPrices(minGasPrice)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)

	// set the sequence to an incorrect value
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()+1))
	require.NoError(t, err)

	b, err := blob.NewV0Blob(share.RandomNamespace(), []byte("hello world"))
	require.NoError(t, err)

	msg, err := blob.NewMsgPayForBlobs(signer.Account(account).Address().String(), appconsts.LatestVersion, b)
	require.NoError(t, err)

	rawTx, _, err := signer.CreateTx([]sdk.Msg{msg})
	require.NoError(t, err)

	decorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, encCfg.TxConfig.SignModeHandler())
	anteHandler := sdk.ChainAnteDecorators(decorator)

	sdkTx, err := signer.DecodeTx(rawTx)
	require.NoError(t, err)

	// We set simulate to true here to bypass having to initialize the
	// accounts public key.
	_, err = anteHandler(ctx, sdkTx, true)
	require.True(t, apperr.IsNonceMismatch(err), err)
	expectedNonce, err := apperr.ParseNonceMismatch(err)
	require.NoError(t, err)
	require.EqualValues(t, 0, expectedNonce, err)
}
