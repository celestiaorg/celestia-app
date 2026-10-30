package app_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/mempool"
	"github.com/cometbft/cometbft/mempool/cat"
	"github.com/cometbft/cometbft/proxy"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// recheckHarness drives a celestia-app instance with a real CAT pool the way
// BlockExecutor.Commit does: FinalizeBlock, then Commit and Update (recheck)
// under the mempool lock.
type recheckHarness struct {
	t      *testing.T
	app    *app.App
	pool   *cat.TxPool
	conn   proxy.AppConnMempool
	signer *user.Signer
	names  []string
	addrs  []sdk.AccAddress
}

func newRecheckHarness(t *testing.T, accounts int, wire bool) *recheckHarness {
	t.Helper()
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	names := testfactory.GenerateAccounts(accounts)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), names...)
	commitBlock(t, testApp)

	client, err := proxy.NewLocalClientCreator(server.NewCometABCIWrapper(testApp)).NewABCIClient()
	require.NoError(t, err)
	require.NoError(t, client.Start())
	t.Cleanup(func() { _ = client.Stop() })
	conn := proxy.NewAppConnMempool(client, proxy.NopMetrics())

	cfg := cmtcfg.TestMempoolConfig()
	cfg.Recheck = true
	cfg.RootDir = t.TempDir()
	pool := cat.NewTxPool(cmtlog.NewNopLogger(), cfg, conn, testApp.LastBlockHeight())
	if wire {
		testApp.SetPendingSequencer(pool)
	}

	signerAccounts := make([]*user.Account, accounts)
	addrs := make([]sdk.AccAddress, accounts)
	for i, info := range queryAccountInfo(testApp, names, kr) {
		signerAccounts[i] = user.NewAccount(names[i], info.AccountNum, info.Sequence)
		addrs[i] = testfactory.GetAddress(kr, names[i])
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, signerAccounts...)
	require.NoError(t, err)
	return &recheckHarness{t: t, app: testApp, pool: pool, conn: conn, signer: signer, names: names, addrs: addrs}
}

// sendTx signs a self-send from account i at the given sequence.
func (h *recheckHarness) sendTx(i int, seq uint64, opts ...user.TxOption) []byte {
	h.t.Helper()
	require.NoError(h.t, h.signer.SetSequence(h.names[i], seq))
	msg := banktypes.NewMsgSend(h.addrs[i], h.addrs[i], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	opts = append([]user.TxOption{user.SetGasLimit(100_000), user.SetFee(10_000)}, opts...)
	raw, _, err := h.signer.CreateTx([]sdk.Msg{msg}, opts...)
	require.NoError(h.t, err)
	return raw
}

func (h *recheckHarness) addTx(raw []byte) error {
	tx := coretypes.Tx(raw)
	rsp, err := h.pool.TryAddNewTx(tx.ToCachedTx(), tx.Key(), mempool.TxInfo{})
	if err != nil {
		return err
	}
	if rsp.Code != abci.CodeTypeOK {
		return fmt.Errorf("CheckTx code %d: %s", rsp.Code, rsp.Log)
	}
	return nil
}

func (h *recheckHarness) querySequence(i int) (uint64, error) {
	rsp, err := h.conn.QuerySequence(context.Background(), &abci.RequestQuerySequence{Signer: h.addrs[i]})
	if err != nil {
		return 0, err
	}
	return rsp.Sequence, nil
}

// commit executes and commits txs, then updates the pool, rechecking the rest.
// onRecheck, if set, flags the window in which Commit and recheck run.
func (h *recheckHarness) commit(onRecheck *atomic.Bool, txs ...[]byte) error {
	resp, err := h.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: h.app.LastBlockHeight() + 1,
		Time:   time.Now(),
		Hash:   h.app.LastCommitID().Hash,
		Txs:    txs,
	})
	if err != nil {
		return err
	}
	h.pool.Lock()
	defer h.pool.Unlock()
	if onRecheck != nil {
		onRecheck.Store(true)
		defer onRecheck.Store(false)
	}
	if _, err := h.app.Commit(); err != nil {
		return err
	}
	return h.pool.Update(h.app.LastBlockHeight(), coretypes.CachedTxFromTxs(coretypes.ToTxs(txs)), resp.TxResults, nil, nil)
}

