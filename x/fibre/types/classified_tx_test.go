package types_test

import (
	"bytes"
	"testing"

	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

var (
	testNamespace  = share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	testCommitment = bytes.Repeat([]byte{0xFF}, share.FibreCommitmentSize)
	testSignerRaw  = bytes.Repeat([]byte{0xAA}, share.SignerSize)
)

func TestTryParseFibreTx(t *testing.T) {
	signer, err := bech32.EncodeFromBase256("celestia", testSignerRaw)
	require.NoError(t, err)

	tests := []struct {
		name    string
		txBytes []byte
		wantNil bool
		wantErr bool
	}{
		{
			name:    "random bytes",
			txBytes: []byte("not-a-cosmos-tx"),
			wantNil: true,
		},
		{
			name:    "empty bytes",
			txBytes: []byte{},
			wantNil: true,
		},
		{
			name:    "nil bytes",
			txBytes: nil,
			wantNil: true,
		},
		{
			name:    "valid MsgPayForFibre tx",
			txBytes: fibreTxBytes(t, signer, testNamespace.Bytes(), testCommitment),
		},
		{
			name: "plain SDK tx with different message type",
			txBytes: marshalTx(t, &codectypes.Any{
				TypeUrl: "/cosmos.bank.v1beta1.MsgSend",
				Value:   []byte("some-value"),
			}),
			wantNil: true,
		},
		{
			name: "SDK tx with empty body",
			txBytes: func() []byte {
				txBytes, err := (&cosmostx.Tx{Body: &cosmostx.TxBody{}}).Marshal()
				require.NoError(t, err)
				return txBytes
			}(),
			wantNil: true,
		},
		{
			name: "BlobTx bytes",
			txBytes: func() []byte {
				blob, err := share.NewV0Blob(testNamespace, []byte("data"))
				require.NoError(t, err)
				blobTxBytes, err := squaretx.MarshalBlobTx([]byte("inner-tx"), blob)
				require.NoError(t, err)
				return blobTxBytes
			}(),
			wantNil: true,
		},
		{
			name: "MsgPayForFibre with corrupted inner message",
			txBytes: marshalTx(t, &codectypes.Any{
				TypeUrl: fibretypes.MsgPayForFibreTypeURL,
				Value:   []byte{0xFF, 0xFF, 0xFF},
			}),
			wantNil: true,
			wantErr: true,
		},
		{
			name:    "MsgPayForFibre with invalid signer address",
			txBytes: fibreTxBytes(t, "not-a-bech32-address", testNamespace.Bytes(), testCommitment),
			wantNil: true,
			wantErr: true,
		},
		{
			name:    "MsgPayForFibre with invalid namespace",
			txBytes: fibreTxBytes(t, signer, []byte{0x1}, testCommitment),
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fibreTx, isFibreTx, err := fibretypes.TryParseFibreTx(tc.txBytes)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantNil {
				require.Nil(t, fibreTx)
				// Malformed fibre txs are still detected as fibre txs.
				require.Equal(t, tc.wantErr, isFibreTx)
				return
			}
			require.True(t, isFibreTx)
			require.NotNil(t, fibreTx)
			require.Equal(t, tc.txBytes, fibreTx.Tx)
			require.Equal(t, testNamespace, fibreTx.SystemBlob.Namespace())
			require.Equal(t, testSignerRaw, fibreTx.SystemBlob.Signer())
			require.Equal(t, share.ShareVersionTwo, fibreTx.SystemBlob.ShareVersion())
		})
	}
}

func TestClassifyTxs(t *testing.T) {
	signer, err := bech32.EncodeFromBase256("celestia", testSignerRaw)
	require.NoError(t, err)
	normalTx := []byte("normal-tx")
	fibreTx := fibreTxBytes(t, signer, testNamespace.Bytes(), testCommitment)

	t.Run("classifies normal and fibre txs", func(t *testing.T) {
		classified, err := fibretypes.ClassifyTxs([][]byte{normalTx, fibreTx})
		require.NoError(t, err)
		require.Len(t, classified, 2)

		require.Equal(t, normalTx, classified[0].Bytes)
		require.Nil(t, classified[0].FibreTx)

		require.Equal(t, fibreTx, classified[1].Bytes)
		require.NotNil(t, classified[1].FibreTx)
		require.Equal(t, testNamespace, classified[1].FibreTx.SystemBlob.Namespace())
	})

	t.Run("returns an error for a malformed fibre tx", func(t *testing.T) {
		malformed := fibreTxBytes(t, "not-a-bech32-address", testNamespace.Bytes(), testCommitment)
		_, err := fibretypes.ClassifyTxs([][]byte{normalTx, malformed})
		require.ErrorContains(t, err, "index 1")
	})
}

// fibreTxBytes constructs Cosmos SDK Tx proto bytes containing a single
// MsgPayForFibre message.
func fibreTxBytes(t *testing.T, signer string, namespace, commitment []byte) []byte {
	t.Helper()
	msg := &fibretypes.MsgPayForFibre{
		Signer: signer,
		PaymentPromise: fibretypes.PaymentPromise{
			Namespace:   namespace,
			BlobVersion: fibretypes.BlobVersionZero,
			Commitment:  commitment,
		},
	}
	anyMsg, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)
	return marshalTx(t, anyMsg)
}

func marshalTx(t *testing.T, msgs ...*codectypes.Any) []byte {
	t.Helper()
	txBytes, err := (&cosmostx.Tx{Body: &cosmostx.TxBody{Messages: msgs}}).Marshal()
	require.NoError(t, err)
	return txBytes
}
