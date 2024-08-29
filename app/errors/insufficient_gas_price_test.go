package errors_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/v3/app/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// This will detect any changes to the DeductFeeDecorator which may cause a
// different error message that does not match the regexp.
func TestInsufficientMinGasPriceIntegration(t *testing.T) {
	var (
		gasLimit  uint64 = 1_000_000
		feeAmount uint64 = 10
		gasPrice         = float64(feeAmount) / float64(gasLimit)
	)
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	minGasPrice, err := sdk.ParseDecCoins(fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, app.BondDenom))
	require.NoError(t, err)
	ctx := testApp.NewContext(true, tmproto.Header{}).WithMinGasPrices(minGasPrice)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(t, err)

	b, err := blob.NewV0Blob(share.RandomNamespace(), []byte("hello world"))
	require.NoError(t, err)

	msg, err := blob.NewMsgPayForBlobs(signer.Account(account).Address().String(), appconsts.LatestVersion, b)
	require.NoError(t, err)

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(gasLimit), user.SetFee(feeAmount))
	require.NoError(t, err)

	decorator := ante.NewDeductFeeDecorator(testApp.AccountKeeper, testApp.BankKeeper, testApp.FeeGrantKeeper, nil)
	anteHandler := sdk.ChainAnteDecorators(decorator)

	sdkTx, err := signer.DecodeTx(rawTx)
	require.NoError(t, err)

	_, err = anteHandler(ctx, sdkTx, false)
	require.True(t, apperr.IsInsufficientMinGasPrice(err))
	actualGasPrice, err := apperr.ParseInsufficientMinGasPrice(err, gasPrice, gasLimit)
	require.NoError(t, err)
	require.Equal(t, appconsts.DefaultMinGasPrice, actualGasPrice, err)
}

func TestInsufficientMinGasPriceTable(t *testing.T) {
	testCases := []struct {
		name                         string
		err                          error
		inputGasPrice                float64
		inputGasLimit                uint64
		isInsufficientMinGasPriceErr bool
		expectParsingError           bool
		expectedGasPrice             float64
	}{
		{
			name:                         "nil error",
			err:                          nil,
			isInsufficientMinGasPriceErr: false,
		},
		{
			name:                         "not insufficient fee error",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFee, "not enough gas to pay for blobs (minimum: 1000000, got: 100000)"),
			isInsufficientMinGasPriceErr: false,
		},
		{
			name:                         "not insufficient fee error 2",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFunds, "not enough gas to pay for blobs (got: 1000000, required: 100000)"),
			isInsufficientMinGasPriceErr: false,
		},
		{
			name:                         "insufficient fee error",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFee, "insufficient fees; got: 10utia required: 100utia"),
			inputGasPrice:                0.01,
			expectedGasPrice:             0.1,
			isInsufficientMinGasPriceErr: true,
		},
		{
			name:                         "insufficient fee error with zero gas price",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFee, "insufficient fees; got: 0utia required: 100utia"),
			inputGasPrice:                0,
			inputGasLimit:                100,
			expectedGasPrice:             1,
			isInsufficientMinGasPriceErr: true,
		},
		{
			name:                         "insufficient fee error with zero gas price and zero gas limit",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFee, "insufficient fees; got: 0utia required: 100utia"),
			inputGasPrice:                0,
			inputGasLimit:                0,
			isInsufficientMinGasPriceErr: true,
			expectParsingError:           true,
		},
		{
			name:                         "incorrectly formatted error",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFee, "insufficient fees; got: 0uatom required: 100uatom"),
			isInsufficientMinGasPriceErr: false,
		},
		{
			name:                         "error with zero required gas price",
			err:                          errors.Wrap(sdkerrors.ErrInsufficientFee, "insufficient fees; got: 10utia required: 0utia"),
			isInsufficientMinGasPriceErr: true,
			expectParsingError:           true,
		},
		{
			name:                         "error with extra wrapping",
			err:                          errors.Wrap(errors.Wrap(sdkerrors.ErrInsufficientFee, "insufficient fees; got: 10utia required: 100utia"), "extra wrapping"),
			inputGasPrice:                0.01,
			expectedGasPrice:             0.1,
			isInsufficientMinGasPriceErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.isInsufficientMinGasPriceErr, apperr.IsInsufficientMinGasPrice(tc.err))
			actualGasPrice, err := apperr.ParseInsufficientMinGasPrice(tc.err, tc.inputGasPrice, tc.inputGasLimit)
			if tc.expectParsingError {
				require.Error(t, err)
				require.Zero(t, actualGasPrice)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedGasPrice, actualGasPrice)
			}
		})
	}
}