// A burst from one signer is committed in part and the rest rechecked while
// QuerySequence and Simulate run: the sequence must never drop and
// simulation must never fail on the sequence.
func TestQuerySequenceMonotonicDuringRecheck(t *testing.T) {
	h := newRecheckHarness(t, 1, true)
	start := testutil.DirectQueryAccount(h.app, h.addrs[0]).GetSequence()

	const burst, perBlock = 300, 60
	txs := make([][]byte, burst)
	for i := range txs {
		txs[i] = h.sendTx(0, start+uint64(i))
		require.NoError(t, h.addTx(txs[i]))
	}
	next := start + burst
	seq, err := h.querySequence(0)
	require.NoError(t, err)
	require.Equal(t, next, seq)

	probe := h.sendTx(0, next)
	baseline, _, err := h.app.Simulate(probe)
	require.NoError(t, err)

	var (
		stop, inRecheck             atomic.Bool
		recheckSamples, recheckSims atomic.Int64
		wg                          sync.WaitGroup
		queryErr, simErr, backwards atomic.Value
	)
	wg.Go(func() {
		highest := next
		for !stop.Load() {
			during := inRecheck.Load()
			seq, err := h.querySequence(0)
			if err != nil {
				queryErr.Store(err)
				return
			}
			if seq < highest {
				backwards.Store(fmt.Sprintf("QuerySequence went from %d to %d", highest, seq))
				return
			}
			highest = seq
			if during {
				recheckSamples.Add(1)
			}
		}
	})
	wg.Go(func() {
		for !stop.Load() {
			during := inRecheck.Load()
			gas, _, err := h.app.Simulate(probe)
			if err != nil {
				simErr.Store(err)
				return
			}
			if gas.GasUsed > baseline.GasUsed+baseline.GasUsed/100 || gas.GasUsed+baseline.GasUsed/100 < baseline.GasUsed {
				simErr.Store(fmt.Errorf("simulated gas %d, baseline %d", gas.GasUsed, baseline.GasUsed))
				return
			}
			if during {
				recheckSims.Add(1)
			}
		}
	})

	for committed := 0; committed < burst-perBlock; committed += perBlock {
		require.NoError(t, h.commit(&inRecheck, txs[committed:committed+perBlock]...))
	}
	stop.Store(true)
	wg.Wait()

	for _, v := range []*atomic.Value{&queryErr, &simErr, &backwards} {
		if got := v.Load(); got != nil {
			t.Error(got)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Logf("samples inside Commit+recheck: %d QuerySequence, %d Simulate", recheckSamples.Load(), recheckSims.Load())
	require.Positive(t, recheckSamples.Load(), "no QuerySequence sample landed inside recheck")
	require.Positive(t, recheckSims.Load(), "no Simulate call landed inside recheck")
	seq, err = h.querySequence(0)
	require.NoError(t, err)
	require.Equal(t, next, seq)
}

// A pending tx evicted after Commit no longer counts: QuerySequence falls back
// to the committed sequence.
func TestQuerySequenceAfterEviction(t *testing.T) {
	h := newRecheckHarness(t, 1, true)
	start := testutil.DirectQueryAccount(h.app, h.addrs[0]).GetSequence()
	height := uint64(h.app.LastBlockHeight())

	// The last two txs expire at the next height, so recheck evicts them.
	txs := make([][]byte, 0, 5)
	for i := range uint64(5) {
		var opts []user.TxOption
		if i >= 3 {
			opts = append(opts, user.SetTimeoutHeight(height))
		}
		txs = append(txs, h.sendTx(0, start+i, opts...))
		require.NoError(t, h.addTx(txs[i]))
	}
	seq, err := h.querySequence(0)
	require.NoError(t, err)
	require.Equal(t, start+5, seq)

	require.NoError(t, h.commit(nil, txs[:2]...))
	require.Equal(t, 1, h.pool.Size(), "recheck should evict the expired txs")
	seq, err = h.querySequence(0)
	require.NoError(t, err)
	require.Equal(t, start+3, seq)

	// Evicting the last pending tx leaves only the committed sequence.
	require.NoError(t, h.pool.RemoveTxByKey(coretypes.Tx(txs[2]).Key()))
	seq, err = h.querySequence(0)
	require.NoError(t, err)
	require.Equal(t, start+2, seq)
}

// Commit, CheckTx (admission and recheck), Simulate and QuerySequence run
// concurrently; meant for -race. Both QuerySequence paths are covered.
func TestQuerySequenceConcurrentWithBlocks(t *testing.T) {
	for _, wire := range []bool{true, false} {
		t.Run(fmt.Sprintf("mempool=%t", wire), func(t *testing.T) {
			const accounts, perAccount = 3, 40
			h := newRecheckHarness(t, accounts, wire)
			txs := make([][][]byte, accounts)
			for i := range txs {
				start := testutil.DirectQueryAccount(h.app, h.addrs[i]).GetSequence()
				for s := range uint64(perAccount) {
					txs[i] = append(txs[i], h.sendTx(i, start+s))
				}
			}
			probe := h.sendTx(0, 0)

			var stop atomic.Bool
			var readers sync.WaitGroup
			// Two QuerySequence readers share the cached committed context.
			for range 2 {
				readers.Go(func() {
					for !stop.Load() {
						for i := range h.addrs {
							if _, err := h.querySequence(i); err != nil {
								t.Error(err)
								return
							}
							if _, err := h.app.QuerySequence(context.Background(), &abci.RequestQuerySequence{Signer: h.addrs[i]}); err != nil {
								t.Error(err)
								return
							}
						}
					}
				})
			}
			readers.Go(func() {
				for !stop.Load() {
					_, _, _ = h.app.Simulate(probe)
				}
			})

			var writers sync.WaitGroup
			for i := range txs {
				writers.Go(func() {
					for _, tx := range txs[i] {
						if err := h.addTx(tx); err != nil {
							t.Error(err)
							return
						}
					}
				})
			}
			for range 6 {
				reaped := h.pool.ReapMaxTxs(30)
				block := make([][]byte, len(reaped))
				for j, tx := range reaped {
					block[j] = tx.Tx
				}
				require.NoError(t, h.commit(nil, block...))
			}
			writers.Wait()
			stop.Store(true)
			readers.Wait()
		})
	}
}

// hookSequencer runs hook once, before reading the pool, to land a Commit and
// Update between QuerySequence's mempool and committed-state reads.
type hookSequencer struct {
	pool *cat.TxPool
	hook func()
}

func (s *hookSequencer) HighestPendingSequence(signer []byte) (uint64, bool) {
	if f := s.hook; f != nil {
		s.hook = nil
		f()
	}
	return s.pool.HighestPendingSequence(signer)
}

// QuerySequence reads the mempool before committed state, so a Commit that
// empties the signer's pending set between the reads cannot lower the value.
func TestQuerySequenceReadOrderAcrossCommit(t *testing.T) {
	h := newRecheckHarness(t, 1, false)
	hs := &hookSequencer{pool: h.pool}
	h.app.SetPendingSequencer(hs)
	start := testutil.DirectQueryAccount(h.app, h.addrs[0]).GetSequence()
	txs := [][]byte{h.sendTx(0, start), h.sendTx(0, start+1)}
	for _, tx := range txs {
		require.NoError(t, h.addTx(tx))
	}
	seq, err := h.querySequence(0)
	require.NoError(t, err)
	require.Equal(t, start+2, seq)

	hs.hook = func() { require.NoError(t, h.commit(nil, txs...)) }
	rsp, err := h.app.QuerySequence(context.Background(), &abci.RequestQuerySequence{Signer: h.addrs[0]})
	require.NoError(t, err)
	require.Equal(t, start+2, rsp.Sequence)
}

// Simulate accepts only the signer's next sequence while checkState lags; a
// used or skipped sequence still fails, so clients can resync from the error.
func TestSimulateSequenceWhileRecheckLags(t *testing.T) {
	h := newRecheckHarness(t, 1, true)
	start := testutil.DirectQueryAccount(h.app, h.addrs[0]).GetSequence()
	txs := make([][]byte, 20)
	for i := range txs {
		txs[i] = h.sendTx(0, start+uint64(i))
		require.NoError(t, h.addTx(txs[i]))
	}
	next := start + uint64(len(txs))

	// Commit without Update: checkState is back at start+5, the pool still holds all.
	resp, err := h.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: h.app.LastBlockHeight() + 1, Time: time.Now(), Hash: h.app.LastCommitID().Hash, Txs: txs[:5],
	})
	require.NoError(t, err)
	h.pool.Lock()
	defer h.pool.Unlock()
	_, err = h.app.Commit()
	require.NoError(t, err)

	_, _, err = h.app.Simulate(h.sendTx(0, next))
	require.NoError(t, err)
	for _, seq := range []uint64{start, start + 10, next + 1} {
		_, _, err = h.app.Simulate(h.sendTx(0, seq))
		require.ErrorIs(t, err, sdkerrors.ErrWrongSequence, "sequence %d", seq)
	}
	require.NoError(t, h.pool.Update(h.app.LastBlockHeight(), coretypes.CachedTxFromTxs(coretypes.ToTxs(txs[:5])), resp.TxResults, nil, nil))
}
