package ante_test

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func TestFeeAddressDecorator(t *testing.T) {
	decorator := ante.NewFeeAddressDecorator()
	signer := sdk.AccAddress("test_signer__________")

	// Create MsgExec with nested MsgSend for authz tests
	msgSendNonUtia := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddresstypes.FeeAddressBech32,
		Amount:      sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
	}
	msgSendUtia := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddresstypes.FeeAddressBech32,
		Amount:      sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))),
	}

	anyMsgNonUtia, _ := codectypes.NewAnyWithValue(msgSendNonUtia)
	anyMsgUtia, _ := codectypes.NewAnyWithValue(msgSendUtia)

	// Create MsgExec with nested MsgTransfer for authz tests
	msgTransferNonUtia := &ibctransfertypes.MsgTransfer{
		SourcePort:    "transfer",
		SourceChannel: "channel-0",
		Token:         sdk.NewCoin("wrongdenom", math.NewInt(1000)),
		Sender:        signer.String(),
		Receiver:      feeaddresstypes.FeeAddressBech32,
	}
	anyMsgTransferNonUtia, _ := codectypes.NewAnyWithValue(msgTransferNonUtia)

	// Another address for multi-output tests
	otherAddr := sdk.AccAddress("other_address________")

	testCases := []struct {
		name           string
		msg            sdk.Msg
		expectErr      bool
		expectErrMatch string // If non-empty, error must contain this substring
	}{
		{
			name: "allow utia to fee address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   feeaddresstypes.FeeAddressBech32,
				Amount:      sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))),
			},
			expectErr: false,
		},
		{
			name: "reject non-utia to fee address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   feeaddresstypes.FeeAddressBech32,
				Amount:      sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
			},
			expectErr:      true,
			expectErrMatch: "only utia can be sent to fee address, got wrongdenom",
		},
		{
			name: "allow any denom to non-fee address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   signer.String(),
				Amount:      sdk.NewCoins(sdk.NewCoin("anydenom", math.NewInt(1000))),
			},
			expectErr: false,
		},
		{
			name: "reject multi-send with non-utia to fee address",
			msg: &banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{
					{Address: signer.String(), Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))},
				},
				Outputs: []banktypes.Output{
					{Address: feeaddresstypes.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))},
				},
			},
			expectErr: true,
		},
		{
			name: "allow multi-send with utia to fee address",
			msg: &banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{
					{Address: signer.String(), Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))},
				},
				Outputs: []banktypes.Output{
					{Address: feeaddresstypes.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))},
				},
			},
			expectErr: false,
		},
		// IBC MsgTransfer tests
		{
			name: "reject IBC transfer of non-utia to fee address",
			msg: &ibctransfertypes.MsgTransfer{
				SourcePort:    "transfer",
				SourceChannel: "channel-0",
				Token:         sdk.NewCoin("wrongdenom", math.NewInt(1000)),
				Sender:        signer.String(),
				Receiver:      feeaddresstypes.FeeAddressBech32,
			},
			expectErr: true,
		},
		{
			name: "allow IBC transfer of utia to fee address",
			msg: &ibctransfertypes.MsgTransfer{
				SourcePort:    "transfer",
				SourceChannel: "channel-0",
				Token:         sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
				Sender:        signer.String(),
				Receiver:      feeaddresstypes.FeeAddressBech32,
			},
			expectErr: false,
		},
		// authz MsgExec tests - validates nested messages
		{
			name: "reject authz MsgExec with nested non-utia to fee address",
			msg: &authz.MsgExec{
				Grantee: signer.String(),
				Msgs:    []*codectypes.Any{anyMsgNonUtia},
			},
			expectErr: true,
		},
		{
			name: "allow authz MsgExec with nested utia to fee address",
			msg: &authz.MsgExec{
				Grantee: signer.String(),
				Msgs:    []*codectypes.Any{anyMsgUtia},
			},
			expectErr: false,
		},
		{
			name: "reject authz MsgExec with nested MsgTransfer non-utia to fee address",
			msg: &authz.MsgExec{
				Grantee: signer.String(),
				Msgs:    []*codectypes.Any{anyMsgTransferNonUtia},
			},
			expectErr: true,
		},
		// MsgMultiSend with multiple outputs
		{
			name: "reject multi-send with multiple outputs where one is non-utia to fee address",
			msg: &banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{
					{Address: signer.String(), Coins: sdk.NewCoins(
						sdk.NewCoin("wrongdenom", math.NewInt(500)),
						sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)),
					)},
				},
				Outputs: []banktypes.Output{
					{Address: feeaddresstypes.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(500)))},
					{Address: otherAddr.String(), Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)))},
				},
			},
			expectErr: true,
		},
		{
			name: "allow multi-send with multiple outputs all valid",
			msg: &banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{
					{Address: signer.String(), Coins: sdk.NewCoins(
						sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)),
						sdk.NewCoin("otherdenom", math.NewInt(500)),
					)},
				},
				Outputs: []banktypes.Output{
					{Address: feeaddresstypes.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)))},
					{Address: otherAddr.String(), Coins: sdk.NewCoins(sdk.NewCoin("otherdenom", math.NewInt(500)))},
				},
			},
			expectErr: false,
		},
		// Multi-denom test
		{
			name: "reject mixed denoms to fee address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   feeaddresstypes.FeeAddressBech32,
				Amount: sdk.NewCoins(
					sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
					sdk.NewCoin("wrongdenom", math.NewInt(500)),
				),
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx := mockTx([]sdk.Msg{tc.msg})
			ctx := sdk.Context{}

			_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

			if tc.expectErr {
				require.Error(t, err)
				if tc.expectErrMatch != "" {
					require.ErrorContains(t, err, tc.expectErrMatch)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// mockFeeTx implements sdk.Tx and sdk.FeeTx for testing FeeForwardDecorator.
type mockFeeTx struct {
	msgs []sdk.Msg
	fee  sdk.Coins
	gas  uint64
}

func (m *mockFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *mockFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *mockFeeTx) ValidateBasic() error                  { return nil }
func (m *mockFeeTx) GetFee() sdk.Coins                     { return m.fee }
func (m *mockFeeTx) GetGas() uint64                        { return m.gas }
func (m *mockFeeTx) FeePayer() []byte                      { return nil }
func (m *mockFeeTx) FeeGranter() []byte                    { return nil }

// mockBankKeeper implements ante.FeeForwardBankKeeper for testing.
type mockBankKeeper struct {
	sentToModule map[string]sdk.Coins
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.sentToModule == nil {
		m.sentToModule = make(map[string]sdk.Coins)
	}
	m.sentToModule[recipientModule] = amt
	return nil
}

func TestFeeForwardDecoratorRejectsUserSubmittedTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create CheckTx context - this simulates a user submitting the tx
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()) // isCheckTx = true

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

func TestFeeForwardDecoratorRejectsReCheckTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create ReCheckTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()).WithIsReCheckTx(true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

func TestFeeForwardDecoratorValidatesSingleDenom(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Multiple denoms in fee - should be rejected
	fee := sdk.NewCoins(
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
		sdk.NewCoin("otherdenom", math.NewInt(500)),
	)
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context with fee forward flag set (simulates EarlyFeeForwardDetector)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithValue(ante.FeeForwardContextKey{}, true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "must have exactly one fee coin")
}

func TestFeeForwardDecoratorRejectsWrongDenom(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Wrong denom - should be rejected
	fee := sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context with fee forward flag set (simulates EarlyFeeForwardDetector)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithValue(ante.FeeForwardContextKey{}, true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "must use utia")
}

func TestFeeForwardDecoratorSuccess(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context with fee forward flag set (simulates EarlyFeeForwardDetector)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithValue(ante.FeeForwardContextKey{}, true)

	newCtx, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.NoError(t, err)
	// Verify fee was sent to fee collector
	require.Equal(t, fee, bankKeeper.sentToModule[authtypes.FeeCollectorName])
	// Verify context flag is still set
	require.True(t, ante.IsFeeForwardTx(newCtx))
}

// TestFeeForwardDecoratorRequiresContextFlag verifies that the FeeForwardDecorator
// rejects fee forward transactions in DeliverTx mode if the context flag was not
// set by EarlyFeeForwardDetector. This is a defense-in-depth assertion.
func TestFeeForwardDecoratorRequiresContextFlag(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context WITHOUT fee forward flag (simulates misconfigured ante chain)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward context flag not set")
	require.ErrorContains(t, err, "EarlyFeeForwardDetector")
}

// mockBankKeeperWithError implements ante.FeeForwardBankKeeper and returns an error.
type mockBankKeeperWithError struct {
	err error
}

func (m *mockBankKeeperWithError) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	return m.err
}

func TestFeeForwardDecoratorBankTransferFailure(t *testing.T) {
	// Test that bank transfer failure is properly handled
	bankKeeper := &mockBankKeeperWithError{err: sdkerrors.ErrInsufficientFunds}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context with fee forward flag set (simulates EarlyFeeForwardDetector)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithValue(ante.FeeForwardContextKey{}, true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to deduct fee from fee address")
}

func TestFeeForwardDecoratorZeroFeeRejected(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Zero fee should be rejected
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: sdk.Coins{}, gas: 50000}

	// Create DeliverTx context with fee forward flag set (simulates EarlyFeeForwardDetector)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithValue(ante.FeeForwardContextKey{}, true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "must have non-zero fee")
}

func TestFeeAddressDecoratorDeeplyNestedAuthz(t *testing.T) {
	// Test deeply nested authz - MsgExec containing another MsgExec containing MsgSend
	decorator := ante.NewFeeAddressDecorator()
	signer := sdk.AccAddress("test_signer__________")

	// Create inner MsgSend with non-utia to fee address
	innerMsgSend := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddresstypes.FeeAddressBech32,
		Amount:      sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
	}
	anyInnerMsgSend, err := codectypes.NewAnyWithValue(innerMsgSend)
	require.NoError(t, err)

	// Create inner MsgExec wrapping the MsgSend
	innerMsgExec := &authz.MsgExec{
		Grantee: signer.String(),
		Msgs:    []*codectypes.Any{anyInnerMsgSend},
	}
	anyInnerMsgExec, err := codectypes.NewAnyWithValue(innerMsgExec)
	require.NoError(t, err)

	// Create outer MsgExec wrapping the inner MsgExec (two levels of nesting)
	outerMsgExec := &authz.MsgExec{
		Grantee: signer.String(),
		Msgs:    []*codectypes.Any{anyInnerMsgExec},
	}

	tx := mockTx([]sdk.Msg{outerMsgExec})
	ctx := sdk.Context{}

	_, err = decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "only utia can be sent to fee address")
}

