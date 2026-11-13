package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

// protoTx is a tx whose proto form may be missing fields.
type protoTx struct {
	sdk.Tx
	tx *txtypes.Tx
}

func (p protoTx) GetProtoTx() *txtypes.Tx { return p.tx }

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
		{"tx that is not a protobuf tx", msgsTx{}, 2, true},
		{"nil proto tx", protoTx{}, 2, true},
		{"proto tx without AuthInfo", protoTx{tx: &txtypes.Tx{}}, 2, true},
		{"proto tx without a fee", protoTx{tx: &txtypes.Tx{AuthInfo: &txtypes.AuthInfo{}}}, 2, true},
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
