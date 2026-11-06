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

// FuzzExceedsPFFSignatureLimitNoFalsePositives checks that the precheck only
// flags txs the decoder or ValidateBasic would reject anyway.
func FuzzExceedsPFFSignatureLimitNoFalsePositives(f *testing.F) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	limit := appconsts.MaxFibreValidatorSignatures
	f.Add(rawTxWithAnys(f, pffAny(pffValueWithSignatures(f, limit, true))))
	f.Add(rawTxWithAnys(f, pffAny(pffValueWithSignatures(f, limit+1, false))))
	f.Add(rawTxWithAnys(f, pffAny(pffValueWithSignatures(f, 1, true))))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if !exceedsPFFSignatureLimit(raw) {
			return
		}
		tx, err := txConfig.TxDecoder()(raw)
		if err != nil {
			return
		}
		for _, msg := range tx.GetMsgs() {
			if pff, ok := msg.(*fibretypes.MsgPayForFibre); ok && pff.ValidateBasic() != nil {
				return
			}
			// A MsgPayForFibre nested in MsgExec or MsgSubmitProposal is rejected by ante.
			if _, ok := msg.(*authz.MsgExec); ok {
				return
			}
			if _, ok := msg.(*govv1.MsgSubmitProposal); ok {
				return
			}
		}
		t.Fatalf("precheck flagged a tx that decodes and passes ValidateBasic")
	})
}
