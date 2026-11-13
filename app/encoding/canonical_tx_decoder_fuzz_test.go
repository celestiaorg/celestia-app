package encoding

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/blob"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/stretchr/testify/require"
)

// FuzzCheckCanonicalTx checks that the encoding check returns rather than
// panics on any bytes, and that every tx the app decoder accepts also decodes
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
		_ = checkCanonicalTx(raw)
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
	decode := canonicalTxDecoder(sdkDecode)
	seeds := decoderSeeds(b)
	for _, bc := range []struct {
		name string
		raw  []byte
	}{
		{"MsgSend", seeds[0]},
		{"MsgPayForBlobs", fieldValue(seeds[2], 1)},
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
		b.Run(bc.name+"/check-only", func(b *testing.B) {
			for range b.N {
				_ = checkCanonicalTx(bc.raw)
			}
		})
	}
}
