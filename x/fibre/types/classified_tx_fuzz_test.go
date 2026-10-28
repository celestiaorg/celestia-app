package types

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// payForFibreTxSeeds returns tx bytes in the shapes the proposal walk sees:
// a well formed pay-for-fibre tx, one with a malformed message, one with a
// second message, a send tx, and the fibre tx wrapped as a blob tx.
func payForFibreTxSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	pubKey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	signer := sdk.AccAddress(pubKey.Address())
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	msg := &MsgPayForFibre{
		Signer: signer.String(),
		PaymentPromise: PaymentPromise{
			ChainId:           "fuzz-chain",
			Height:            1,
			Namespace:         ns.Bytes(),
			BlobSize:          100,
			BlobVersion:       BlobVersionZero,
			Commitment:        bytes.Repeat([]byte{0xab}, share.FibreCommitmentSize),
			SignerPublicKey:   *pubKey,
			CreationTimestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Signature:         make([]byte, PromiseSignatureSize),
		},
		ValidatorSignatures: [][]byte{make([]byte, 64)},
	}
	require.NoError(tb, msg.ValidateBasic())
	pffAny, err := codectypes.NewAnyWithValue(msg)
	require.NoError(tb, err)
	sendAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{FromAddress: signer.String(), ToAddress: signer.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin("utia", 1))})
	require.NoError(tb, err)

	rawTx := func(msgs ...*codectypes.Any) []byte {
		bodyBytes, err := (&cosmostx.TxBody{Messages: msgs}).Marshal()
		require.NoError(tb, err)
		authInfoBytes, err := (&cosmostx.AuthInfo{Fee: &cosmostx.Fee{GasLimit: 100_000}}).Marshal()
		require.NoError(tb, err)
		out, err := (&cosmostx.TxRaw{BodyBytes: bodyBytes, AuthInfoBytes: authInfoBytes, Signatures: [][]byte{make([]byte, 64)}}).Marshal()
		require.NoError(tb, err)
		return out
	}
	pff := rawTx(pffAny)
	blob, err := share.NewBlob(ns, []byte("data"), share.ShareVersionZero, nil)
	require.NoError(tb, err)
	blobWrapped, err := blobtx.MarshalBlobTx(pff, blob)
	require.NoError(tb, err)

	return [][]byte{
		pff,
		rawTx(&codectypes.Any{TypeUrl: MsgPayForFibreTypeURL, Value: []byte{0xff}}),
		rawTx(pffAny, sendAny),
		rawTx(sendAny),
		blobWrapped,
		{0x0a, 0x00},
		{},
	}
}

// checkPayForFibreTx runs the pay-for-fibre classifiers on raw and checks they
// agree. None may panic. A message that parses is run through the stateless
// checks CheckTx and ProcessProposal apply to it.
func checkPayForFibreTx(t *testing.T, raw []byte) {
	t.Helper()
	msg, isPFF, err := ParsePayForFibreTx(raw)
	require.Equal(t, isPFF && err == nil, IsPayForFibreTx(raw), "IsPayForFibreTx disagrees with ParsePayForFibreTx")
	if !isPFF {
		require.NoError(t, err)
		require.Nil(t, msg)
	}

	fibreTx, isFibreTx, tryErr := TryParseFibreTx(raw)
	require.Equal(t, isPFF, isFibreTx, "TryParseFibreTx disagrees with ParsePayForFibreTx")
	if err != nil {
		require.Error(t, tryErr, "TryParseFibreTx accepted a tx ParsePayForFibreTx rejects")
	}

	classified, classifyErr := ClassifyTxs([][]byte{raw})
	if tryErr != nil {
		require.Error(t, classifyErr, "ClassifyTxs accepted a tx TryParseFibreTx rejects")
	} else {
		require.NoError(t, classifyErr)
		require.Len(t, classified, 1)
	}
	if isFibreTx && tryErr == nil {
		require.NotNil(t, fibreTx.SystemBlob)
		require.Equal(t, raw, fibreTx.Tx)
		systemBlob, err := msg.SystemBlob()
		require.NoError(t, err)
		require.Equal(t, systemBlob, fibreTx.SystemBlob)
	}

	if msg == nil {
		return
	}
	_ = msg.ValidateBasic()
	_ = msg.PaymentPromise.ValidateBasic()
	_, _ = msg.SigCacheKey()
	_ = EstimateGasForPayForFibre(msg.PaymentPromise.BlobSize)
	_ = EstimateGasForPayForFibreSignatureVerification(uint64(len(msg.ValidatorSignatures)))
	_ = PaymentAmount(msg.PaymentPromise.BlobSize)
}

func TestPayForFibreTxSeeds(t *testing.T) {
	seeds := payForFibreTxSeeds(t)
	require.True(t, IsPayForFibreTx(seeds[0]), "the first seed must be a well formed pay-for-fibre tx")
	for _, raw := range seeds {
		checkPayForFibreTx(t, raw)
	}
}

// FuzzParsePayForFibreTx mutates pay-for-fibre and ordinary tx bytes and runs
// the classifiers that CheckTx, PrepareProposal and ProcessProposal use to
// find pay-for-fibre txs and synthesize their system blobs.
func FuzzParsePayForFibreTx(f *testing.F) {
	for _, raw := range payForFibreTxSeeds(f) {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > appconsts.MaxTxSize {
			return
		}
		checkPayForFibreTx(t, raw)
	})
}
