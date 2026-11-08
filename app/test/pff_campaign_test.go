//go:build pffcampaign

package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	core "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

type campaignSpec struct {
	Duration time.Duration
	Finished time.Time
}

type campaignLogger struct {
	log.Logger
	finished chan campaignSpec
}

func (l campaignLogger) With(...any) log.Logger { return l }
func (l campaignLogger) Debug(msg string, args ...any) {
	if msg != "OE finished" {
		return
	}
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "duration" {
			d, err := time.ParseDuration(fmt.Sprint(args[i+1]))
			if err == nil {
				l.finished <- campaignSpec{Duration: d, Finished: time.Now()}
			}
			return
		}
	}
}

type campaignFixture struct {
	dir     string
	txs     [][]byte
	vals    *core.ValidatorSet
	now     time.Time
	initial []byte
}

func campaignApp(t *testing.T, dir string, optimistic bool) (*app.App, chan campaignSpec) {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, dir)
	require.NoError(t, err)
	done := make(chan campaignSpec, 4)
	opts := []func(*baseapp.BaseApp){baseapp.SetChainID(testutil.ChainID), baseapp.SetIAVLCacheSize(781250), baseapp.SetInterBlockCache(store.NewCommitKVStoreCacheManager())}
	if !optimistic {
		opts = append(opts, baseapp.DisableOptimisticExecution())
	}
	a := app.New(campaignLogger{Logger: log.NewNopLogger(), finished: done}, db, nil, 0, 0, testutil.EmptyAppOptions{}, opts...)
	_, err = a.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	return a, done
}
func newCampaignFixture(t *testing.T, n int) *campaignFixture {
	t.Helper()
	f := &campaignFixture{dir: t.TempDir(), now: testutil.GenesisTime.Add(time.Minute)}
	keys := make(map[string]ed25519.PrivKey)
	vals := make([]*core.Validator, 100)
	for i := range vals {
		key := ed25519.GenPrivKeyFromSecret([]byte(fmt.Sprintf("pff-campaign-validator-%d", i)))
		keys[string(key.PubKey().Bytes())] = key
		vals[i] = core.NewValidator(key.PubKey(), 1)
	}
	f.vals = core.NewValidatorSet(vals)
	var fixturePath string
	if parent := os.Getenv("PFF_FIXTURES"); parent != "" {
		fixturePath = filepath.Join(parent, strconv.Itoa(n)+"-"+os.Getenv("PFF_VARIANT"), "fixture.json")
		f.dir = filepath.Join(filepath.Dir(fixturePath), "database")
		if bz, err := os.ReadFile(fixturePath); err == nil {
			var saved struct {
				Txs     [][]byte
				Initial []byte
			}
			require.NoError(t, json.Unmarshal(bz, &saved))
			require.Len(t, saved.Txs, n)
			f.txs, f.initial = saved.Txs, saved.Initial
			return f
		} else {
			require.True(t, os.IsNotExist(err), "%v", err)
		}
		require.NoError(t, os.MkdirAll(f.dir, 0755))
	}
	a, _ := campaignApp(t, f.dir, false)

	names := testfactory.GenerateDeterministicAccounts(n)
	genesis, kr := testutil.CampaignGenesis(a, f.vals, names...)
	a = testutil.InitialiseTestAppWithGenesis(a, app.DefaultConsensusParams(), genesis)
	_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: testutil.GenesisTime, NextValidatorsHash: f.vals.Hash()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	for _, name := range names {
		seedFibreEscrow(t, a, testfactory.GetAddress(kr, name), 10_000_000)
	}
	_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: f.now, NextValidatorsHash: f.vals.Hash()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	ctx := a.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: 2, Time: f.now})
	history, err := a.StakingKeeper.GetHistoricalInfo(ctx, 1)
	require.NoError(t, err)
	require.Len(t, history.Valset, 100)
	ordered := make([]ed25519.PrivKey, 100)
	for i, v := range history.Valset {
		pub, err := v.ConsPubKey()
		require.NoError(t, err)
		ordered[i] = keys[string(pub.Bytes())]
		require.NotNil(t, ordered[i])
	}
	accounts := make([]*user.Account, n)
	for i, name := range names {
		acc := testutil.DirectQueryAccount(a, testfactory.GetAddress(kr, name))
		accounts[i] = user.NewAccount(name, acc.GetAccountNumber(), acc.GetSequence())
	}
	signer, err := user.NewSigner(kr, encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig, testutil.ChainID, accounts...)
	require.NoError(t, err)
	f.txs = make([][]byte, n)
	for i, name := range names {
		msg := blobfactory.NewMsgPayForFibre(t, signer.Account(name).PubKey().(*secp256k1.PubKey), testutil.ChainID)
		msg.PaymentPromise.Height = 2
		msg.PaymentPromise.CreationTimestamp = f.now
		msg.PaymentPromise.Commitment = make([]byte, 32)
		binary.BigEndian.PutUint64(msg.PaymentPromise.Commitment, uint64(i+1))
		var pp fibre.PaymentPromise
		require.NoError(t, pp.FromProto(&msg.PaymentPromise))
		signBytes, err := pp.SignBytes()
		require.NoError(t, err)
		msg.PaymentPromise.Signature, _, err = kr.Sign(name, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
		require.NoError(t, err)
		msg.ValidatorSignatures = make([][]byte, 67)
		for j := range msg.ValidatorSignatures {
			msg.ValidatorSignatures[j], err = ordered[j].Sign(signBytes)
			require.NoError(t, err)
		}
		gas := uint64(1_000_000)
		switch variant := os.Getenv("PFF_VARIANT"); variant {
		case "", "valid":
		case "lowgas":
			gas = 0
		case "invalid-first", "invalid-boundary", "invalid-quorum-end":
			index := 0
			if variant == "invalid-boundary" {
				index = 8
			}
			if variant == "invalid-quorum-end" {
				index = 66
			}
			msg.ValidatorSignatures[index], err = ordered[index].Sign([]byte("different promise"))
			require.NoError(t, err)
		default:
			t.Fatalf("unknown variant %s", variant)
		}
		f.txs[i], _, err = signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(gas), user.SetFee(4_000))
		require.NoError(t, err)
	}
	f.initial = bytes.Clone(a.LastCommitID().Hash)
	require.NoError(t, a.Close())
	if fixturePath != "" {
		bz, err := json.Marshal(struct {
			Txs     [][]byte
			Initial []byte
		}{f.txs, f.initial})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(fixturePath+".tmp", bz, 0644))
		require.NoError(t, os.Rename(fixturePath+".tmp", fixturePath))
	}
	return f
}
func (f *campaignFixture) fresh(t *testing.T, optimistic bool) (*app.App, chan campaignSpec, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(f.dir)))
	a, done := campaignApp(t, dir, optimistic)
	require.Equal(t, f.initial, a.LastCommitID().Hash)
	// A reopened BaseApp has a height-zero CheckTx context until Commit.
	// Advance every arm through the same empty block before warming or timing.
	_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: a.LastBlockHeight() + 1, Time: f.now, NextValidatorsHash: f.vals.Hash()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)

	return a, done, dir
}
func (f *campaignFixture) prepare(a *app.App) *abci.RequestPrepareProposal {
	return &abci.RequestPrepareProposal{Height: a.LastBlockHeight() + 1, Time: f.now, Txs: f.txs, MaxTxBytes: app.DefaultConsensusParams().Block.MaxBytes}
}
func (f *campaignFixture) requests(a *app.App, p *abci.ResponsePrepareProposal) (*abci.RequestProcessProposal, *abci.RequestFinalizeBlock) {
	h := core.Header{Version: cmtversion.Consensus{Block: 11, App: appconsts.Version}, ChainID: testutil.ChainID, Height: a.LastBlockHeight() + 1, Time: f.now, DataHash: p.DataRootHash, ValidatorsHash: f.vals.Hash(), NextValidatorsHash: f.vals.Hash(), AppHash: a.LastCommitID().Hash, ProposerAddress: f.vals.Validators[0].Address}
	hash := h.Hash()
	header := h.ToProto()
	return &abci.RequestProcessProposal{Hash: hash, Header: header, Height: h.Height, Time: h.Time, Txs: p.Txs, SquareSize: p.SquareSize, DataRootHash: p.DataRootHash, NextValidatorsHash: h.NextValidatorsHash, ProposerAddress: h.ProposerAddress}, &abci.RequestFinalizeBlock{Hash: hash, Header: header, Height: h.Height, Time: h.Time, Txs: p.Txs, NextValidatorsHash: h.NextValidatorsHash, ProposerAddress: h.ProposerAddress}
}
func campaignAccept(t *testing.T, a *app.App, r *abci.RequestProcessProposal) *abci.ResponseProcessProposal {
	t.Helper()
	p, e := a.ProcessProposal(r)
	require.NoError(t, e)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, p.Status)
	return p
}
func campaignFinalize(t *testing.T, a *app.App, r *abci.RequestFinalizeBlock, n int) *abci.ResponseFinalizeBlock {
	t.Helper()
	p, e := a.FinalizeBlock(r)
	require.NoError(t, e)
	require.Len(t, p.TxResults, n)
	for i, r := range p.TxResults {
		if r.Code != 0 {
			t.Fatalf("tx %d failed: %s", i, r.Log)
		}
	}
	return p
}
func campaignCheck(t *testing.T, a *app.App, txs [][]byte, kind abci.CheckTxType) []*abci.ResponseCheckTx {
	t.Helper()
	out := make([]*abci.ResponseCheckTx, len(txs))
	for i, tx := range txs {
		r, e := a.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: kind})
		require.NoError(t, e)
		if r.Code != 0 {
			t.Fatalf("CheckTx %d failed: %s", i, r.Log)
		}
		out[i] = r
	}
	return out
}
func campaignDigest(v any) string {
	bz, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:])
}

