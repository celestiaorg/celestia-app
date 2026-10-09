//go:build benchmarks

package benchmarks_test

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/fibrefactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// pffCounts is the 5,800-PFF workload within the 256 square and 32 MiB block
// limit. The benchmarks build tag lifts the lower protocol message limit.
var pffCounts = []int{5800}

// pffValidatorCounts is the validator-set sweep. 100 matches every prior fleet
// run; 50 halves the quorum, which is what the per-message cost should track.
var pffValidatorCounts = []int{50, 100}

const (
	// defaultPFFValidators is the validator set the nPFF sweep runs against.
	defaultPFFValidators = 100
	// validatorSweepPFFCount is the single nPFF point the validator sweep uses.
	// A 50-validator certificate is smaller than a 100-validator one, so the
	// ceiling measured at 100 fits both.
	validatorSweepPFFCount = 5848
	// checkTxPoolSize is how many distinct transactions BenchmarkCheckTx_PFF
	// can admit before it has to rebuild the CheckTx state.
	checkTxPoolSize = 2000
)

// pffBlock is a committed app plus a full, valid PayForFibre proposal over it.
type pffBlock struct {
	app     *app.App
	fixture *fibrefactory.Fixture
	// rawTxs is what a proposer reads out of its mempool.
	rawTxs [][]byte
	// txs is what PrepareProposal made of them.
	txs          [][]byte
	height       int64
	blockTime    time.Time
	squareSize   uint64
	dataRootHash []byte
	// warmed records that the caches needed by a warm benchmark are populated.
	// CheckTx admission is not repeatable: the sequence check rejects a replay.
	warmed bool
}

// newPFFBlock builds count PayForFibre transactions against a validators-strong
// set and runs them through PrepareProposal, so every benchmark measures the
// proposal a proposer would really produce.
func newPFFBlock(b *testing.B, count, validators int) *pffBlock {
	fixture := fibrefactory.NewFixture(b, count, validators)
	rawTxs := fixture.Txs(b, fixture.Quorum(b))
	height := fixture.App.LastBlockHeight() + 1

	resp, err := fixture.App.PrepareProposal(&abci.RequestPrepareProposal{
		Height: height,
		Time:   fixture.BlockTime,
		Txs:    rawTxs,
	})
	require.NoError(b, err)
	if len(resp.Txs) != count {
		// Not require.Len: it would print every transaction in the proposal.
		b.Fatalf("proposal took %d of %d PayForFibre txs; it does not fit a %d square",
			len(resp.Txs), count, appconsts.DefaultGovMaxSquareSize)
	}

	block := &pffBlock{
		app:          fixture.App,
		fixture:      fixture,
		rawTxs:       rawTxs,
		txs:          resp.Txs,
		height:       height,
		blockTime:    fixture.BlockTime,
		squareSize:   resp.SquareSize,
		dataRootHash: resp.DataRootHash,
	}

	// A non-proposing validator has none of the proposer's cached artifacts.
	// Drop them without doing an extra, untimed ProcessProposal.
	block.app.PurgeNodeCaches()
	return block
}

// lazyPFFBlock builds its block on first use, so a filtered -bench run does not
// pay for fixtures it never measures.
type lazyPFFBlock struct {
	count      int
	validators int
	block      *pffBlock
}

func newLazyPFFBlock(count, validators int) *lazyPFFBlock {
	return &lazyPFFBlock{count: count, validators: validators}
}

func (l *lazyPFFBlock) get(b *testing.B) *pffBlock {
	if l.block == nil {
		l.block = newPFFBlock(b, l.count, l.validators)
	}
	return l.block
}

func (p *pffBlock) prepareRequest() *abci.RequestPrepareProposal {
	return &abci.RequestPrepareProposal{Height: p.height, Time: p.blockTime, Txs: p.rawTxs}
}

func (p *pffBlock) processRequest() *abci.RequestProcessProposal {
	return &abci.RequestProcessProposal{
		Height:       p.height,
		Time:         p.blockTime,
		Txs:          p.txs,
		SquareSize:   p.squareSize,
		DataRootHash: p.dataRootHash,
	}
}

func (p *pffBlock) finalizeRequestFor(testApp *app.App) *abci.RequestFinalizeBlock {
	return &abci.RequestFinalizeBlock{
		Height: p.height,
		Time:   p.blockTime,
		Txs:    p.txs,
		Hash:   testApp.LastCommitID().Hash,
	}
}