func TestFeeAddressDecoratorTripleNestedAuthz(t *testing.T) {
	// Test triple nested authz - MsgExec -> MsgExec -> MsgExec -> MsgSend
	decorator := ante.NewFeeAddressDecorator()
	signer := sdk.AccAddress("test_signer__________")

	// Create innermost MsgSend with non-utia to fee address
	innermostMsgSend := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddresstypes.FeeAddressBech32,
		Amount:      sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
	}
	anyInnermostMsgSend, err := codectypes.NewAnyWithValue(innermostMsgSend)
	require.NoError(t, err)

	// Level 1: MsgExec wrapping MsgSend
	level1MsgExec := &authz.MsgExec{
		Grantee: signer.String(),
		Msgs:    []*codectypes.Any{anyInnermostMsgSend},
	}
	anyLevel1MsgExec, err := codectypes.NewAnyWithValue(level1MsgExec)
	require.NoError(t, err)

	// Level 2: MsgExec wrapping Level 1
	level2MsgExec := &authz.MsgExec{
		Grantee: signer.String(),
		Msgs:    []*codectypes.Any{anyLevel1MsgExec},
	}
	anyLevel2MsgExec, err := codectypes.NewAnyWithValue(level2MsgExec)
	require.NoError(t, err)

	// Level 3: MsgExec wrapping Level 2 (outermost)
	level3MsgExec := &authz.MsgExec{
		Grantee: signer.String(),
		Msgs:    []*codectypes.Any{anyLevel2MsgExec},
	}

	tx := mockTx([]sdk.Msg{level3MsgExec})
	ctx := sdk.Context{}

	_, err = decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "only utia can be sent to fee address")
}
