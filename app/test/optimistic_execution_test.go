package app_test

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// oePair holds two production-config apps on the same genesis: a takes the
// validator path (ProcessProposal, then a FinalizeBlock served by optimistic
// execution), b the replay path (FinalizeBlock alone).
type oePair struct {
	t         *testing.T
	a, b      *app.App
	proposer  []byte
	valHash   []byte
	accounts  []string
	next      int
	newSigner func(int) *user.Signer
	now       time.Time
}

func newOEPair(t *testing.T, numAccounts int) *oePair {
	t.Helper()
	accounts := testfactory.GenerateAccounts(numAccounts)
	cparams := app.DefaultConsensusParams()
	a := testutil.NewTestApp()
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(a, accounts...)
	a = testutil.InitialiseTestAppWithGenesis(a, cparams, genesis)
	b := testutil.InitialiseTestAppWithGenesis(testutil.NewTestApp(), cparams, genesis)
	for _, testApp := range []*app.App{a, b} {
		// Height 1 never runs optimistically; escrow is written straight into state.
		_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:               testutil.GenesisTime,
			Height:             1,
			Hash:               testApp.LastCommitID().Hash,
			NextValidatorsHash: valSet.Hash(),
		})
		require.NoError(t, err)
		_, err = testApp.Commit()
		require.NoError(t, err)
		for _, account := range accounts {
			seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), pffTestEscrowAmount)
		}
	}
	require.Equal(t, a.LastCommitID().Hash, b.LastCommitID().Hash)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	return &oePair{
		t:         t,
		a:         a,
		b:         b,
		proposer:  valSet.Validators[0].Address,
		valHash:   valSet.Hash(),
		accounts:  accounts,
		newSigner: newSignerFactory(t, kr, enc.TxConfig, accounts, queryAccountInfo(a, accounts, kr)),
		now:       time.Now(),
	}
}

// mixedTxs builds two BlobTxs (three blobs each, one over 10 KiB) with an
// estimated gas limit, two MsgSends and two PFFs, each from a fresh account.
func (p *oePair) mixedTxs() [][]byte {
	p.t.Helper()
	r := rand.New(rand.NewSource(int64(p.next)))
	txs := make([][]byte, 0, 6)
	for range 2 {
		idx, signer := p.nextSigner()
		blobs := blobfactory.ManyRandBlobs(r, 500, 3_000, 12_000)
		msg, err := blobtypes.NewMsgPayForBlobs(signer.Account(p.accounts[idx]).Address().String(), appconsts.Version, blobs...)
		require.NoError(p.t, err)
		tx, _, err := signer.CreatePayForBlobs(p.accounts[idx], blobs, blobfactory.FeeTxOpts(blobtypes.DefaultEstimateGas(msg))...)
		require.NoError(p.t, err)
		txs = append(txs, tx)
	}
	for range 2 {
		idx, signer := p.nextSigner()
		from := signer.Account(p.accounts[idx]).Address()
		msg := banktypes.NewMsgSend(from, from, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)))
		tx, _, err := signer.CreateTx([]sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
		require.NoError(p.t, err)
		txs = append(txs, tx)
	}
	for range 2 {
		idx, signer := p.nextSigner()
		txs = append(txs, newSignedPayForFibreTx(p.t, signer, p.accounts[idx], true))
	}
	return txs
}

func (p *oePair) nextSigner() (int, *user.Signer) {
	idx := p.next
	p.next++
	require.Less(p.t, idx, len(p.accounts), "out of test accounts")
	return idx, p.newSigner(idx)
}

// propose runs PrepareProposal on b for the next height and returns the
// proposal, its height, block time and a deterministic block hash.
func (p *oePair) propose(txs [][]byte) (*abci.ResponsePrepareProposal, int64, time.Time, []byte) {
	p.t.Helper()
	height := p.b.LastBlockHeight() + 1
	blockTime := p.now.Add(time.Duration(height) * time.Second)
	resp, err := p.b.PrepareProposal(&abci.RequestPrepareProposal{Txs: txs, Height: height, Time: blockTime})
	require.NoError(p.t, err)
	require.Len(p.t, resp.Txs, len(txs))
	return resp, height, blockTime, blockHash(height, resp.DataRootHash)
}

func blockHash(height int64, dataRoot []byte) []byte {
	h := sha256.Sum256(append([]byte(fmt.Sprint(height)), dataRoot...))
	return h[:]
}

func (p *oePair) processProposal(prop *abci.ResponsePrepareProposal, height int64, blockTime time.Time, hash []byte) abci.ResponseProcessProposal_ProposalStatus {
	p.t.Helper()
	resp, err := p.a.ProcessProposal(&abci.RequestProcessProposal{
		Txs:                prop.Txs,
		SquareSize:         prop.SquareSize,
		DataRootHash:       prop.DataRootHash,
		Hash:               hash,
		Height:             height,
		Time:               blockTime,
		ProposerAddress:    p.proposer,
		NextValidatorsHash: p.valHash,
		Header:             blockHeader(height, blockTime),
	})
	require.NoError(p.t, err)
	return resp.Status
}

