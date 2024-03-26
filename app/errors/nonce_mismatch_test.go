package errors_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/app/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// This will detect any changes to the DeductFeeDecorator which may cause a
// different error message that does not match the regexp.
func TestNonceMismatchIntegration(t *testing.T) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	minGasPrice, err := sdk.ParseDecCoins(fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, app.BondDenom))
	require.NoError(t, err)
	ctx := testApp.NewContext(true, tmproto.Header{}).WithMinGasPrices(minGasPrice)
	signer := blob.NewKeyringSigner(kr, account, testutil.ChainID)
	// set the sequence to an incorrect value
	signer.SetSequence(2)
	builder := signer.NewTxBuilder()

	address, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	b, err := blob.NewBlob(namespace.RandomNamespace(), []byte("hello world"), 0)
	require.NoError(t, err)

	pfb, err := blob.NewMsgPayForBlobs(address.String(), b)
	require.NoError(t, err, address)

	tx, err := signer.BuildSignedTx(builder, pfb)
	require.NoError(t, err)

	decorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, encCfg.TxConfig.SignModeHandler())
	anteHandler := sdk.ChainAnteDecorators(decorator)

	// We set simulate to true here to bypass having to initialize the
	// accounts public key.
	_, err = anteHandler(ctx, tx, true)
	require.True(t, apperr.IsNonceMismatch(err), err)
	expectedNonce, err := apperr.ParseNonceMismatch(err)
	require.NoError(t, err)
	require.EqualValues(t, 0, expectedNonce, err)
}
