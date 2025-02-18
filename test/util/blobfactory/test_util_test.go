package blobfactory_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateManyRandomRawSendTxsSameSigner_Deterministic tests whether with the same random seed the GenerateManyRandomRawSendTxsSameSigner function produces the same send transactions.
func TestGenerateManyRandomRawSendTxsSameSigner_Deterministic(t *testing.T) {
	normalTxCount := 10
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	TxDecoder := enc.TxConfig.TxDecoder()

	kr, _ := testnode.NewKeyring(testfactory.TestAccName)
	signer, err := user.NewSigner(kr, enc.TxConfig, testfactory.ChainID, appconsts.LatestVersion, user.NewAccount(testfactory.TestAccName, 1, 0))
	require.NoError(t, err)

	encodedTxs1 := blobfactory.GenerateManyRandomRawSendTxsSameSigner(signer, normalTxCount)

	require.NoError(t, signer.SetSequence(testfactory.TestAccName, 0))
	encodedTxs2 := blobfactory.GenerateManyRandomRawSendTxsSameSigner(signer, normalTxCount)

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
