package app_test

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalCertificatePassPanics covers the certificate pass that runs
// alongside the walk: a panic it lets escape rejects the block, and a walk that
// rejects or panics first still waits for it.
func TestProcessProposalCertificatePassPanics(t *testing.T) {
	type walk int
	const (
		walkAccepts walk = iota
		walkRejects
		walkPanics
	)
	for _, tc := range []struct {
		name         string
		walk         walk
		verifyPanics bool
		want         abci.ResponseProcessProposal_ProposalStatus
	}{
		{"valid block", walkAccepts, false, abci.ResponseProcessProposal_ACCEPT},
		{"valid block, verification panics", walkAccepts, true, abci.ResponseProcessProposal_REJECT},
		{"walk rejects first", walkRejects, false, abci.ResponseProcessProposal_REJECT},
		{"walk rejects first, verification panics", walkRejects, true, abci.ResponseProcessProposal_REJECT},
		{"walk panics first", walkPanics, false, abci.ResponseProcessProposal_REJECT},
		{"walk panics first, verification panics", walkPanics, true, abci.ResponseProcessProposal_REJECT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &proposalLog{}
			testApp, txs := newMixedBlockLogged(t, logs, 1, 1) // [MsgSend, PayForFibre]
			certKey := fibrekeeper.DecodePayForFibre(txs[1]).CertKey

			walkFailed := make(chan struct{})
			if tc.walk == walkAccepts {
				close(walkFailed)
			} else {
				// The walk fails at tx 0, before it reaches the PayForFibre.
				txs = slices.Clone(txs)
				txs[0] = withCorruptTxSignature(t, txs[0])
				logs.onReject = func() {
					close(walkFailed)
					if tc.walk == walkPanics {
						panic("walk panic")
					}
				}
			}

			// Only the pass records certificate keys through the keeper's cache, so this
			// hook runs inside it: the pass cannot finish before the walk has failed.
			// Plain bool: under -race, a handler that did not wait would race on it.
			verified := false
			useFibreSigCache(testApp, hookedSigCache{Cache: sigcache.New(1024), onAdd: func(key sigcache.Key) {
				if key != certKey {
					return
				}
				<-walkFailed
				verified = true
				if tc.verifyPanics {
					panic("verification panic")
				}
			}})

			resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, txs))
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.Status)
			require.True(t, verified, "ProcessProposal returned before certificate verification finished")
			out := logs.String()
			require.NotContains(t, out, "panic recovered in ProcessProposal", "a panic escaped the handler")
			if tc.verifyPanics || tc.walk == walkPanics {
				require.Contains(t, out, "caught panic", "the handler must recover the panic itself")
			}
		})
	}
}

// TestProcessProposalWarmCertificate covers a proposal whose certificate and promise
// are cached: the pass has no work and starts no goroutine, yet tx signatures still decide.
func TestProcessProposalWarmCertificate(t *testing.T) {
	testApp, txs := newMixedBlock(t, 1, 0, 0)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, preverifyProposalStatus(t, testApp, txs))

	tampered := withCorruptTxSignature(t, txs[0])
	d := fibrekeeper.DecodePayForFibre(tampered)
	require.True(t, testApp.HasVerifiedSignature(d.CertKey))
	require.True(t, testApp.HasVerifiedSignature(d.PromiseKey))
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: testApp.LastBlockHeight() + 1, Time: time.Now()})
	require.Nil(t, testApp.FibreKeeper.PreparePreverification(ctx, []*fibretypes.DecodedPayForFibre{d},
		fibrekeeper.PreverifyOptions{Certificates: true, StopOnFirstFailure: true}))

	require.Equal(t, abci.ResponseProcessProposal_REJECT, preverifyProposalStatus(t, testApp, [][]byte{tampered}))
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, preverifyProposalStatus(t, testApp, txs))
}

// preverifyProposalStatus uses the same wall-clock proposal time as newMixedBlock.
func preverifyProposalStatus(t *testing.T, testApp *app.App, txs [][]byte) abci.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, txs))
	require.NoError(t, err)
	return resp.Status
}

// hookedSigCache calls onAdd before every Add.
type hookedSigCache struct {
	*sigcache.Cache
	onAdd func(sigcache.Key)
}

func (c hookedSigCache) Add(key sigcache.Key) {
	c.onAdd(key)
	c.Cache.Add(key)
}

// useFibreSigCache rebuilds the fibre keeper around cache. The ante handler keeps
// the app's own cache, so the walk still verifies every certificate itself.
func useFibreSigCache(testApp *app.App, cache fibrekeeper.SigCache) {
	*testApp.FibreKeeper = *fibrekeeper.NewKeeper(testApp.AppCodec(), testApp.GetKey(fibretypes.StoreKey), testApp.GetTKey(fibretypes.TStoreKey),
		testApp.BankKeeper, proposalStakingKeeper{testApp.StakingKeeper, runtime.NewKVStoreService(testApp.GetKey(stakingtypes.StoreKey))},
		testApp.FibreKeeper.GetAuthority(), false, cache)
}

// proposalStakingKeeper mirrors the app's fibre staking adapter for the rebuilt keeper.
type proposalStakingKeeper struct {
	*stakingkeeper.Keeper
	storeService corestore.KVStoreService
}

func (k proposalStakingKeeper) HasHistoricalInfo(ctx context.Context, height int64) (bool, error) {
	return k.storeService.OpenKVStore(ctx).Has(stakingtypes.GetHistoricalInfoKey(height))
}

// newMixedBlockLogged is newMixedBlock with the app's error logs written to w.
func newMixedBlockLogged(t *testing.T, w *proposalLog, pff, send int) (*app.App, [][]byte) {
	t.Helper()
	prev := testutil.TestAppLogger
	testutil.TestAppLogger = log.NewLogger(w, log.ColorOption(false), log.LevelOption(zerolog.ErrorLevel))
	defer func() { testutil.TestAppLogger = prev }()
	return newMixedBlock(t, pff, 0, send)
}

// proposalLog records log output and calls onReject when the first proposal
// rejection is logged.
type proposalLog struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	once     sync.Once
	onReject func()
}

func (l *proposalLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf.Write(p)
	l.mu.Unlock()
	if l.onReject != nil && bytes.Contains(p, []byte("Rejected proposal block")) {
		l.once.Do(l.onReject)
	}
	return len(p), nil
}

func (l *proposalLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}
