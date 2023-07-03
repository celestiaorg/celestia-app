package blobfactory

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	apptypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// TestGenerateManyRandomRawSendTxsSameSigner_Deterministic tests whether with the same random seed the GenerateManyRandomRawSendTxsSameSigner function produces the same send transactions.
func TestGenerateManyRandomRawSendTxsSameSigner_Deterministic(t *testing.T) {
	normalTxCount := 10
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	TxDecoder := encCfg.TxConfig.TxDecoder()

	signer := apptypes.GenerateKeyringSigner(t)

	rand := tmrand.NewRand()
	rand.Seed(1)
	encodedTxs1 := GenerateManyRandomRawSendTxsSameSigner(encCfg.TxConfig, rand, signer, normalTxCount)

	rand2 := tmrand.NewRand()
	rand2.Seed(1)
	encodedTxs2 := GenerateManyRandomRawSendTxsSameSigner(encCfg.TxConfig, rand2, signer, normalTxCount)

	// additional check for the sake of future debugging
	for i := 0; i < normalTxCount; i++ {
		tx1, err := TxDecoder(encodedTxs1[i])
		assert.NoError(t, err)
		assert.NotNil(t, tx1)
		msgs1 := tx1.GetMsgs()

		tx2, err2 := TxDecoder(encodedTxs2[i])
		assert.NoError(t, err2)
		assert.NotNil(t, tx2)
		msgs2 := tx2.GetMsgs()

		assert.Equal(t, msgs1, msgs2)
		assert.Equal(t, tx1, tx2)
	}

	assert.Equal(t, encodedTxs1, encodedTxs2)
}
