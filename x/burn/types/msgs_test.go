package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgBurn_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("test_signer__________").String()
	validAmount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))

	tests := []struct {
		name    string
		msg     *MsgBurn
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: &MsgBurn{
				Signer: validAddress,
				Amount: validAmount,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: &MsgBurn{
				Signer: "invalid",
				Amount: validAmount,
			},
			wantErr: true,
			errMsg:  "invalid signer",
		},
		{
			name: "wrong denomination",
			msg: &MsgBurn{
				Signer: validAddress,
				Amount: sdk.NewCoin("wrongdenom", math.NewInt(1000)),
			},
			wantErr: true,
			errMsg:  "only utia can be burned",
		},
		{
			name: "zero amount",
			msg: &MsgBurn{
				Signer: validAddress,
				Amount: sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			},
			wantErr: true,
			errMsg:  "positive",
		},
		{
			name: "negative amount",
			msg: &MsgBurn{
				Signer: validAddress,
				Amount: sdk.Coin{Denom: appconsts.BondDenom, Amount: math.NewInt(-100)},
			},
			wantErr: true,
			errMsg:  "positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
