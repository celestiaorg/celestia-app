package app

import (
	"bytes"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// firstMsgPeekSeeds returns signed and unsigned send and pay-for-fibre txs,
// multi-message txs in both orders and the pay-for-fibre classification seeds.
func firstMsgPeekSeeds(tb testing.TB, txConfig client.TxConfig) [][]byte {
	tb.Helper()
	seeds := pffDecodeCandidateSeeds(tb)

	kr := testfactory.TestKeyring(encoding.MakeConfig(ModuleEncodingRegisters...).Codec)
	signer, err := user.NewSigner(kr, txConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 1, 0))
	require.NoError(tb, err)
	addr := signer.Account(testfactory.TestAccName).Address()
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	pffMsg := &fibretypes.MsgPayForFibre{
		Signer: addr.String(),
		PaymentPromise: fibretypes.PaymentPromise{
			ChainId:     testfactory.ChainID,
			Height:      1,
			Namespace:   ns.Bytes(),
			BlobSize:    100,
			BlobVersion: fibretypes.BlobVersionZero,
			Commitment:  bytes.Repeat([]byte{0xab}, share.FibreCommitmentSize),
			Signature:   make([]byte, 64),
		},
		ValidatorSignatures: [][]byte{make([]byte, 64)},
	}
	send := &banktypes.MsgSend{FromAddress: addr.String(), ToAddress: addr.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))}
	for _, msgs := range [][]sdk.Msg{{send}, {pffMsg}, {send, pffMsg}, {pffMsg, send}, {pffMsg, pffMsg}} {
		signed, _, err := signer.CreateTx(msgs, user.SetGasLimit(200_000), user.SetFee(2_000), user.SetMemo("memo"))
		require.NoError(tb, err)
		builder := txConfig.NewTxBuilder()
		require.NoError(tb, builder.SetMsgs(msgs...))
		unsigned, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(tb, err)
		seeds = append(seeds, signed, unsigned)
	}
	return seeds
}

// firstMsgPeekParity checks firstMsgIsPayForFibre and separateTxsDecoded
// against the SDK decoder on raw. PrepareProposal routes a tx by the peek
// alone, so a decodable pay-for-fibre tx the peek calls ordinary (or the
// reverse) would skip or wrongly take the fibre filtering path.
func firstMsgPeekParity(t *testing.T, txConfig client.TxConfig, raw []byte) {
	t.Helper()
	if len(raw) > appconsts.MaxTxSize {
		return
	}
	isPFF, ok := firstMsgIsPayForFibre(raw)
	// separateTxsDecoded routes blob txs by type ID before peeking.
	if tx.HasBlobTxTypeID(raw) {
		return
	}
	normal, blobs, pffs := separateTxsDecoded(log.NewNopLogger(), txConfig, [][]byte{raw})
	require.Empty(t, blobs)
	require.LessOrEqual(t, len(normal)+len(pffs), 1)

	sdkTx, err := txConfig.TxDecoder()(raw)
	if err != nil {
		return
	}
	msgs := sdkTx.GetMsgs()
	decodedPFF := false
	if len(msgs) > 0 {
		_, decodedPFF = msgs[0].(*fibretypes.MsgPayForFibre)
	}
	require.Truef(t, ok, "the SDK decoder accepts a tx the peek cannot parse (msgs[0] pay-for-fibre=%v)", decodedPFF)
	require.Equalf(t, decodedPFF, isPFF, "peek disagrees with the SDK decoder on msgs[0] (%d msgs)", len(msgs))

	// Routing: a tx whose decoded messages are all pay-for-fibre or all
	// other non-PFB messages must land on the matching list.
	_, hasPFB := hasPFB(msgs)
	pffCount := countMsgPayForFibre(sdkTx)
	switch {
	case hasPFB:
	case pffCount > 0 && pffCount == len(msgs):
		require.Len(t, pffs, 1, "decodable pay-for-fibre tx not routed to the fibre path")
	case pffCount == 0:
		require.Len(t, normal, 1, "decodable tx without pay-for-fibre not routed to the normal path")
	}
}

func TestFirstMsgPeekParitySeeds(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	for _, raw := range firstMsgPeekSeeds(t, txConfig) {
		firstMsgPeekParity(t, txConfig, raw)
	}
}

// FuzzFirstMsgIsPayForFibre mutates send, pay-for-fibre and wrapped tx bytes
// and checks the PrepareProposal wire-level peek, and the routing built on it,
// against the SDK decoder wherever that decoder accepts the bytes.
func FuzzFirstMsgIsPayForFibre(f *testing.F) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	for _, raw := range firstMsgPeekSeeds(f, txConfig) {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		firstMsgPeekParity(t, txConfig, raw)
	})
}
