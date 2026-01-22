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

func TestFeeForwardTerminatorRejectsUserSubmittedTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create CheckTx context - this simulates a user submitting the tx
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()) // isCheckTx = true

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

func TestFeeForwardTerminatorRejectsReCheckTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create ReCheckTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()).WithIsReCheckTx(true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

func TestFeeForwardTerminatorValidatesSingleDenom(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Multiple denoms in fee - should be rejected
	fee := sdk.NewCoins(
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
		sdk.NewCoin("otherdenom", math.NewInt(500)),
	)
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward tx requires exactly one fee coin")
}

func TestFeeForwardTerminatorRejectsWrongDenom(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Wrong denom - should be rejected
	fee := sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward tx requires utia denom")
}

func TestFeeForwardTerminatorSuccess(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	newCtx, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.NoError(t, err)
	// Verify fee was sent to fee collector
	require.Equal(t, fee, bankKeeper.sentToModule[authtypes.FeeCollectorName])
	// Verify GetFeeForwardAmount returns the correct fee
	feeFromCtx, ok := feeaddresstypes.GetFeeForwardAmount(newCtx)
	require.True(t, ok, "GetFeeForwardAmount should return fee")
	require.Equal(t, fee, feeFromCtx, "fee from context should match tx fee")
}

// TestFeeForwardTerminatorRejectsSimulation verifies that the FeeForwardTerminatorDecorator
// rejects fee forward transactions in simulation mode.
func TestFeeForwardTerminatorRejectsSimulation(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context but pass simulate=true
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, true, nextAnteHandler) // simulate = true

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

// mockNonFeeTx implements sdk.Tx but NOT sdk.FeeTx for testing error path.
type mockNonFeeTx struct {
	msgs []sdk.Msg
}

func (m *mockNonFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *mockNonFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *mockNonFeeTx) ValidateBasic() error                  { return nil }

// TestFeeForwardTerminatorRejectsNonFeeTx verifies that the FeeForwardTerminatorDecorator
// rejects transactions that don't implement sdk.FeeTx.
func TestFeeForwardTerminatorRejectsNonFeeTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	tx := &mockNonFeeTx{msgs: []sdk.Msg{msg}}

	// Create DeliverTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "tx must implement FeeTx")
}

// mockBankKeeperWithError implements ante.FeeForwardBankKeeper and returns an error.
type mockBankKeeperWithError struct {
	err error
}

func (m *mockBankKeeperWithError) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	return m.err
}

func TestFeeForwardTerminatorBankTransferFailure(t *testing.T) {
	// Test that bank transfer failure is properly handled
	bankKeeper := &mockBankKeeperWithError{err: sdkerrors.ErrInsufficientFunds}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to deduct fee from fee address")
}

func TestFeeForwardTerminatorZeroFeeRejected(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Zero fee should be rejected
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: sdk.Coins{}, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward tx requires exactly one fee coin")
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
