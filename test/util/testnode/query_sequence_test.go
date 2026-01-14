package testnode

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	"github.com/celestiaorg/celestia-app/v7/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v7/x/blob/types"
	"github.com/celestiaorg/go-square/v3/share"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestQuerySequence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	suite.Run(t, new(MempoolQuerySequenceSuite))
}

type MempoolQuerySequenceSuite struct {
	suite.Suite

	accounts []string
	cctx     Context
	txClient *user.TxClient
}

func (s *MempoolQuerySequenceSuite) SetupSuite() {
	t := s.T()
	s.accounts = testfactory.GenerateAccounts(2)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blobGenState := blobtypes.DefaultGenesis()
	blobGenState.Params.GovMaxSquareSize = uint64(appconsts.SquareSizeUpperBound)

	// Create a config with a longer block time (2 seconds) to allow time to query
	// the sequence before the transaction is included in a block
	cfg := DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithModifiers(genesis.SetBlobParams(enc.Codec, blobGenState.Params)).
		WithDelayedPrecommitTimeout(time.Second * 2)

	cctx, _, _ := NewNetwork(t, cfg)
	s.cctx = cctx

	// Wait for the first block to be produced so checkState is initialized
	require.NoError(t, s.cctx.WaitForBlocks(1))

	txClient, err := user.SetupTxClient(
		s.cctx.GoContext(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		enc,
		user.WithDefaultAccount(s.accounts[0]),
	)
	require.NoError(t, err)
	s.txClient = txClient
}

func (s *MempoolQuerySequenceSuite) TearDownSuite() {
	if s.txClient != nil {
		s.txClient.StopTxQueueForTest()
	}
}

func (s *MempoolQuerySequenceSuite) TestQuerySequence() {
	t := s.T()
	require := require.New(t)

	kr := s.cctx.Keyring
	rec, err := kr.Key(s.accounts[0])
	require.NoError(err)

	addr, err := rec.GetAddress()
	require.NoError(err)

	// Query the initial sequence - should be 0 (account starts with sequence 0)
	resp1, err := s.cctx.tmNode.ProxyApp().Mempool().QuerySequence(context.Background(), &abci.RequestQuerySequence{
		Signer: addr.Bytes(),
	})
	require.NoError(err)
	require.NotNil(resp1)
	initialSequence := resp1.Sequence
	t.Logf("Initial sequence for account %s: %d", addr.String(), initialSequence)

	// Submit a transaction with BroadcastPayForBlob (which should add it to mempool)
	txCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	blob, err := blobtypes.NewV0Blob(share.RandomBlobNamespace(), []byte("test data"))
	require.NoError(err)

	txResp, err := s.txClient.BroadcastPayForBlob(txCtx, []*share.Blob{blob})
	require.NoError(err)
	t.Logf("Submitted transaction: %s", txResp.TxHash)

	// Wait a brief moment for the transaction to be added to the mempool
	time.Sleep(200 * time.Millisecond)

	// Query the sequence number after submitting the transaction
	// The mempool should now return the sequence of the transaction in the mempool
	resp2, err := s.cctx.tmNode.ProxyApp().Mempool().QuerySequence(context.Background(), &abci.RequestQuerySequence{
		Signer: addr.Bytes(),
	})
	require.NoError(err)
	require.NotNil(resp2)
	t.Logf("Sequence after tx submission for account %s: %d", addr.String(), resp2.Sequence)

	// The sequence should be the same as initial (the tx in mempool uses this sequence)
	require.Equal(initialSequence+1, resp2.Sequence, "sequence should be the same as initial when tx is in mempool")

	// Wait for the transaction to be included in a block
	_, err = s.txClient.ConfirmTx(txCtx, txResp.TxHash)
	require.NoError(err)
	t.Logf("Transaction committed to block")

	// After block commit, wait a moment for checkState to be updated
	time.Sleep(100 * time.Millisecond)

	// Query the sequence number after the transaction has been committed
	// Now it should return the next sequence (incremented by 1)
	resp3, err := s.cctx.tmNode.ProxyApp().Mempool().QuerySequence(context.Background(), &abci.RequestQuerySequence{
		Signer: addr.Bytes(),
	})
	require.NoError(err)
	require.NotNil(resp3)
	finalSequence := resp3.Sequence

	t.Logf("Sequence after commit for account %s: %d", addr.String(), finalSequence)

	// After the transaction is committed, the sequence should be incremented
	require.Equal(initialSequence+1, finalSequence, "sequence should increment by 1 after transaction is committed")
}

func (s *MempoolQuerySequenceSuite) TestQuerySequenceUnknownAccount() {
	t := s.T()
	require := require.New(t)

	// Create a random address that doesn't have any transactions
	randomAddr := sdk.AccAddress([]byte("randomaddress123"))

	ctx := context.Background()
	resp, err := s.cctx.tmNode.ProxyApp().Mempool().QuerySequence(ctx, &abci.RequestQuerySequence{
		Signer: randomAddr.Bytes(),
	})

	// Should not error, but should return sequence 0
	require.NoError(err)
	require.NotNil(resp)
	require.Equal(uint64(0), resp.Sequence, "unknown account should have sequence 0")
}
