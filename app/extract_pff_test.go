package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestValidatePayForFibreTxShape(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := enc.TxConfig

	tests := []struct {
		name    string
		txBytes []byte
		wantErr error
	}{
		{
			name:    "normal tx is valid",
			txBytes: newNormalTx(t, txConfig),
		},
		{
			name:    "two-message SDK tx without pay-for-fibre is valid",
			txBytes: newMultiMsgSendTx(t, txConfig, 2),
		},
		{
			name:    "ten-message SDK tx without pay-for-fibre is valid",
			txBytes: newMultiMsgSendTx(t, txConfig, 10),
		},
		{
			name:    "single pay-for-fibre tx is valid",
			txBytes: blobfactory.UnsignedPayForFibreTx(t, txConfig),
		},
		{
			name:    "pay-for-fibre mixed with MsgSend is invalid",
			txBytes: newMixedPayForFibreTx(t, txConfig),
			wantErr: apperr.ErrInvalidPayForFibreTx,
		},
		{
			name:    "two pay-for-fibre messages are invalid",
			txBytes: newMultiPayForFibreTx(t, txConfig),
			wantErr: apperr.ErrInvalidPayForFibreTx,
		},
		{
			name:    "pay-for-fibre promising a reserved namespace is invalid",
			txBytes: newPayForFibreTxWithNamespace(t, txConfig, share.TxNamespace.Bytes()),
			wantErr: apperr.ErrInvalidPayForFibreTx,
		},
		{
			name:    "pay-for-fibre promising the parity namespace is invalid",
			txBytes: newPayForFibreTxWithNamespace(t, txConfig, share.ParitySharesNamespace.Bytes()),
			wantErr: apperr.ErrInvalidPayForFibreTx,
		},
		{
			name:    "pay-for-fibre promising a malformed namespace is invalid",
			txBytes: newPayForFibreTxWithNamespace(t, txConfig, []byte{0x01, 0x02}),
			wantErr: apperr.ErrInvalidPayForFibreTx,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePayForFibreTxShape(decodeTx(t, txConfig, tc.txBytes))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPayForFibreMsg(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := enc.TxConfig

	tests := []struct {
		name    string
		txBytes []byte
		wantOK  bool
	}{
		{
			name:    "single pay-for-fibre tx",
			txBytes: blobfactory.UnsignedPayForFibreTx(t, txConfig),
			wantOK:  true,
		},
		{
			name:    "normal tx",
			txBytes: newNormalTx(t, txConfig),
			wantOK:  false,
		},
		{
			name:    "multi-message SDK tx",
			txBytes: newMultiMsgSendTx(t, txConfig, 3),
			wantOK:  false,
		},
		{
			name:    "two pay-for-fibre messages",
			txBytes: newMultiPayForFibreTx(t, txConfig),
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pff, ok := payForFibreMsg(decodeTx(t, txConfig, tc.txBytes))
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.NotNil(t, pff)
			}
		})
	}
}

func decodeTx(t *testing.T, txConfig client.TxConfig, txBytes []byte) sdk.Tx {
	t.Helper()
	tx, err := txConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	return tx
}

// newPayForFibreTxWithNamespace creates an unsigned pay-for-fibre tx whose
// payment promise names the provided namespace.
func newPayForFibreTxWithNamespace(t *testing.T, txConfig client.TxConfig, namespace []byte) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	msg := blobfactory.NewMsgPayForFibre(t, privKey.PubKey().(*secp256k1.PubKey), "test")
	msg.PaymentPromise.Namespace = namespace
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// newMultiMsgSendTx creates an unsigned tx with msgCount MsgSend messages.
func newMultiMsgSendTx(t *testing.T, txConfig client.TxConfig, msgCount int) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	msgs := make([]sdk.Msg, msgCount)
	for i := range msgs {
		msgs[i] = &banktypes.MsgSend{
			FromAddress: addr.String(),
			ToAddress:   addr.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
		}
	}
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msgs...))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}
