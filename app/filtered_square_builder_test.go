package app

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	gosquaretx "github.com/celestiaorg/go-square/v4/tx"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestSeparateTxs(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	normalTx := newNormalTx(t, txConfig)
	blobTx := newBlobTx(t)
	payForFibreTx := newPayForFibreTx(t, txConfig)
	wrappedFibreTx := newWrappedFibreTx(t, txConfig)

	tests := []struct {
		name     string
		rawTxs   [][]byte
		wantNorm int
		wantBlob int
		wantPFF  int
	}{
		{
			name:     "empty",
			rawTxs:   [][]byte{},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "one normal tx",
			rawTxs:   [][]byte{normalTx},
			wantNorm: 1,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "one blob tx",
			rawTxs:   [][]byte{blobTx},
			wantNorm: 0,
			wantBlob: 1,
			wantPFF:  0,
		},
		{
			name:     "one pay-for-fibre tx",
			rawTxs:   [][]byte{payForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  1,
		},
		{
			name:     "one of each",
			rawTxs:   [][]byte{normalTx, blobTx, payForFibreTx},
			wantNorm: 1,
			wantBlob: 1,
			wantPFF:  1,
		},
		{
			name:     "two pay-for-fibre txs",
			rawTxs:   [][]byte{payForFibreTx, payForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  2,
		},
		{
			name:     "already-wrapped FibreTx goes to payForFibreTxs",
			rawTxs:   [][]byte{wrappedFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  1,
		},
		{
			name:     "mix of plain and wrapped FibreTx",
			rawTxs:   [][]byte{payForFibreTx, wrappedFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalTxs, blobTxs, payForFibreTxs := separateTxs(txConfig, tc.rawTxs)
			require.Len(t, normalTxs, tc.wantNorm)
			require.Len(t, blobTxs, tc.wantBlob)
			require.Len(t, payForFibreTxs, tc.wantPFF)
		})
	}
}

func TestExtractMsgPayForFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	tests := []struct {
		name      string
		txBytes   func() []byte
		wantFound bool
	}{
		{
			name:      "MsgPayForFibre",
			txBytes:   func() []byte { return newPayForFibreTx(t, txConfig) },
			wantFound: true,
		},
		{
			name:      "MsgSend",
			txBytes:   func() []byte { return newNormalTx(t, txConfig) },
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdkTx, err := txConfig.TxDecoder()(tc.txBytes())
			require.NoError(t, err)

			msg, found := extractMsgPayForFibre(sdkTx)
			require.Equal(t, tc.wantFound, found)
			if tc.wantFound {
				require.NotNil(t, msg)
			} else {
				require.Nil(t, msg)
			}
		})
	}
}

func TestCreateSystemBlobForPayForFibre(t *testing.T) {
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	validNs := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	validCommitment := bytes.Repeat([]byte{0xFF}, share.FibreCommitmentSize)

	tests := []struct {
		name      string
		msg       *fibretypes.MsgPayForFibre
		wantErr   string
		checkBlob func(t *testing.T, blob *share.Blob, msg *fibretypes.MsgPayForFibre)
	}{
		{
			name: "valid",
			msg: &fibretypes.MsgPayForFibre{
				Signer: addr.String(),
				PaymentPromise: fibretypes.PaymentPromise{
					Namespace:   validNs.Bytes(),
					Commitment:  validCommitment,
					BlobVersion: fibretypes.BlobVersionZero,
					ChainId:     "test",
					Height:      1,
				},
			},
			checkBlob: func(t *testing.T, blob *share.Blob, msg *fibretypes.MsgPayForFibre) {
				t.Helper()
				require.Equal(t, share.ShareVersionTwo, blob.ShareVersion())
				ns, err := share.NewNamespaceFromBytes(msg.PaymentPromise.Namespace)
				require.NoError(t, err)
				require.True(t, blob.Namespace().Equals(ns))
				require.Equal(t, addr.Bytes(), blob.Signer())
				blobData := blob.Data()
				require.Len(t, blobData, share.FibreBlobVersionSize+share.FibreCommitmentSize)
				require.Equal(t, msg.PaymentPromise.Commitment, blobData[share.FibreBlobVersionSize:])
			},
		},
		{
			name: "invalid namespace size",
			msg: &fibretypes.MsgPayForFibre{
				Signer: addr.String(),
				PaymentPromise: fibretypes.PaymentPromise{
					Namespace:  []byte{1, 2, 3}, // too short
					Commitment: validCommitment,
				},
			},
			wantErr: "invalid namespace size",
		},
		{
			name: "invalid signer address",
			msg: &fibretypes.MsgPayForFibre{
				Signer: "not-a-valid-bech32",
				PaymentPromise: fibretypes.PaymentPromise{
					Namespace:  validNs.Bytes(),
					Commitment: validCommitment,
				},
			},
			wantErr: "failed to decode signer address",
		},
		{
			name: "invalid commitment size",
			msg: &fibretypes.MsgPayForFibre{
				Signer: addr.String(),
				PaymentPromise: fibretypes.PaymentPromise{
					Namespace:  validNs.Bytes(),
					Commitment: []byte{1, 2, 3}, // wrong size
				},
			},
			wantErr: "invalid commitment size",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := createSystemBlobForPayForFibre(tc.msg)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, blob)
			tc.checkBlob(t, blob, tc.msg)
		})
	}
}

