package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// TestPrepareProposalConsistency produces blocks with random data using
// PrepareProposal and then tests those blocks by calling ProcessProposal. All
// blocks produced by PrepareProposal should be accepted by ProcessProposal. It
// doesn't use the standard go tools for fuzzing as those tools only support
// fuzzing limited types, instead we create blocks our selves using random
// transactions.
func TestPrepareProposalConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestPrepareProposalConsistency in short mode.")
	}
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := make([]string, 10000)
	for i := range accounts {
		accounts[i] = tmrand.Str(20)
	}

	testApp, kr := testutil.SetupTestAppWithGenesisValSet(accounts...)

	type test struct {
		name                   string
		count, blobCount, size int
	}
	tests := []test{
		{"many small single share single blob transactions", 10000, 1, 400},
		{"one hundred normal sized single blob transactions", 100, 1, 400000},
		{"many single share multi-blob transactions", 1000, 100, 400},
		{"one hundred normal sized multi-blob transactions", 100, 4, 400000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timer := time.After(time.Second * 30)
			for {
				select {
				case <-timer:
					return
				default:
					ProcessRandomProposal(t, tt.count, tt.size, tt.blobCount, encConf, testApp, accounts, kr, "")
				}
			}
		})
	}
}

func ProcessRandomProposal(
	t *testing.T,
	count,
	maxSize int,
	maxBlobCount int,
	cfg encoding.Config,
	capp *app.App,
	accounts []string,
	kr keyring.Keyring,
	chainid string,
) {
	txs := testutil.RandBlobTxsWithAccounts(
		t,
		capp,
		cfg.TxConfig.TxEncoder(),
		kr,
		maxSize,
		maxBlobCount,
		true,
		chainid,
		accounts[:count],
	)
	sendTxs := blobfactory.GenerateManyRawSendTxsWithAccounts(
		cfg.TxConfig,
		kr,
		accounts[:count],
		chainid,
	)
	txs = append(txs, sendTxs...)
	resp := capp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &core.Data{
			Txs: coretypes.Txs(txs).ToSliceOfBytes(),
		},
	})
	res := capp.ProcessProposal(abci.RequestProcessProposal{
		BlockData: resp.BlockData,
		Header: core.Header{
			DataHash: resp.BlockData.Hash,
		},
	})
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Result)
}
