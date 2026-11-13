package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateSDKMessageGas(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	withGas := func(gas uint64) sdk.Tx {
		b := txConfig.NewTxBuilder()
		b.SetGasLimit(gas)
		return b.GetTx()
	}

	testCases := []struct {
		name     string
		tx       sdk.Tx
		msgCount int
		wantErr  bool
	}{
		{"one message needs no gas", withGas(0), 1, false},
		{"two messages at the floor", withGas(gasPerAdditionalSDKMessage), 2, false},
		{"two messages below the floor", withGas(gasPerAdditionalSDKMessage - 1), 2, true},
		{"many messages at the floor", withGas(599 * gasPerAdditionalSDKMessage), 600, false},
		{"many messages below the floor", withGas(599*gasPerAdditionalSDKMessage - 1), 600, true},
		{"tx without a fee", msgsTx{}, 2, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSDKMessageGas(tc.tx, tc.msgCount)
			if tc.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrOutOfGas)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
