package app_test

import (
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// querySequenceFixture is a test app in which one account has committed a tx,
// so its sequence is 1 rather than the 0 every unknown signer reports.
type querySequenceFixture struct {
	app    *app.App
	signer []byte
}

var (
	querySequenceOnce sync.Once
	querySequence     *querySequenceFixture
)

func newQuerySequenceFixture(t *testing.T) *querySequenceFixture {
	t.Helper()
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	sendTx := testutil.SendTxsWithAccounts(t, testApp, enc.TxConfig, kr, 1000, accounts[1], accounts[:1], testutil.ChainID)[0]
	resp := commitBlockAt(t, testApp, testutil.GenesisTime.Add(1), sendTx)
	require.Zero(t, resp.TxResults[0].Code, resp.TxResults[0].Log)
	return &querySequenceFixture{app: testApp, signer: testfactory.GetAddress(kr, accounts[0])}
}

func fuzzQuerySequence(t *testing.T, signer []byte) {
	t.Helper()
	querySequenceOnce.Do(func() { querySequence = newQuerySequenceFixture(t) })
	resp, err := querySequence.app.QuerySequence(t.Context(), &abci.RequestQuerySequence{Signer: signer})
	if err != nil {
		return
	}
	require.NotNil(t, resp)
	want := uint64(0)
	if string(signer) == string(querySequence.signer) {
		want = 1
	}
	require.Equal(t, want, resp.Sequence)
}

// TestQuerySequenceFuzzSeeds checks the account that committed a tx reports
// sequence 1, so the fuzz target compares against a live account.
func TestQuerySequenceFuzzSeeds(t *testing.T) {
	fuzzQuerySequence(t, nil)
	resp, err := querySequence.app.QuerySequence(t.Context(), &abci.RequestQuerySequence{Signer: querySequence.signer})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.Sequence)
}

// FuzzQuerySequence sends arbitrary signer bytes to QuerySequence. The CAT
// mempool asks it about signers named by peers, on the ABCI mempool
// connection where nothing recovers a panic, so any input must return a
// sequence or an error. Signers without an account report sequence 0 and the
// account that committed a tx reports 1.
func FuzzQuerySequence(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 20))
	f.Add(make([]byte, 32))
	f.Add(make([]byte, 255))
	f.Add(make([]byte, 256))
	f.Add(make([]byte, 1<<16))
	f.Fuzz(fuzzQuerySequence)
}
