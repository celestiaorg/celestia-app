package blobfactory

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	apptypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

// TestRandMultiBlobTxsSameSigner_Deterministic tests whether with the same random seed the RandMultiBlobTxsSameSigner function produces the same blob txs.
func TestRandMultiBlobTxsSameSigner_Deterministic(t *testing.T) {
	pfbCount := 10
	signer := apptypes.GenerateKeyringSigner(t)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encCfg.TxConfig.TxDecoder()

	rand1 := tmrand.NewRand()
	rand1.Seed(1)
	marshalledBlobTxs1 := RandMultiBlobTxsSameSigner(t, encCfg.TxConfig.TxEncoder(), rand1, signer, pfbCount)

	rand2 := tmrand.NewRand()
	rand2.Seed(1)
	marshalledBlobTxs2 := RandMultiBlobTxsSameSigner(t, encCfg.TxConfig.TxEncoder(), rand2, signer, pfbCount)

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