type campaignSample struct {
	Variant                                                                                string
	InputMiB, WireMiB, SquareMiB                                                           float64
	Validators, SignaturesPerPFF                                                           int
	Method, Cache, Checkpoint, Limits, CGO                                                 string
	Count, Iteration                                                                       int
	NS, Allocs, Bytes, PeakRSS, ProcessNS, FinalizeNS, CommitNS, SpeculativeNS, ResidualNS int64
	Digest, InitialHash, FinalHash                                                         string
	CacheBefore, CacheAfter                                                                map[string]int
}

// TestPFFCampaign uses one fresh disk snapshot per operation. Fixture creation,
// snapshot copy and cache warming are outside the measurement interval.
func TestPFFCampaign(t *testing.T) {
	if os.Getenv("PFF_CAMPAIGN") != "1" {
		t.Skip("explicit campaign only")
	}
	n, err := strconv.Atoi(os.Getenv("PFF_COUNT"))
	require.NoError(t, err)
	method, cache := os.Getenv("PFF_METHOD"), os.Getenv("PFF_CACHE")
	variant := os.Getenv("PFF_VARIANT")
	if variant == "" {
		variant = "valid"
	}
	expected := n
	if variant != "valid" {
		expected = 0
		require.Contains(t, []string{"CheckTxRejected", "PrepareProposalRejected"}, method)
	}
	if n > appconsts.MaxPayForFibreMessages {
		t.Fatalf("requested %d exceeds build limit %d", n, appconsts.MaxPayForFibreMessages)
	}
	f := newCampaignFixture(t, n)
	var samples []campaignSample
	iterations := 5
	if v := os.Getenv("PFF_OPERATIONS"); v != "" {
		iterations, err = strconv.Atoi(v)
		require.NoError(t, err)
	}
	for iteration := -1; iteration < iterations; iteration++ {
		oe := strings.HasPrefix(method, "OE")
		a, done, dir := f.fresh(t, oe)
		proposal, err := a.PrepareProposal(f.prepare(a))
		require.NoError(t, err)
		if len(proposal.Txs) != expected {
			t.Fatalf("proposal included %d of %d", len(proposal.Txs), n)
		}
		process, finalize := f.requests(a, proposal)
		// Preparation populated signatures; warm non-proposer calls retain them
		// while proposal artifacts are cleared independently in every such case.
		a.CampaignPurgeProposal()
		if method == "ReCheckTx" {
			campaignCheck(t, a, f.txs, abci.CheckTxType_New)
			empty := *finalize
			empty.Txs = nil
			empty.Hash = nil
			campaignFinalize(t, a, &empty, 0)
			_, err = a.Commit()
			require.NoError(t, err)
			process, finalize = f.requests(a, proposal)
		}
		if method == "Commit" {
			campaignFinalize(t, a, finalize, expected)
		}
		if cache == "cold" {
			a.CampaignPurge()
		} else if cache != "warm" {
			t.Fatal("cache must be cold or warm")
		}
		if method == "ProcessProposalProposer" {
			p, e := a.PrepareProposal(f.prepare(a))
			require.NoError(t, e)
			require.Equal(t, campaignDigest(proposal), campaignDigest(p))
		}
		var inputBytes, wireBytes int
		for _, tx := range f.txs {
			inputBytes += int(core.ComputeProtoSizeForTxs([]core.Tx{tx}))
		}
		for _, tx := range proposal.Txs {
			wireBytes += int(core.ComputeProtoSizeForTxs([]core.Tx{tx}))
		}
		sample := campaignSample{Variant: variant, InputMiB: float64(inputBytes) / (1 << 20), WireMiB: float64(wireBytes) / (1 << 20), SquareMiB: float64(proposal.SquareSize*proposal.SquareSize*512) / (1 << 20), Validators: 100, SignaturesPerPFF: 67, Method: method, Cache: cache, Count: n, Iteration: iteration, Checkpoint: os.Getenv("PFF_CHECKPOINT"), Limits: os.Getenv("PFF_LIMITS"), CGO: os.Getenv("PFF_CGO"), InitialHash: hex.EncodeToString(a.LastCommitID().Hash), CacheBefore: a.CampaignCacheStats()}
		var before, after runtime.MemStats
		profile := os.Getenv("PFF_PROFILE")
		if profile != "" && iteration == 0 {
			file, e := os.Create(profile + ".cpu.pprof")
			require.NoError(t, e)
			require.NoError(t, pprof.StartCPUProfile(file))
			defer file.Close()
		}
		runtime.ReadMemStats(&before)
		start := time.Now()
		var result any
		switch method {
		case "PrepareProposal", "PrepareProposalRejected":
			p, e := a.PrepareProposal(f.prepare(a))
			require.NoError(t, e)
			if len(p.Txs) != expected {
				t.Fatal("truncated workload")
			}
			result = p
		case "ProcessProposal", "ProcessProposalProposer":
			result = campaignAccept(t, a, process)
		case "FinalizeBlock":
			result = campaignFinalize(t, a, finalize, expected)
		case "CheckTxRejected":
			rejected := make([]*abci.ResponseCheckTx, len(f.txs))
			for i, tx := range f.txs {
				r, e := a.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
				require.NoError(t, e)
				require.NotZero(t, r.Code)
				rejected[i] = r
			}
			result = rejected
		case "CheckTx":
			result = campaignCheck(t, a, f.txs, abci.CheckTxType_New)
		case "ReCheckTx":
			result = campaignCheck(t, a, f.txs, abci.CheckTxType_Recheck)
		case "Commit":
			result, err = a.Commit()
			require.NoError(t, err)
		case "OEImmediate", "OEComplete":
			campaignAccept(t, a, process)
			sample.ProcessNS = time.Since(start).Nanoseconds()
			var spec campaignSpec
			if method == "OEComplete" {
				select {
				case spec = <-done:
				case <-time.After(2 * time.Minute):
					t.Fatal("OE did not complete")
				}
			}
			finalStart := time.Now()
			result = campaignFinalize(t, a, finalize, expected)
			sample.FinalizeNS = time.Since(finalStart).Nanoseconds()

			if method == "OEImmediate" {
				select {
				case spec = <-done:
				case <-time.After(2 * time.Minute):
					t.Fatal("OE did not complete")
				}
			}
			sample.SpeculativeNS = spec.Duration.Nanoseconds()
			sample.ResidualNS = max(int64(0), spec.Finished.Sub(finalStart).Nanoseconds())
			commitStart := time.Now()
			_, err = a.Commit()
			require.NoError(t, err)
			sample.CommitNS = time.Since(commitStart).Nanoseconds()
		default:
			t.Fatalf("unknown method %q", method)
		}
		sample.NS = time.Since(start).Nanoseconds()
		runtime.ReadMemStats(&after)
		sample.Allocs = int64(after.Mallocs - before.Mallocs)
		sample.Bytes = int64(after.TotalAlloc - before.TotalAlloc)
		var usage syscall.Rusage
		require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &usage))
		sample.PeakRSS = usage.Maxrss
		if runtime.GOOS == "linux" {
			sample.PeakRSS *= 1024
		}
		sample.Digest = campaignDigest(result)
		sample.CacheAfter = a.CampaignCacheStats()
		if !oe && method != "Commit" && method != "FinalizeBlock" {
			campaignFinalize(t, a, finalize, expected)
		}
		if !oe && method != "Commit" {
			_, err = a.Commit()
			require.NoError(t, err)
		}
		sample.FinalHash = hex.EncodeToString(a.LastCommitID().Hash)
		if profile != "" && iteration == 0 {
			pprof.StopCPUProfile()
			file, e := os.Create(profile + ".heap.pprof")
			require.NoError(t, e)
			require.NoError(t, pprof.WriteHeapProfile(file))
			require.NoError(t, file.Close())
		}
		require.NoError(t, a.Close())
		require.NoError(t, os.RemoveAll(dir))
		if iteration >= 0 {
			samples = append(samples, sample)
		}
	}
	bz, err := json.MarshalIndent(samples, "", "  ")
	require.NoError(t, err)
	path := os.Getenv("PFF_OUTPUT")
	require.NotEmpty(t, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, bz, 0644))
}
