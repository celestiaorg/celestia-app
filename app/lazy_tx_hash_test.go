package app

import (
	"bytes"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	tmbytes "github.com/cometbft/cometbft/libs/bytes"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// lazyTxHash must log exactly what the eager tmbytes.HexBytes hash logged.
func TestLazyTxHashRendersLikeHexBytes(t *testing.T) {
	namespace := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))
	blob, err := share.NewBlob(namespace, []byte("data"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	rawBlobTx, err := blobtx.MarshalBlobTx([]byte("tx"), blob)
	require.NoError(t, err)

	for name, raw := range map[string][]byte{"plain": []byte("tx"), "blob": rawBlobTx} {
		t.Run(name, func(t *testing.T) {
			eager := tmbytes.HexBytes(coretypes.Tx(raw).Hash())
			require.Equal(t, eager.String(), lazyTxHash(raw).String())

			for _, opts := range [][]log.Option{
				{log.OutputJSONOption(), log.TimeFormatOption("")},
				{log.ColorOption(false), log.TimeFormatOption("")},
			} {
				var want, got bytes.Buffer
				log.NewLogger(&want, opts...).Info("msg", "tx", eager)
				log.NewLogger(&got, opts...).Info("msg", "tx", lazyTxHash(raw))
				require.Equal(t, want.String(), got.String())
			}
		})
	}
}
