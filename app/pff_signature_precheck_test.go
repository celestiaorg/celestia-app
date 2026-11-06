package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// emptyValidatorSignature is one empty MsgPayForFibre.validator_signatures entry.
var emptyValidatorSignature = []byte{0x1a, 0x00}

// testMsgPayForFibre mirrors blobfactory.NewMsgPayForFibre for callers without
// a *testing.T.
func testMsgPayForFibre() *fibretypes.MsgPayForFibre {
	pubKey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	return &fibretypes.MsgPayForFibre{
		Signer: sdk.AccAddress(pubKey.Address()).String(),
		PaymentPromise: fibretypes.PaymentPromise{
			ChainId:           "test",
			Height:            1,
			Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize)).Bytes(),
			BlobSize:          100,
			BlobVersion:       fibretypes.BlobVersionZero,
			Commitment:        bytes.Repeat([]byte{0xAB}, share.FibreCommitmentSize),
			CreationTimestamp: time.Now(),
			SignerPublicKey:   *pubKey,
			Signature:         make([]byte, 64),
		},
	}
}

// pffValueWithSignatures returns an encoded MsgPayForFibre carrying count
// validator signatures, each empty or 64 bytes.
func pffValueWithSignatures(t testing.TB, count int, full bool) []byte {
	t.Helper()
	msg := testMsgPayForFibre()
	if full {
		msg.ValidatorSignatures = make([][]byte, count)
		for i := range msg.ValidatorSignatures {
			msg.ValidatorSignatures[i] = make([]byte, 64)
		}
		bz, err := msg.Marshal()
		require.NoError(t, err)
		return bz
	}
	bz, err := msg.Marshal()
	require.NoError(t, err)
	return append(bz, bytes.Repeat(emptyValidatorSignature, count)...)
}

func rawTxWithAnys(t testing.TB, anys ...*codectypes.Any) []byte {
	t.Helper()
	body, err := (&txtypes.TxBody{Messages: anys}).Marshal()
	require.NoError(t, err)
	raw, err := (&txtypes.TxRaw{BodyBytes: body}).Marshal()
	require.NoError(t, err)
	return raw
}

func pffAny(value []byte) *codectypes.Any {
	return &codectypes.Any{TypeUrl: fibretypes.MsgPayForFibreTypeURL, Value: value}
}

func marshalAny(t testing.TB, typeURL string, msg interface{ Marshal() ([]byte, error) }) *codectypes.Any {
	t.Helper()
	bz, err := msg.Marshal()
	require.NoError(t, err)
	return &codectypes.Any{TypeUrl: typeURL, Value: bz}
}

// rejectedByDecodeOrValidateBasic reports whether raw fails the SDK decoder or
// a top-level MsgPayForFibre's ValidateBasic.
func rejectedByDecodeOrValidateBasic(txConfig client.TxConfig, raw []byte) bool {
	tx, err := txConfig.TxDecoder()(raw)
	if err != nil {
		return true
	}
	for _, msg := range tx.GetMsgs() {
		if pff, ok := msg.(*fibretypes.MsgPayForFibre); ok && pff.ValidateBasic() != nil {
			return true
		}
	}
	return false
}

