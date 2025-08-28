package encoding_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMakeConfig(t *testing.T) {
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	require.NotNil(t, config)

	t.Run("should decode a blob tx", func(t *testing.T) {
		tx := createBlobTx(t, testApp, config, kr, accounts)

		decodedTx, err := config.TxConfig.TxDecoder()(tx)
		require.NoError(t, err)

		msgs := decodedTx.GetMsgs()
		require.NotEmpty(t, msgs)

		msgType := sdk.MsgTypeURL(msgs[0])
		require.Equal(t, "/celestia.blob.v1.MsgPayForBlobs", msgType)
	})
}

func createBlobTx(t *testing.T, testApp *app.App, config encoding.Config, kr keyring.Keyring, accounts []string) []byte {
	accountName := accounts[0]
	address := testfactory.GetAddress(kr, accountName)
	account := testutil.DirectQueryAccount(testApp, address)
	sequence := account.GetSequence()
	accountNumber := account.GetAccountNumber()
	blobSize := 100
	blobCount := 1

	tx := testutil.BlobTxWithManualSequence(t, config.TxConfig, kr, blobSize, blobCount, testutil.ChainID, accountName, sequence, accountNumber)
	return tx
}