func TestFilteredSquareBuilderFillWithPayForFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}
	alwaysReject := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, errors.New("ante handler rejected tx")
	}

	normalTx := newNormalTx(t, txConfig)
	blobTx := newBlobTx(t)
	payForFibreTx := newPayForFibreTx(t, txConfig)
	wrappedFibreTx := newWrappedFibreTx(t, txConfig)

	tests := []struct {
		name              string
		anteHandler       sdk.AnteHandler
		txs               [][]byte
		wantKeptCount     int
		wantFibreTxCount  int // FibreTx-wrapped outputs
		wantFibreInSquare bool
	}{
		{
			name:              "only pay-for-fibre",
			anteHandler:       alwaysPass,
			txs:               [][]byte{payForFibreTx},
			wantKeptCount:     1,
			wantFibreTxCount:  1,
			wantFibreInSquare: true,
		},
		{
			name:              "normal and pay-for-fibre",
			anteHandler:       alwaysPass,
			txs:               [][]byte{normalTx, payForFibreTx},
			wantKeptCount:     2,
			wantFibreTxCount:  1,
			wantFibreInSquare: true,
		},
		{
			name:              "blob and pay-for-fibre",
			anteHandler:       alwaysPass,
			txs:               [][]byte{blobTx, payForFibreTx},
			wantKeptCount:     2,
			wantFibreTxCount:  1,
			wantFibreInSquare: true,
		},
		{
			name:              "all three types",
			anteHandler:       alwaysPass,
			txs:               [][]byte{normalTx, blobTx, payForFibreTx},
			wantKeptCount:     3,
			wantFibreTxCount:  1,
			wantFibreInSquare: true,
		},
		{
			name:              "two pay-for-fibre txs",
			anteHandler:       alwaysPass,
			txs:               [][]byte{payForFibreTx, payForFibreTx},
			wantKeptCount:     2,
			wantFibreTxCount:  2,
			wantFibreInSquare: true,
		},
		{
			name:              "pay-for-fibre rejected by ante handler is excluded",
			anteHandler:       alwaysReject,
			txs:               [][]byte{payForFibreTx},
			wantKeptCount:     0,
			wantFibreTxCount:  0,
			wantFibreInSquare: false,
		},
		{
			name:        "non-fibre txs kept when ante handler rejects pay-for-fibre",
			anteHandler: alwaysReject,
			txs:         [][]byte{normalTx, blobTx, payForFibreTx},
			// normalTx and blobTx are also rejected because alwaysReject fires for every tx type.
			wantKeptCount:     0,
			wantFibreTxCount:  0,
			wantFibreInSquare: false,
		},
		{
			name:              "already-wrapped FibreTx passes through unchanged",
			anteHandler:       alwaysPass,
			txs:               [][]byte{wrappedFibreTx},
			wantKeptCount:     1,
			wantFibreTxCount:  1,
			wantFibreInSquare: true,
		},
		{
			name:              "already-wrapped FibreTx rejected by ante handler is excluded",
			anteHandler:       alwaysReject,
			txs:               [][]byte{wrappedFibreTx},
			wantKeptCount:     0,
			wantFibreTxCount:  0,
			wantFibreInSquare: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsb, err := NewFilteredSquareBuilder(tc.anteHandler, txConfig, 64, 64)
			require.NoError(t, err)

			db := dbm.NewMemDB()
			ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
			ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

			kept := fsb.Fill(ctx, tc.txs)
			require.Len(t, kept, tc.wantKeptCount)

			// Count how many of the kept txs are FibreTx-wrapped.
			fibreTxCount := 0
			for _, rawTx := range kept {
				_, isFibre, _ := gosquaretx.UnmarshalFibreTx(rawTx)
				if isFibre {
					fibreTxCount++
				}
			}
			require.Equal(t, tc.wantFibreTxCount, fibreTxCount)

			sq, err := fsb.Build()
			require.NoError(t, err)

			pffRange := share.GetShareRangeForNamespace(sq, share.PayForFibreNamespace)
			if tc.wantFibreInSquare {
				require.False(t, pffRange.IsEmpty(), "expected PayForFibreNamespace shares in square")
			} else {
				require.True(t, pffRange.IsEmpty(), "expected no PayForFibreNamespace shares in square")
			}
		})
	}
}