func TestExceedsPFFSignatureLimit(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	limit := appconsts.MaxFibreValidatorSignatures

	atLimit := rawTxWithAnys(t, pffAny(pffValueWithSignatures(t, limit, true)))
	overLimit := rawTxWithAnys(t, pffAny(pffValueWithSignatures(t, limit+1, true)))
	emptyOverLimit := rawTxWithAnys(t, pffAny(pffValueWithSignatures(t, limit+1, false)))

	exec := &authz.MsgExec{Grantee: "grantee", Msgs: []*codectypes.Any{pffAny(pffValueWithSignatures(t, limit+1, false))}}
	execOverLimit := rawTxWithAnys(t, marshalAny(t, sdk.MsgTypeURL(exec), exec))
	proposal := &govv1.MsgSubmitProposal{Messages: []*codectypes.Any{pffAny(pffValueWithSignatures(t, limit+1, false))}}
	proposalOverLimit := rawTxWithAnys(t, marshalAny(t, sdk.MsgTypeURL(proposal), proposal))
	indexWrapped, err := coretypes.MarshalIndexWrapper(emptyOverLimit, 0)
	require.NoError(t, err)

	// The decoder keeps the last Any.value, so an oversized earlier value is never decoded.
	shadowed := append(protowire.AppendTag(nil, 1, protowire.BytesType), protowire.AppendString(nil, fibretypes.MsgPayForFibreTypeURL)...)
	shadowed = protowire.AppendBytes(protowire.AppendTag(shadowed, 2, protowire.BytesType), pffValueWithSignatures(t, limit+1, false))
	shadowed = protowire.AppendBytes(protowire.AppendTag(shadowed, 2, protowire.BytesType), pffValueWithSignatures(t, 1, true))
	body := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), shadowed)
	shadowedTx := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), body)

	// Groups on a non-critical field number pass the decoder's unknown field
	// check, so the scan must skip them rather than stop.
	group := protowire.AppendTag(protowire.AppendTag(nil, 1025, protowire.StartGroupType), 1025, protowire.EndGroupType)
	overValue := pffValueWithSignatures(t, limit+1, false)
	anyBytes := func(prefix, value []byte) []byte {
		bz := append(append([]byte{}, prefix...), protowire.AppendTag(nil, 1, protowire.BytesType)...)
		bz = protowire.AppendString(bz, fibretypes.MsgPayForFibreTypeURL)
		return protowire.AppendBytes(protowire.AppendTag(bz, 2, protowire.BytesType), value)
	}
	txRaw := func(bodyPrefix, anyBz, authInfo []byte, signatures ...[]byte) []byte {
		body := protowire.AppendBytes(protowire.AppendTag(append([]byte{}, bodyPrefix...), 1, protowire.BytesType), anyBz)
		bz := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), body)
		if authInfo != nil {
			bz = protowire.AppendBytes(protowire.AppendTag(bz, 2, protowire.BytesType), authInfo)
		}
		for _, sig := range signatures {
			bz = protowire.AppendBytes(protowire.AppendTag(bz, 3, protowire.BytesType), sig)
		}
		return bz
	}
	groupInValue := txRaw(nil, anyBytes(nil, append(append([]byte{}, group...), overValue...)), nil)
	groupInAny := txRaw(nil, anyBytes(group, overValue), nil)
	groupInBody := txRaw(group, anyBytes(nil, overValue), nil)
	// An "INDX" signature does not make a tx an IndexWrapper when the
	// IndexWrapper unmarshal fails; the decoder then reads it as a TxRaw.
	indxSignature := txRaw(nil, anyBytes(nil, overValue), bytes.Repeat([]byte{0xff}, 11), []byte("INDX"))
	_, isIndexWrapper := coretypes.UnmarshalIndexWrapper(indxSignature)
	require.False(t, isIndexWrapper)
	indexWrappedGroup := append(append([]byte{}, indexWrapped...), protowire.AppendTag(protowire.AppendTag(nil, 5, protowire.StartGroupType), 5, protowire.EndGroupType)...)
	_, isIndexWrapper = coretypes.UnmarshalIndexWrapper(indexWrappedGroup)
	require.True(t, isIndexWrapper)
	// A field number protowire rejects, which the IndexWrapper unmarshal skips.
	indexWrappedBigField := append(append(protowire.AppendVarint(nil, uint64(1<<29)<<3), 0x00), indexWrapped...)
	_, isIndexWrapper = coretypes.UnmarshalIndexWrapper(indexWrappedBigField)
	require.True(t, isIndexWrapper)

	tests := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"plain PFF tx", newPayForFibreTx(t, txConfig, func(*fibretypes.MsgPayForFibre) {}), false},
		{"PFF at the signature limit", atLimit, false},
		{"PFF one over the signature limit", overLimit, true},
		{"PFF with empty signatures over the limit", emptyOverLimit, true},
		{"PFF over the limit inside MsgExec", execOverLimit, true},
		{"PFF over the limit inside MsgSubmitProposal", proposalOverLimit, true},
		{"index wrapped PFF over the limit", indexWrapped, true},
		{"oversized Any.value shadowed by a later one", shadowedTx, false},
		{"not a tx", []byte("not a tx"), false},
		{"truncated PFF over the limit", overLimit[:len(overLimit)/2], false},
		{"group in the PFF value", groupInValue, true},
		{"group in the Any", groupInAny, true},
		{"group in the TxBody", groupInBody, true},
		{"INDX signature on a tx that is not an IndexWrapper", indxSignature, true},
		{"index wrapper with a trailing group", indexWrappedGroup, true},
		{"index wrapper with a field number protowire rejects", indexWrappedBigField, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, exceedsPFFSignatureLimit(tc.raw))
		})
	}

	// The limit matches ValidateBasic exactly; nested and index wrapped txs
	// are rejected by ante and the index wrapper decoder respectively.
	require.False(t, rejectedByDecodeOrValidateBasic(txConfig, atLimit))
	require.True(t, rejectedByDecodeOrValidateBasic(txConfig, overLimit))
	require.True(t, rejectedByDecodeOrValidateBasic(txConfig, emptyOverLimit))
	require.True(t, rejectedByDecodeOrValidateBasic(txConfig, indexWrapped))
	for _, raw := range [][]byte{groupInValue, groupInAny, groupInBody} {
		tx, err := txConfig.TxDecoder()(raw)
		require.NoError(t, err, "the decoder accepts the group, so it would decode every entry")
		require.Equal(t, limit+1, maxPFFSignatures(tx.GetMsgs()))
	}
	require.False(t, rejectedByDecodeOrValidateBasic(txConfig, shadowedTx))
}

