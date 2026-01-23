package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	_ "github.com/celestiaorg/celestia-app/v7/app/params" // Sets SDK bech32 prefixes
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/feeaddress"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

// feeAddrMockTx implements sdk.Tx for testing FeeAddressDecorator.
type feeAddrMockTx struct {
	msgs []sdk.Msg
}

func (m *feeAddrMockTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *feeAddrMockTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *feeAddrMockTx) ValidateBasic() error                  { return nil }

func feeAddrNextAnteHandler(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

func TestFeeAddressDecorator(t *testing.T) {
	decorator := ante.NewFeeAddressDecorator()
	signer := sdk.AccAddress("test_signer__________")

	// Create MsgExec with nested MsgSend for authz tests
	msgSendNonUtia := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddress.FeeAddressBech32,
		Amount:      sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
	}
	msgSendUtia := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddress.FeeAddressBech32,
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
				ToAddress:   feeaddress.FeeAddressBech32,
				Amount:      sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))),
			},
			expectErr: false,
		},
		{
			name: "reject non-utia to fee address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   feeaddress.FeeAddressBech32,
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
					{Address: feeaddress.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))},
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
					{Address: feeaddress.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))},
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
					{Address: feeaddress.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(500)))},
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
					{Address: feeaddress.FeeAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)))},
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
				ToAddress:   feeaddress.FeeAddressBech32,
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
			tx := &feeAddrMockTx{msgs: []sdk.Msg{tc.msg}}
			ctx := sdk.Context{}

			_, err := decorator.AnteHandle(ctx, tx, false, feeAddrNextAnteHandler)

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

func TestFeeAddressDecoratorDeeplyNestedAuthz(t *testing.T) {
	// Test deeply nested authz - MsgExec containing another MsgExec containing MsgSend
	decorator := ante.NewFeeAddressDecorator()
	signer := sdk.AccAddress("test_signer__________")

	// Create inner MsgSend with non-utia to fee address
	innerMsgSend := &banktypes.MsgSend{
		FromAddress: signer.String(),
		ToAddress:   feeaddress.FeeAddressBech32,
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

	tx := &feeAddrMockTx{msgs: []sdk.Msg{outerMsgExec}}
	ctx := sdk.Context{}

	_, err = decorator.AnteHandle(ctx, tx, false, feeAddrNextAnteHandler)

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
		ToAddress:   feeaddress.FeeAddressBech32,
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

	tx := &feeAddrMockTx{msgs: []sdk.Msg{level3MsgExec}}
	ctx := sdk.Context{}

	_, err = decorator.AnteHandle(ctx, tx, false, feeAddrNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "only utia can be sent to fee address")
}
