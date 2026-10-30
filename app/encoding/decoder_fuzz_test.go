package encoding

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/txutil"
	"github.com/celestiaorg/celestia-app/v11/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// decoderPair holds the decoder MakeConfig installs, which skips the BlobTx
// and IndexWrapper probes for plain SDK envelopes, and the same decoder chain
// without that fast path.
type decoderPair struct {
	fast, wrapped sdk.TxDecoder
}

func newDecoderPair() decoderPair {
	config := MakeConfig(bank.AppModuleBasic{}, blob.AppModule{})
	sdkDecoder := authtx.DefaultTxDecoder(config.Codec)
	return decoderPair{
		fast:    config.TxConfig.TxDecoder(),
		wrapped: rejectEmptyTxDecoder(blobTxDecoder(indexWrapperDecoder(sdkDecoder))),
	}
}

// decoderSeeds returns a bank send tx, the same tx wrapped in a BlobTx, a
// pay-for-blobs tx wrapped in an IndexWrapper, and a TxRaw carrying only a body.
func decoderSeeds(t testing.TB) [][]byte {
	t.Helper()
	from := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	encode := func(m sdk.Msg) ([]byte, []byte) {
		msg, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		body, err := (&txtypes.TxBody{Messages: []*codectypes.Any{msg}, Memo: "fuzz"}).Marshal()
		require.NoError(t, err)
		authInfo, err := (&txtypes.AuthInfo{Fee: &txtypes.Fee{GasLimit: 100_000}}).Marshal()
		require.NoError(t, err)
		raw, err := (&txtypes.TxRaw{BodyBytes: body, AuthInfoBytes: authInfo, Signatures: [][]byte{make([]byte, 64)}}).Marshal()
		require.NoError(t, err)
		return raw, body
	}
	raw, body := encode(banktypes.NewMsgSend(from, from, sdk.NewCoins(sdk.NewInt64Coin("utia", 1))))
	bodyOnly, err := (&txtypes.TxRaw{BodyBytes: body}).Marshal()
	require.NoError(t, err)

	blob, err := share.NewV0Blob(share.MustNewV0Namespace(bytes.Repeat([]byte{2}, 10)), []byte("data"))
	require.NoError(t, err)
	blobTx, err := squaretx.MarshalBlobTx(raw, blob)
	require.NoError(t, err)
	pfb, err := blobtypes.NewMsgPayForBlobs(from.String(), 0, blob)
	require.NoError(t, err)
	pfbRaw, _ := encode(pfb)
	indexWrapper, err := squaretx.MarshalIndexWrapper(pfbRaw, 7)
	require.NoError(t, err)
	return [][]byte{raw, blobTx, indexWrapper, bodyOnly}
}

// FuzzTxDecoderFastPath checks that the decoder fast path for plain SDK
// envelopes decodes every byte string exactly as the full wrapper chain does.
// Every node decodes proposer and mempool txs through this decoder, so a
// divergence would make the fast path accept or reject a tx, or see different
// messages, than the wrapper chain it replaces.
func FuzzTxDecoderFastPath(f *testing.F) {
	for _, seed := range decoderSeeds(f) {
		f.Add(seed)
		// Mark the plain tx with a wrapper type id in field 3.
		f.Add(append(bytes.Clone(seed), 0x1a, 0x04, 'B', 'L', 'O', 'B'))
		f.Add(append(bytes.Clone(seed), 0x1a, 0x04, 'I', 'N', 'D', 'X'))
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})
	pair := newDecoderPair()
	f.Fuzz(func(t *testing.T, raw []byte) {
		fastTx, fastErr := pair.fast(raw)
		wrappedTx, wrappedErr := pair.wrapped(raw)
		require.Equal(t, wrappedErr == nil, fastErr == nil, "fast: %v, wrapped: %v", fastErr, wrappedErr)
		if fastErr != nil {
			require.Equal(t, wrappedErr.Error(), fastErr.Error())
			return
		}
		fastProto, ok := fastTx.(interface{ GetProtoTx() *txtypes.Tx })
		require.True(t, ok)
		wrappedProto, ok := wrappedTx.(interface{ GetProtoTx() *txtypes.Tx })
		require.True(t, ok)
		fastBytes, err := fastProto.GetProtoTx().Marshal()
		require.NoError(t, err)
		wrappedBytes, err := wrappedProto.GetProtoTx().Marshal()
		require.NoError(t, err)
		require.Equal(t, wrappedBytes, fastBytes)
	})
}

// TestTxDecoderFastPathSeeds checks the plain seed takes the fast path and
// every seed decodes, so the fuzz target compares successful decodes.
func TestTxDecoderFastPathSeeds(t *testing.T) {
	seeds := decoderSeeds(t)
	_, plain := txutil.PlainSDKBody(seeds[0])
	require.True(t, plain)
	_, plain = txutil.PlainSDKBody(seeds[1])
	require.False(t, plain)
	pair := newDecoderPair()
	for _, seed := range seeds {
		_, err := pair.fast(seed)
		require.NoError(t, err)
	}
}
