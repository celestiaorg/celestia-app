package types_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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
			name: "BlobTx wrapping a TxBody with MsgPayForFibre",
			txBytes: func() []byte {
				blob, err := share.NewV0Blob(testNamespace, []byte("data"))
				require.NoError(t, err)
				innerBody := marshalTxBody(t, fibreMsgAny(t, signer, testNamespace.Bytes(), testCommitment))
				blobTxBytes, err := squaretx.MarshalBlobTx(innerBody, blob)
				require.NoError(t, err)
				return blobTxBytes
			}(),
			wantNil: true,
		},
		{
			name: "MsgPayForFibre with additional message",
			txBytes: marshalTx(t,
				fibreMsgAny(t, signer, testNamespace.Bytes(), testCommitment),
				&codectypes.Any{
					TypeUrl: "/cosmos.bank.v1beta1.MsgSend",
					Value:   []byte("some-value"),
				}),
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

// TestTryParseFibreTxSDKParity asserts that classification agrees with the
// app's actual SDK tx decoder: a decodable tx is classified as fibre exactly
// when it contains a single MsgPayForFibre message. The decoder reads the
// outer TxRaw, where body_bytes is a scalar and a repeated occurrence
// resolves to the last one; classification decoding into the embedded-message
// cosmostx.Tx instead would merge duplicate bodies and disagree with the SDK.
func TestTryParseFibreTxSDKParity(t *testing.T) {
	decoder := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig.TxDecoder()

	signer, err := bech32.EncodeFromBase256("celestia", testSignerRaw)
	require.NoError(t, err)

	fibreAny := fibreMsgAny(t, signer, testNamespace.Bytes(), testCommitment)
	sendAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{FromAddress: signer, ToAddress: signer})
	require.NoError(t, err)
	fibreBody := marshalTxBody(t, fibreAny)
	normalBody := marshalTxBody(t, sendAny)

	tests := []struct {
		name    string
		txBytes []byte
	}{
		{"single fibre body", bodyField(t, fibreBody)},
		{"single normal body", bodyField(t, normalBody)},
		{"fibre and normal messages in one body", marshalTx(t, fibreAny, sendAny)},
		{"duplicate bodies [fibre, normal]", append(bodyField(t, fibreBody), bodyField(t, normalBody)...)},
		{"duplicate bodies [normal, fibre]", append(bodyField(t, normalBody), bodyField(t, fibreBody)...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdkTx, err := decoder(tc.txBytes)
			require.NoError(t, err, "every vector must be decodable so the parity check is not vacuous")
			msgs := sdkTx.GetMsgs()
			_, firstIsPFF := msgs[0].(*fibretypes.MsgPayForFibre)
			wantFibre := len(msgs) == 1 && firstIsPFF

			_, isFibreTx, err := fibretypes.TryParseFibreTx(tc.txBytes)
			require.NoError(t, err)
			require.Equal(t, wantFibre, isFibreTx)
		})
	}
}

// bodyField encodes body as protobuf field 1 (body/body_bytes) with wire type
// 2 by marshalling a TxRaw holding it as body_bytes.
func bodyField(t *testing.T, body []byte) []byte {
	t.Helper()
	encoded, err := (&cosmostx.TxRaw{BodyBytes: body}).Marshal()
	require.NoError(t, err)
	return encoded
}

func marshalTxBody(t *testing.T, msgs ...*codectypes.Any) []byte {
	t.Helper()
	bodyBytes, err := (&cosmostx.TxBody{Messages: msgs}).Marshal()
	require.NoError(t, err)
	return bodyBytes
}

// fibreMsgAny constructs the Any packing of a MsgPayForFibre.
func fibreMsgAny(t *testing.T, signer string, namespace, commitment []byte) *codectypes.Any {
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
	return anyMsg
}

// fibreTxBytes constructs Cosmos SDK Tx proto bytes containing a single
// MsgPayForFibre message.
func fibreTxBytes(t *testing.T, signer string, namespace, commitment []byte) []byte {
	t.Helper()
	return marshalTx(t, fibreMsgAny(t, signer, namespace, commitment))
}

func marshalTx(t *testing.T, msgs ...*codectypes.Any) []byte {
	t.Helper()
	txBytes, err := (&cosmostx.Tx{Body: &cosmostx.TxBody{Messages: msgs}}).Marshal()
	require.NoError(t, err)
	return txBytes
}
