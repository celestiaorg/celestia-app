package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

// TestFuzzPrepareProcessProposal produces blocks with random data using
// PrepareProposal and then tests those blocks by calling ProcessProposal. All
// blocks produced by PrepareProposal should be accepted by ProcessProposal. It
// doesn't use the standard go tools for fuzzing as those tools only support
// fuzzing limited types, which forces us to create random block data ourselves
// anyway. We also want to run this test alongside the other tests and not just
// when fuzzing.
func TestFuzzPrepareProcessProposal(t *testing.T) {
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer := types.GenerateKeyringSigner(t, types.TestAccName)
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	timer := time.After(time.Minute * 1)

	type test struct {
		count, size int
	}
	tests := []test{{10000, 400}, {100, -1}}

	for _, tt := range tests {
		for {
			select {
			case <-timer:
				return
			default:
				t.Run("randomized inputs to Prepare and Process Proposal", func(t *testing.T) {
					pfdTxs := app.GenerateManyRawWirePFB(t, encConf.TxConfig, signer, tmrand.Intn(tt.count), tt.size)
					txs := app.GenerateManyRawSendTxs(t, encConf.TxConfig, signer, tmrand.Intn(20))
					txs = append(txs, pfdTxs...)
					resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
						BlockData: &core.Data{
							Txs: txs,
						},
					})
					res := testApp.ProcessProposal(abci.RequestProcessProposal{
						BlockData: resp.BlockData,
						Header: core.Header{
							DataHash: resp.BlockData.Hash,
						},
					})
					require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Result)
				})
			}
		}
	}
}