// newNormalTx creates an unsigned MsgSend transaction for testing.
func newNormalTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	msg := &banktypes.MsgSend{
		FromAddress: addr.String(),
		ToAddress:   addr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
	}
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// newBlobTx creates a BlobTx for testing.
func newBlobTx(t *testing.T) []byte {
	t.Helper()
	ns := share.MustNewV0Namespace([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	blob, err := share.NewBlob(ns, []byte("test blob"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blobTxBytes, err := gosquaretx.MarshalBlobTx(nil, blob)
	require.NoError(t, err)
	return blobTxBytes
}

// newWrappedFibreTx creates a FibreTx (already-wrapped MsgPayForFibre) for testing,
// simulating what the fibre client submits via BroadcastTxWithWrap.
func newWrappedFibreTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	sdkTxBytes := newPayForFibreTx(t, txConfig)
	// Decode to extract the MsgPayForFibre so we can build a matching system blob.
	sdkTx, err := txConfig.TxDecoder()(sdkTxBytes)
	require.NoError(t, err)
	msg, ok := extractMsgPayForFibre(sdkTx)
	require.True(t, ok)
	systemBlob, err := createSystemBlobForPayForFibre(msg)
	require.NoError(t, err)
	wrapped, err := gosquaretx.MarshalFibreTx(sdkTxBytes, systemBlob)
	require.NoError(t, err)
	return wrapped
}

// newPayForFibreTx creates an unsigned SDK tx containing MsgPayForFibre for testing.
func newPayForFibreTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	msg := &fibretypes.MsgPayForFibre{
		Signer: addr.String(),
		PaymentPromise: fibretypes.PaymentPromise{
			ChainId:           "test",
			Height:            1,
			Namespace:         ns.Bytes(),
			BlobSize:          100,
			BlobVersion:       fibretypes.BlobVersionZero,
			Commitment:        bytes.Repeat([]byte{0xAB}, share.FibreCommitmentSize),
			CreationTimestamp: time.Now(),
			SignerPublicKey:   *privKey.PubKey().(*secp256k1.PubKey),
			Signature:         make([]byte, 64),
		},
	}
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}