// newExecutedApp returns a fresh app with the proposal processed but not yet
// finalized, which is the state a validator is in when it calls FinalizeBlock.
// FinalizeBlock writes into the root store, so it cannot be measured twice on
// one app. cold drops everything ProcessProposal cached, which is the state a
// node replaying history is in: it never ran ProcessProposal for these blocks.
func (p *pffBlock) newExecutedApp(b *testing.B, cold bool) *app.App {
	testApp := p.fixture.NewApp(b)
	mustAccept(b, testApp, p.processRequest())
	if cold {
		testApp.PurgeNodeCaches()
	}
	return testApp
}

// warm verifies every transaction through CheckTx, the way a node with a
// healthy mempool would have done before the block arrived. The signature cache
// then holds every certificate, so the proposal paths are pure cache hits.
func (p *pffBlock) warm(b *testing.B) {
	if p.warmed {
		return
	}
	p.app.PurgeNodeCaches()
	for _, rawTx := range p.rawTxs {
		resp, err := p.app.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(b, err)
		require.Equal(b, uint32(0), resp.Code, "%s: %s", resp.Codespace, resp.Log)
	}
	p.warmed = true
}

// resetCheckState commits an empty block, which rebuilds the CheckTx state from
// committed state so the same transactions can be admitted again. It also drops
// the signature cache, because a transaction arriving at a mempool for the
// first time is always a cache miss.
func (p *pffBlock) resetCheckState(b *testing.B) {
	_, err := p.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: p.app.LastBlockHeight() + 1,
		Time:   p.blockTime,
		Hash:   p.app.LastCommitID().Hash,
	})
	require.NoError(b, err)
	_, err = p.app.Commit()
	require.NoError(b, err)
	p.app.PurgeNodeCaches()
}

// BenchmarkCheckTx_PFF measures per-transaction admission cost, which sets the
// rate at which a node can take PayForFibre in from its mempool. Every
// iteration admits a distinct transaction: the sequence check rejects a replay.
func BenchmarkCheckTx_PFF(b *testing.B) {
	for _, validators := range pffValidatorCounts {
		lazy := newLazyPFFBlock(checkTxPoolSize, validators)
		b.Run(fmt.Sprintf("validators=%d", validators), func(b *testing.B) {
			block := lazy.get(b)
			block.resetCheckState(b)
			next := 0
			for b.Loop() {
				if next == len(block.rawTxs) {
					b.StopTimer()
					block.resetCheckState(b)
					next = 0
					b.StartTimer()
				}
				resp, err := block.app.CheckTx(&abci.RequestCheckTx{
					Tx:   block.rawTxs[next],
					Type: abci.CheckTxType_New,
				})
				if err != nil || resp.Code != 0 {
					b.Fatalf("CheckTx rejected tx %d: %v %s", next, err, resp.GetLog())
				}
				next++
			}
			reportPFFMetrics(b, 1)
		})
	}
}

// BenchmarkPrepareProposal_PFF measures the proposer path.
func BenchmarkPrepareProposal_PFF(b *testing.B) {
	for _, count := range pffCounts {
		lazy := newLazyPFFBlock(count, defaultPFFValidators)
		benchmarkPFFCached(b, lazy, false, func(b *testing.B, block *pffBlock) func() {
			req := block.prepareRequest()
			return func() {
				resp, err := block.app.PrepareProposal(req)
				if err != nil || len(resp.Txs) != lazy.count {
					b.Fatalf("PrepareProposal dropped transactions: %v %d", err, len(resp.Txs))
				}
			}
		})
	}
}

// BenchmarkProcessProposal_PFF measures the term every validator pays on the
// critical path.
func BenchmarkProcessProposal_PFF(b *testing.B) {
	for _, count := range pffCounts {
		lazy := newLazyPFFBlock(count, defaultPFFValidators)
		benchmarkPFFCached(b, lazy, true, func(b *testing.B, block *pffBlock) func() {
			req := block.processRequest()
			return func() { mustAccept(b, block.app, req) }
		})
	}
}

// BenchmarkProcessProposal_PFF_Validators confirms the per-message cost tracks
// the quorum, which is what sets the PayForFibre ceiling as the set grows.
func BenchmarkProcessProposal_PFF_Validators(b *testing.B) {
	for _, validators := range pffValidatorCounts {
		lazy := newLazyPFFBlock(validatorSweepPFFCount, validators)
		b.Run(fmt.Sprintf("validators=%d/cache=cold", validators), func(b *testing.B) {
			block := lazy.get(b)
			req := block.processRequest()
			for b.Loop() {
				b.StopTimer()
				block.app.PurgeNodeCaches()
				b.StartTimer()
				mustAccept(b, block.app, req)
			}
			reportPFFMetrics(b, validatorSweepPFFCount)
		})
	}
}

