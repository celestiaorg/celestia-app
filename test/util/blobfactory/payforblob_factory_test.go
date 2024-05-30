package blobfactory_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

// TestRandMultiBlobTxsSameSigner_Deterministic tests whether with the same random seed the RandMultiBlobTxsSameSigner function produces the same blob txs.
func TestRandMultiBlobTxsSameSigner_Deterministic(t *testing.T) {
	pfbCount := 10
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encCfg.TxConfig.TxDecoder()

	rand1 := tmrand.NewRand()
	rand1.Seed(1)
	marshalledBlobTxs1 := blobfactory.RandMultiBlobTxsSameSigner(t, rand1, signer, pfbCount)

	require.NoError(t, signer.SetSequence(testfactory.TestAccName, 0))
	rand2 := tmrand.NewRand()
	rand2.Seed(1)
	marshalledBlobTxs2 := blobfactory.RandMultiBlobTxsSameSigner(t, rand2, signer, pfbCount)

	// additional checks for the sake of future debugging
	for index := 0; index < pfbCount; index++ {
		blobTx1, isBlob := types.UnmarshalBlobTx(marshalledBlobTxs1[index])
		assert.True(t, isBlob)
		pfbMsgs1, err := decoder(blobTx1.Tx)
		assert.NoError(t, err)

		blobTx2, isBlob := types.UnmarshalBlobTx(marshalledBlobTxs2[index])
		assert.True(t, isBlob)
		pfbMsgs2, err := decoder(blobTx2.Tx)
		assert.NoError(t, err)

		assert.Equal(t, blobTx1.Blobs, blobTx2.Blobs)
		assert.Equal(t, pfbMsgs1, pfbMsgs2)

		msgs2 := pfbMsgs2.GetMsgs()
		msgs1 := pfbMsgs1.GetMsgs()
		for i, msg := range msgs1 {
			assert.Equal(t, msg, msgs2[i])
		}
	}

	assert.Equal(t, marshalledBlobTxs1, marshalledBlobTxs2)
}