// newMaxSizePFFSignatureBomb returns a MaxTxSize-bounded tx whose
// MsgPayForFibre carries millions of empty validator signatures.
func newMaxSizePFFSignatureBomb(t testing.TB) []byte {
	t.Helper()
	value := pffValueWithSignatures(t, 0, false)
	count := (appconsts.MaxTxSize - len(value) - 1024) / len(emptyValidatorSignature)
	raw := rawTxWithAnys(t, pffAny(pffValueWithSignatures(t, count, false)))
	require.LessOrEqual(t, len(raw), appconsts.MaxTxSize)
	return raw
}

func TestExceedsPFFSignatureLimitDoesNotAllocate(t *testing.T) {
	raw := newMaxSizePFFSignatureBomb(t)
	flagged := false
	allocs := testing.AllocsPerRun(10, func() {
		flagged = exceedsPFFSignatureLimit(raw)
	})
	require.True(t, flagged)
	require.Zero(t, allocs)
}

func BenchmarkPFFSignatureBomb(b *testing.B) {
	raw := newMaxSizePFFSignatureBomb(b)
	b.Run("precheck", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if !exceedsPFFSignatureLimit(raw) {
				b.Fatal("precheck must flag the tx")
			}
		}
	})
	// Worst case for the walk itself: millions of empty messages, none of them
	// a MsgPayForFibre, so nothing stops it early.
	emptyAnys := bytes.Repeat([]byte{0x0a, 0x00}, (appconsts.MaxTxSize-1024)/2)
	emptyAnys = append(emptyAnys, protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), msgPayForFibreTypeURLBytes)...)
	walkAll := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), emptyAnys)
	b.Run("precheck full walk", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if exceedsPFFSignatureLimit(walkAll) {
				b.Fatal("precheck must not flag the tx")
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		decode := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig.TxDecoder()
		b.ReportAllocs()
		for b.Loop() {
			_, _ = decode(raw)
		}
	})
}

// maxPFFSignatures returns the most validator signatures any MsgPayForFibre in
// msgs carries, including those nested in MsgExec or MsgSubmitProposal.
func maxPFFSignatures(msgs []sdk.Msg) int {
	most := -1
	for _, msg := range msgs {
		count := -1
		switch msg := msg.(type) {
		case *fibretypes.MsgPayForFibre:
			count = len(msg.ValidatorSignatures)
		case *authz.MsgExec:
			if nested, err := msg.GetMessages(); err == nil {
				count = maxPFFSignatures(nested)
			}
		case *govv1.MsgSubmitProposal:
			if nested, err := msg.GetMsgs(); err == nil {
				count = maxPFFSignatures(nested)
			}
		}
		most = max(most, count)
	}
	return most
}

// FuzzExceedsPFFSignatureLimitMatchesDecoder checks that, for every tx the
// decoder accepts, the precheck flags it exactly when it carries a
// MsgPayForFibre over the signature limit. A flagged top-level MsgPayForFibre
// fails ValidateBasic and a nested one fails ante, so this also shows the
// precheck never rejects a tx that could otherwise pass CheckTx.
func FuzzExceedsPFFSignatureLimitMatchesDecoder(f *testing.F) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	limit := appconsts.MaxFibreValidatorSignatures
	group := protowire.AppendTag(protowire.AppendTag(nil, 1025, protowire.StartGroupType), 1025, protowire.EndGroupType)
	f.Add(rawTxWithAnys(f, pffAny(pffValueWithSignatures(f, limit, true))))
	f.Add(rawTxWithAnys(f, pffAny(pffValueWithSignatures(f, limit+1, false))))
	f.Add(rawTxWithAnys(f, pffAny(pffValueWithSignatures(f, 1, true))))
	f.Add(rawTxWithAnys(f, pffAny(append(group, pffValueWithSignatures(f, limit+1, false)...))))
	exec := &authz.MsgExec{Grantee: "grantee", Msgs: []*codectypes.Any{pffAny(pffValueWithSignatures(f, limit+1, false))}}
	f.Add(rawTxWithAnys(f, marshalAny(f, sdk.MsgTypeURL(exec), exec)))
	f.Fuzz(func(t *testing.T, raw []byte) {
		flagged := exceedsPFFSignatureLimit(raw)
		tx, err := txConfig.TxDecoder()(raw)
		if err != nil {
			return
		}
		if overLimit := maxPFFSignatures(tx.GetMsgs()) > limit; overLimit != flagged {
			t.Fatalf("precheck flagged=%v but the decoded tx is over the limit=%v", flagged, overLimit)
		}
	})
}