// BenchmarkFinalizeBlock_PFF measures execution and state writes. FinalizeBlock
// never verifies certificates, so its cold and warm cases differ only by the
// payment promise checks: warm is a validator that just ran ProcessProposal,
// cold is a node replaying history, which never did.
func BenchmarkFinalizeBlock_PFF(b *testing.B) {
	for _, count := range pffCounts {
		lazy := newLazyPFFBlock(count, defaultPFFValidators)
		for _, state := range []string{"cold", "warm"} {
			b.Run(fmt.Sprintf("pff=%d/cache=%s", count, state), func(b *testing.B) {
				block := lazy.get(b)
				for b.Loop() {
					b.StopTimer()
					testApp := block.newExecutedApp(b, state == "cold")
					req := block.finalizeRequestFor(testApp)
					b.StartTimer()

					resp, err := testApp.FinalizeBlock(req)

					b.StopTimer()
					require.NoError(b, err)
					requireTxsSucceeded(b, resp)
					b.StartTimer()
				}
				reportPFFMetrics(b, lazy.count)
			})
		}
	}
}

// BenchmarkCommit_PFF measures the app side of the commit tail. It runs on
// memDB, so it does not see the disk cost that dominates on a real node.
// Committing settles the promises, so every iteration needs a fresh app.
func BenchmarkCommit_PFF(b *testing.B) {
	for _, count := range pffCounts {
		lazy := newLazyPFFBlock(count, defaultPFFValidators)
		b.Run(fmt.Sprintf("pff=%d", count), func(b *testing.B) {
			block := lazy.get(b)
			for b.Loop() {
				b.StopTimer()
				testApp := block.newExecutedApp(b, false)
				resp, err := testApp.FinalizeBlock(block.finalizeRequestFor(testApp))
				require.NoError(b, err)
				requireTxsSucceeded(b, resp)
				b.StartTimer()

				if _, err := testApp.Commit(); err != nil {
					b.Fatal(err)
				}
			}
			reportPFFMetrics(b, lazy.count)
		})
	}
}

// benchmarkPFFCached runs call over both cache states. The gap between them is
// what the signature caching work is worth, measured on one binary.
func benchmarkPFFCached(b *testing.B, lazy *lazyPFFBlock, warmFromCold bool, bind func(*testing.B, *pffBlock) func()) {
	b.Run(fmt.Sprintf("pff=%d/cache=cold", lazy.count), func(b *testing.B) {
		block := lazy.get(b)
		call := bind(b, block)
		for b.Loop() {
			b.StopTimer()
			block.app.PurgeNodeCaches()
			b.StartTimer()
			call()
		}
		if warmFromCold {
			// ProcessProposal populated every cache it reads. Reuse the last
			// cold call's cache state instead of admitting 5,800 txs again.
			block.warmed = true
		}
		reportPFFMetrics(b, lazy.count)
	})
	b.Run(fmt.Sprintf("pff=%d/cache=warm", lazy.count), func(b *testing.B) {
		block := lazy.get(b)
		call := bind(b, block)
		if warmFromCold {
			if !block.warmed {
				// A warm-only -bench filter skipped the cold sub-benchmark.
				mustAccept(b, block.app, block.processRequest())
			}
		} else {
			block.warm(b)
		}
		for b.Loop() {
			call()
		}
		reportPFFMetrics(b, lazy.count)
	})
}

// reportPFFMetrics adds the per-block and per-message views of ns/op, plus the
// core count the run was allowed. Benchmarks are pinned with taskset rather
// than -cpu, because the parallel verifier sizes itself from runtime.NumCPU.
func reportPFFMetrics(b *testing.B, count int) {
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	b.ReportMetric(nsPerOp/1e6, "ms/block")
	b.ReportMetric(nsPerOp/float64(count)/1e3, "us/PFF")
	b.ReportMetric(float64(runtime.NumCPU()), "cores")
}

func mustAccept(b *testing.B, testApp *app.App, req *abci.RequestProcessProposal) {
	resp, err := testApp.ProcessProposal(req)
	if err != nil {
		b.Fatal(err)
	}
	if resp.Status != abci.ResponseProcessProposal_ACCEPT {
		b.Fatalf("proposal rejected: %v", resp.Status)
	}
}

// requireTxsSucceeded fails if any transaction errored, which would mean the
// benchmark is measuring rejections instead of settlement.
func requireTxsSucceeded(b *testing.B, resp *abci.ResponseFinalizeBlock) {
	for i, result := range resp.TxResults {
		if result.Code != 0 {
			b.Fatalf("tx %d failed: %s: %s", i, result.Codespace, result.Log)
		}
	}
}
