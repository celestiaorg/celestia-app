package encoding

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

// FuzzCheckCanonicalTx checks that the app decoder returns on any bytes, and
// that every tx it accepts the SDK decoder alone decodes to the same tx.
func FuzzCheckCanonicalTx(f *testing.F) {
	seeds := append(decoderSeeds(f), nestedDecoderSeeds(f)...)
	for _, seed := range seeds {
		f.Add(seed)
		f.Add(append(bytes.Clone(seed), 0x18, 0x81, 0x00))
	}
	config := MakeConfig(bank.AppModuleBasic{}, blob.AppModule{}, authzmodule.AppModuleBasic{}, gov.NewAppModuleBasic(nil))
	decode := config.TxConfig.TxDecoder()
	sdkDecode := authtx.DefaultTxDecoder(config.Codec)
	f.Fuzz(func(t *testing.T, raw []byte) {
		appTx, err := decode(raw)
		if err != nil {
			return
		}
		// BlobTx and IndexWrapper envelopes reach the SDK decoder unwrapped.
		sdkTx, err := blobTxDecoder(indexWrapperDecoder(sdkDecode))(raw)
		require.NoError(t, err)
		require.Equal(t, protoTxBytes(t, sdkTx), protoTxBytes(t, appTx))
	})
}

// nestedDecoderSeeds returns txs with nested Anys: a MsgExec in a MsgExec, a
// gov proposal holding a MsgExec, and a multisig signer.
func nestedDecoderSeeds(t testing.TB) [][]byte {
	t.Helper()
	from := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	send := banktypes.NewMsgSend(from, from, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
	exec := authz.NewMsgExec(from, []sdk.Msg{send})
	execExec := authz.NewMsgExec(from, []sdk.Msg{&exec})
	proposal, err := govv1.NewMsgSubmitProposal([]sdk.Msg{&exec}, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)), from.String(), "", "title", "summary", true)
	require.NoError(t, err)
	k1 := secp256k1.GenPrivKeyFromSecret([]byte("a")).PubKey()
	k2 := secp256k1.GenPrivKeyFromSecret([]byte("b")).PubKey()
	multisigKey, err := codectypes.NewAnyWithValue(kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, k2}))
	require.NoError(t, err)

	msgs := []sdk.Msg{&execExec, proposal}
	seeds := make([][]byte, 0, len(msgs))
	for _, msg := range msgs {
		msgAny, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		body, err := (&txtypes.TxBody{Messages: []*codectypes.Any{msgAny}}).Marshal()
		require.NoError(t, err)
		authInfo, err := (&txtypes.AuthInfo{
			SignerInfos: []*txtypes.SignerInfo{{PublicKey: multisigKey, ModeInfo: &txtypes.ModeInfo{
				Sum: &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON}},
			}}},
			Fee: &txtypes.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("utia", 10)), GasLimit: 100_000},
		}).Marshal()
		require.NoError(t, err)
		raw, err := (&txtypes.TxRaw{BodyBytes: body, AuthInfoBytes: authInfo, Signatures: [][]byte{make([]byte, 64)}}).Marshal()
		require.NoError(t, err)
		seeds = append(seeds, raw)
	}
	return seeds
}

// protoTxBytes returns the encoding of a decoded tx, to compare two decodes.
func protoTxBytes(t *testing.T, tx sdk.Tx) []byte {
	t.Helper()
	protoTx, ok := tx.(interface{ GetProtoTx() *txtypes.Tx })
	require.True(t, ok)
	bz, err := protoTx.GetProtoTx().Marshal()
	require.NoError(t, err)
	return bz
}

func BenchmarkCanonicalTxDecoder(b *testing.B) {
	config := MakeConfig(bank.AppModuleBasic{}, blob.AppModule{})
	sdkDecode := authtx.DefaultTxDecoder(config.Codec)
	decode := authtx.StrictTxDecoder(config.Codec)
	seeds := decoderSeeds(b)
	for _, bc := range []struct {
		name string
		raw  []byte
	}{
		{"MsgSend", seeds[0]},
		{"MsgPayForBlobs", indexWrappedTx(b, seeds[2])},
	} {
		_, err := decode(bc.raw)
		require.NoError(b, err)
		b.Run(bc.name+"/sdk", func(b *testing.B) {
			for range b.N {
				_, _ = sdkDecode(bc.raw)
			}
		})
		b.Run(bc.name+"/canonical", func(b *testing.B) {
			for range b.N {
				_, _ = decode(bc.raw)
			}
		})
	}
}

// indexWrappedTx returns the tx in an IndexWrapper.
func indexWrappedTx(t testing.TB, raw []byte) []byte {
	t.Helper()
	iw, ok := blobtypes.UnmarshalIndexWrapper(raw)
	require.True(t, ok)
	return iw.Tx
}

// TestNestedDecoderSeedsDecode checks the nested seeds decode, so the fuzz
// target starts from accepted txs.
func TestNestedDecoderSeedsDecode(t *testing.T) {
	config := MakeConfig(bank.AppModuleBasic{}, blob.AppModule{}, authzmodule.AppModuleBasic{}, gov.NewAppModuleBasic(nil))
	for _, seed := range nestedDecoderSeeds(t) {
		_, err := config.TxConfig.TxDecoder()(seed)
		require.NoError(t, err)
	}
}