// finalize runs FinalizeBlock with the block's txs on both apps, requires the
// two responses to be identical and every tx to succeed, then commits both.
// With served, a's request carries a later time that only a speculative result ignores.
func (p *oePair) finalize(blockTxs [][]byte, height int64, blockTime time.Time, hash []byte, served bool) {
	p.t.Helper()
	txs := testutil.CoreFinalizeTxs(blockTxs)
	resps := make([]*abci.ResponseFinalizeBlock, 0, 2)
	for _, testApp := range []*app.App{p.a, p.b} {
		reqTime := blockTime
		if served && testApp == p.a {
			reqTime = blockTime.Add(time.Hour)
		}
		resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Txs:                txs,
			Hash:               hash,
			Height:             height,
			Time:               reqTime,
			ProposerAddress:    p.proposer,
			NextValidatorsHash: p.valHash,
			Header:             blockHeader(height, reqTime),
		})
		require.NoError(p.t, err)
		resps = append(resps, resp)
	}
	require.Equal(p.t, resps[1].AppHash, resps[0].AppHash, "height %d: app hash", height)
	require.Equal(p.t, resps[1], resps[0], "height %d: finalize block response", height)
	require.Len(p.t, resps[0].TxResults, len(txs))
	for i, result := range resps[0].TxResults {
		require.Equal(p.t, abci.CodeTypeOK, result.Code, "height %d tx %d: %s", height, i, result.Log)
	}
	for _, testApp := range []*app.App{p.a, p.b} {
		_, err := testApp.Commit()
		require.NoError(p.t, err)
	}
	require.Equal(p.t, p.b.LastCommitID().Hash, p.a.LastCommitID().Hash, "height %d: commit hash", height)
}

func blockHeader(height int64, blockTime time.Time) *cmtproto.Header {
	return &cmtproto.Header{ChainID: testutil.ChainID, Height: height, Time: blockTime}
}

// differentialHeight runs one block through the validator path on a and the
// replay path on b, requiring the proposal to be accepted.
func (p *oePair) differentialHeight() {
	p.t.Helper()
	prop, height, blockTime, hash := p.propose(p.mixedTxs())
	require.Equal(p.t, abci.ResponseProcessProposal_ACCEPT, p.processProposal(prop, height, blockTime, hash))
	p.finalize(prop.Txs, height, blockTime, hash, true)
}

// TestOptimisticExecutionMatchesReplay checks that a FinalizeBlock served by
// optimistic execution equals one computed without ProcessProposal, which is
// what a replaying or non-optimistic node computes.
func TestOptimisticExecutionMatchesReplay(t *testing.T) {
	p := newOEPair(t, 18)
	for range 3 {
		p.differentialHeight()
	}
	require.Equal(t, int64(4), p.a.LastBlockHeight())
}

// TestOptimisticExecutionDiscardedOnOtherBlock covers the abort paths: the
// decided block differs from the processed one, the same proposal is processed
// in two rounds, and a rejected proposal precedes FinalizeBlock.
func TestOptimisticExecutionDiscardedOnOtherBlock(t *testing.T) {
	p := newOEPair(t, 48)

	t.Run("different block decided", func(t *testing.T) {
		propX, height, blockTime, hashX := p.propose(p.mixedTxs())
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, p.processProposal(propX, height, blockTime, hashX))
		propY, _, _, hashY := p.propose(p.mixedTxs())
		require.NotEqual(t, hashX, hashY)
		p.finalize(propY.Txs, height, blockTime, hashY, false)
		p.differentialHeight()
	})

	t.Run("same proposal in two rounds", func(t *testing.T) {
		prop, height, blockTime, hash := p.propose(p.mixedTxs())
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, p.processProposal(prop, height, blockTime, hash))
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, p.processProposal(prop, height, blockTime, hash))
		p.finalize(prop.Txs, height, blockTime, hash, true)
	})

	t.Run("rejected proposal", func(t *testing.T) {
		prop, height, blockTime, hash := p.propose(p.mixedTxs())
		bad := *prop
		bad.DataRootHash = blockHash(height, nil)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, p.processProposal(&bad, height, blockTime, hash))
		p.finalize(prop.Txs, height, blockTime, hash, false)
	})

	t.Run("rejected proposal after an accepted one", func(t *testing.T) {
		prop, height, blockTime, hash := p.propose(p.mixedTxs())
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, p.processProposal(prop, height, blockTime, hash))
		bad := *prop
		bad.DataRootHash = blockHash(height, nil)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, p.processProposal(&bad, height, blockTime, blockHash(height, []byte("other"))))
		p.finalize(prop.Txs, height, blockTime, hash, false)
		p.differentialHeight()
	})
}
