package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	types1 "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestPrepareProposalValidConstruction(t *testing.T) {
	// Reproduces https://github.com/celestiaorg/celestia-app/issues/4961
	t.Run("prepare proposal creates a proposal that process proposal accepts", func(t *testing.T) {
		encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		accounts := testfactory.GenerateAccounts(1)
		testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
		testApp.GetChainID()
		height := testApp.LastBlockHeight() + 1

		txs := createBlobTxs(t, testApp, encConf, kr, accounts)
		require.Equal(t, 9, len(txs))

		prepareResponse := testApp.PrepareProposal(abci.RequestPrepareProposal{
			BlockData: &types1.Data{
				Txs: txs,
			},
			Height: height,
			Time:   time.Now(),
		})
		// The filtered builder in prepare proposal should have dropped the last two txs.
		// The filtered builder should have dropped the second to last tx because it was too large to fit in the square.
		// The filtered builder should have dropped the last tx because the nonce for it was invalidated by dropping the second to last tx.
		require.Equal(t, 7, len(prepareResponse.BlockData.Txs))

		processResponse := testApp.ProcessProposal(abci.RequestProcessProposal{
			Header: types1.Header{
				Version: version.Consensus{
					Block: 1,
					App:   3,
				},
				ChainID:  testutil.ChainID,
				Height:   height,
				Time:     time.Now(),
				DataHash: prepareResponse.BlockData.Hash,
			},
			BlockData: &types1.Data{
				Txs:        prepareResponse.BlockData.Txs,
				SquareSize: prepareResponse.BlockData.SquareSize,
				Hash:       prepareResponse.BlockData.Hash,
			},
		})

		require.NotNil(t, processResponse)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processResponse.Result)
	})
}

// createBlobTxs returns 9 blob transactions. The first 8 are 1 MiB each and the last one is 100 bytes.
func createBlobTxs(t *testing.T, testApp *app.App, encConf encoding.Config, keyring keyring.Keyring, accounts []string) (txs [][]byte) {
	accountName := accounts[0]
	address := testfactory.GetAddress(keyring, accountName)
	account := testutil.DirectQueryAccount(testApp, address)
	sequence := account.GetSequence()
	accountNumber := account.GetAccountNumber()

	mebibyte := 1024 * 1024 // 1 MiB
	blobSize := 1 * mebibyte
	blobCount := 1

	for i := 0; i < 8; i++ {
		tx := testutil.BlobTxWithManualSequence(t, encConf.TxConfig, keyring, blobSize, blobCount, testutil.ChainID, accountName, sequence, accountNumber)
		txs = append(txs, tx)
		sequence++
	}

	blobSize = 100 // bytes
	tx := testutil.BlobTxWithManualSequence(t, encConf.TxConfig, keyring, blobSize, blobCount, testutil.ChainID, accountName, sequence, accountNumber)
	txs = append(txs, tx)

	return txs
}
