package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// pffMsgCounter decodes raw with the SDK decoder and reports whether the tx
// is a single pay-for-fibre message; decoded is false when the SDK rejects raw
// or it is a blob tx.
type pffMsgCounter func(raw []byte) (single, decoded bool)

func newPFFMsgCounter() pffMsgCounter {
	decoder := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig.TxDecoder()
	return func(raw []byte) (bool, bool) {
		if _, isBlob, _ := blobtx.UnmarshalBlobTx(raw); isBlob {
			return false, false
		}
		sdkTx, err := decoder(raw)
		if err != nil {
			return false, false
		}
		return fibretypes.PayForFibreMsg(sdkTx) != nil, true
	}
}

// pffDecodeCandidateParity pins the fast path against the two slower
// classifiers. ProcessProposal decodes a tx up front only when the fast path
// recognises it, so a pay-for-fibre tx the fast path misses would take the
// non-fibre path of the walk. The fast path must be a superset of
// IsPayForFibreTx and of what the SDK decoder reports as a single-message
// pay-for-fibre tx, and it must never upgrade a decodable tx of another shape.
func pffDecodeCandidateParity(t *testing.T, count pffMsgCounter, raw []byte) {
	t.Helper()
	if len(raw) > appconsts.MaxTxSize {
		return
	}
	candidate := pffDecodeCandidate(raw)
	if fibretypes.IsPayForFibreTx(raw) {
		require.True(t, candidate, "IsPayForFibreTx accepts a tx the fast path rejects")
	}
	single, decoded := count(raw)
	if !decoded {
		return
	}
	if single {
		require.True(t, candidate, "the SDK decoder reports a single pay-for-fibre message the fast path rejects")
	}
	if candidate {
		require.True(t, single, "fast path accepted a decodable tx that is not a single pay-for-fibre message")
	}
}

// pffDecodeCandidateSeeds builds tx bytes without a keyring: unsigned
// pay-for-fibre txs with and without a memo and fee, send txs, and wrappers.
func pffDecodeCandidateSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	signer := sdk.AccAddress(bytes.Repeat([]byte{7}, 20)).String()
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	pffMsg := &fibretypes.MsgPayForFibre{
		Signer: signer,
		PaymentPromise: fibretypes.PaymentPromise{
			ChainId:     "test",
			Height:      1,
			Namespace:   ns.Bytes(),
			BlobSize:    100,
			BlobVersion: fibretypes.BlobVersionZero,
			Commitment:  bytes.Repeat([]byte{0xab}, share.FibreCommitmentSize),
			Signature:   make([]byte, 64),
		},
		ValidatorSignatures: [][]byte{make([]byte, 64)},
	}
	pffAny, err := codectypes.NewAnyWithValue(pffMsg)
	require.NoError(tb, err)
	sendAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{FromAddress: signer, ToAddress: signer, Amount: sdk.NewCoins(sdk.NewInt64Coin("utia", 1))})
	require.NoError(tb, err)

	rawTx := func(body *txtypes.TxBody, authInfo *txtypes.AuthInfo) []byte {
		bodyBytes, err := body.Marshal()
		require.NoError(tb, err)
		raw := &txtypes.TxRaw{BodyBytes: bodyBytes}
		if authInfo != nil {
			raw.AuthInfoBytes, err = authInfo.Marshal()
			require.NoError(tb, err)
			raw.Signatures = [][]byte{make([]byte, 64)}
		}
		out, err := raw.Marshal()
		require.NoError(tb, err)
		return out
	}
	fee := &txtypes.AuthInfo{Fee: &txtypes.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("utia", 1)), GasLimit: 100_000}}

	pff := rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{pffAny}}, nil)
	blob, err := share.NewBlob(ns, []byte("data"), share.ShareVersionZero, nil)
	require.NoError(tb, err)
	blobWrapped, err := blobtx.MarshalBlobTx(pff, blob)
	require.NoError(tb, err)
	indexWrapped, err := blobtx.MarshalIndexWrapper(pff, 0)
	require.NoError(tb, err)

	return [][]byte{
		pff,
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{pffAny}}, fee),
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{pffAny}, Memo: "memo", TimeoutHeight: 10}, fee),
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{pffAny, sendAny}}, fee),
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{sendAny, pffAny}}, fee),
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{pffAny, pffAny}}, fee),
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{sendAny}}, fee),
		rawTx(&txtypes.TxBody{Messages: []*codectypes.Any{{TypeUrl: fibretypes.MsgPayForFibreTypeURL, Value: []byte{0xff}}}}, nil),
		blobWrapped,
		indexWrapped,
		{0x0a, 0x00},
		{},
	}
}

func TestPFFDecodeCandidateParitySeeds(t *testing.T) {
	count := newPFFMsgCounter()
	for _, raw := range pffDecodeCandidateSeeds(t) {
		pffDecodeCandidateParity(t, count, raw)
	}
}

// FuzzPFFDecodeCandidateParity mutates pay-for-fibre and ordinary tx bytes and
// checks that the fast path, IsPayForFibreTx and the SDK decoder agree on
// which txs carry a single pay-for-fibre message.
func FuzzPFFDecodeCandidateParity(f *testing.F) {
	count := newPFFMsgCounter()
	for _, raw := range pffDecodeCandidateSeeds(f) {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		pffDecodeCandidateParity(t, count, raw)
	})
}
