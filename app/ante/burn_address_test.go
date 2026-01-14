package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	burntypes "github.com/celestiaorg/celestia-app/v7/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestBurnAddressDecorator(t *testing.T) {
	decorator := ante.NewBurnAddressDecorator()
	signer := sdk.AccAddress("test_signer__________")

	testCases := []struct {
		name      string
		msg       sdk.Msg
		expectErr bool
	}{
		{
			name: "allow utia to burn address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   burntypes.BurnAddressBech32,
				Amount:      sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))),
			},
			expectErr: false,
		},
		{
			name: "reject non-utia to burn address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   burntypes.BurnAddressBech32,
				Amount:      sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
			},
			expectErr: true,
		},
		{
			name: "allow any denom to non-burn address",
			msg: &banktypes.MsgSend{
				FromAddress: signer.String(),
				ToAddress:   signer.String(),
				Amount:      sdk.NewCoins(sdk.NewCoin("anydenom", math.NewInt(1000))),
			},
			expectErr: false,
		},
		{
			name: "reject multi-send with non-utia to burn address",
			msg: &banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{
					{Address: signer.String(), Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))},
				},
				Outputs: []banktypes.Output{
					{Address: burntypes.BurnAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))},
				},
			},
			expectErr: true,
		},
		{
			name: "allow multi-send with utia to burn address",
			msg: &banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{
					{Address: signer.String(), Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))},
				},
				Outputs: []banktypes.Output{
					{Address: burntypes.BurnAddressBech32, Coins: sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))},
				},
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx := mockTx([]sdk.Msg{tc.msg})
			ctx := sdk.Context{}

			_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
