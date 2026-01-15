package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"
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
