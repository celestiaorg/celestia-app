package encoding

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/stretchr/testify/require"
)

// FuzzCheckCanonicalTx checks that every tx the app decoder accepts also decodes
// under the SDK decoder alone.
func FuzzCheckCanonicalTx(f *testing.F) {
	for _, seed := range decoderSeeds(f) {
		f.Add(seed)
		f.Add(append(bytes.Clone(seed), 0x18, 0x81, 0x00))
	}
	config := MakeConfig(bank.AppModuleBasic{}, blob.AppModule{})
	decode := config.TxConfig.TxDecoder()
	sdkDecode := authtx.DefaultTxDecoder(config.Codec)
	f.Fuzz(func(t *testing.T, raw []byte) {
		if _, err := decode(raw); err != nil {
			return
		}
		// BlobTx and IndexWrapper envelopes reach the SDK decoder unwrapped.
		_, err := blobTxDecoder(indexWrapperDecoder(sdkDecode))(raw)
		require.NoError(t, err)
	})
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
