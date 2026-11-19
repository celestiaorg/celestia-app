package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestRecheckTxsMatchesSequential drives two identical apps through the same random blocks and
// mempool traffic. One rechecks with CheckTx(Type=Recheck) per tx, the other with a parallel
// RecheckTxs; every verdict and the whole check state must match after each recheck.
func TestRecheckTxsMatchesSequential(t *testing.T) {
	seeds, rounds := 6, 25
	if n, err := strconv.Atoi(os.Getenv("RECHECK_EQ_SEEDS")); err == nil && n > 0 {
		seeds = n
	}
	if testing.Short() {
		seeds, rounds = 2, 10
	}
	for seed := range seeds {
		accounts := 16 + rand.New(rand.NewSource(int64(seed))).Intn(33)
		t.Run(fmt.Sprintf("seed=%d/accounts=%d", seed, accounts), func(t *testing.T) {
			e := newEqEnv(t, int64(200+seed), accounts)
			merged := 0
			e.batch.SetBeforeRecheckMerge(func() error { merged++; return nil })
			for round := range rounds {
				e.round(round)
			}
			t.Logf("parallel merges %d/%d (gas bound > 0 in %d rounds)", merged, rounds, e.boundRounds)
			require.Positive(t, merged, "no recheck ran in parallel")
		})
	}
}

// TestRecheckTxsFallback fails every parallel pass just before its merge: the sequential
// rerun must match CheckTx(Type=Recheck) exactly, so nothing from the branches may leak.
func TestRecheckTxsFallback(t *testing.T) {
	e := newEqEnv(t, 300, 24)
	failed := 0
	e.batch.SetBeforeRecheckMerge(func() error { failed++; return errors.New("forced") })
	for round := range 10 {
		e.round(round)
	}
	require.Positive(t, failed, "no parallel pass reached its merge")
}

// TestRecheckGroupsConflict splits one sender's consecutive txs across two groups: the second
// reads the account the first writes, so the pass must fail and leave check state untouched.
func TestRecheckGroupsConflict(t *testing.T) {
	e := newEqEnv(t, 400, 16)
	seq := e.checkSeq(1)
	txs := [][]byte{
		e.sendTx(1, seq, 2, math.NewInt(10), user.SetGasLimit(200_000), user.SetFee(2_000)),
		e.sendTx(1, seq+1, 2, math.NewInt(10), user.SetGasLimit(200_000), user.SetFee(2_000)),
		e.sendTx(3, e.checkSeq(3), 2, math.NewInt(10), user.SetGasLimit(200_000), user.SetFee(2_000)),
	}
	for _, a := range []*app.App{e.stock, e.batch} {
		for _, tx := range txs {
			require.Equal(t, abci.CodeTypeOK, e.checkTx(a, tx, abci.CheckTxType_New).Code)
		}
	}
	e.commit(nil)
	err := e.batch.RecheckGroups(txs, [][]int{{0}, {1}, {2}}, 2)
	require.ErrorContains(t, err, "written by group")
	e.requireSameCheckState(-1)
	require.NoError(t, e.batch.RecheckGroups(txs, [][]int{{0, 1}, {2}}, 2))
	for _, tx := range txs {
		e.checkTx(e.stock, tx, abci.CheckTxType_Recheck)
	}
	e.requireSameCheckState(-1)
}

// TestRecheckTxsNilOnCheckTxError checks that a tx whose CheckTx errors gets a nil response,
// which core treats as it does a CheckTx error: the tx stays in the pool.
func TestRecheckTxsNilOnCheckTxError(t *testing.T) {
	a, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	bad := []byte{0xff, 0x01}
	_, err := a.CheckTx(&abci.RequestCheckTx{Tx: bad, Type: abci.CheckTxType_Recheck})
	require.Error(t, err)
	rsp, err := a.RecheckTxs(t.Context(), &abci.RequestRecheckTxs{Txs: [][]byte{bad, bad}})
	require.NoError(t, err)
	require.Equal(t, []*abci.ResponseCheckTx{nil, nil}, rsp.Responses)
}

type eqPoolTx struct {
	raw     []byte
	seq     uint64
	kind    string
	promise *fibretypes.PaymentPromise
}

// eqEnv runs a stock app and a batch app (rechecked with RecheckTxs) side by side.
type eqEnv struct {
	t           *testing.T
	rng         *rand.Rand
	stock       *app.App
	batch       *app.App
	kr          keyring.Keyring
	enc         encoding.Config
	names       []string
	addrs       []sdk.AccAddress
	signer      *user.Signer
	blockTime   time.Time
	pool        map[int][]eqPoolTx
	gasSeen     map[string]int64
	inWindow    bool
	boundRounds int
}

func newEqApp(cparams *cmtproto.ConsensusParams, genesis app.GenesisState) *app.App {
	a := app.New(testutil.TestAppLogger, dbm.NewMemDB(), nil, 0, 0, simtestutil.AppOptionsMap{}, baseapp.SetChainID(testutil.ChainID))
	return testutil.InitialiseTestAppWithGenesis(a, cparams, genesis)
}

func newEqEnv(t *testing.T, seed int64, nAccounts int) *eqEnv {
	accounts := testfactory.GenerateAccounts(nAccounts)
	cparams := app.DefaultConsensusParams()
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(testutil.NewTestApp(), accounts...)
	stock, batch := newEqApp(cparams, genesis), newEqApp(cparams, genesis)
	t.Cleanup(func() { stock.Close(); batch.Close() })
	for _, a := range []*app.App{stock, batch} {
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time: testutil.GenesisTime, Height: 1, Hash: a.LastCommitID().Hash, NextValidatorsHash: valSet.Hash(),
		})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
	}
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	e := &eqEnv{
		t: t, rng: rand.New(rand.NewSource(seed)), stock: stock, batch: batch, kr: kr, enc: enc,
		names: accounts, pool: map[int][]eqPoolTx{}, gasSeen: map[string]int64{},
		blockTime: testutil.GenesisTime.Add(time.Hour),
	}
	accs := make([]*user.Account, len(accounts))
	for i, name := range accounts {
		addr := testfactory.GetAddress(kr, name)
		e.addrs = append(e.addrs, addr)
		acc := testutil.DirectQueryAccount(stock, addr)
		accs[i] = user.NewAccount(name, acc.GetAccountNumber(), acc.GetSequence())
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, accs...)
	require.NoError(t, err)
	e.signer = signer

	// Short promise lifetimes, a fee granter paying for senders 4 and 5 (when their txs ask),
	// a continuous vesting account at sender 6, and escrows that some promises overdraw.
	e.both(func(a *app.App, ctx sdk.Context) {
		p := a.FibreKeeper.GetParams(ctx)
		p.PaymentPromiseTimeout = 3 * time.Minute
		p.PaymentPromiseHeightWindow = 12
		a.FibreKeeper.SetParams(ctx, p)
		for _, g := range []int{4, 5} {
			require.NoError(t, a.FeeGrantKeeper.GrantAllowance(ctx, e.addrs[len(e.addrs)-1], e.addrs[g], &feegrant.BasicAllowance{}))
		}
		base := a.AccountKeeper.GetAccount(ctx, e.addrs[6]).(*authtypes.BaseAccount)
		bal := a.BankKeeper.GetBalance(ctx, e.addrs[6], appconsts.BondDenom).Amount
		va, err := vestingtypes.NewContinuousVestingAccount(base, sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, bal.QuoRaw(2))),
			e.blockTime.Unix(), e.blockTime.Add(time.Hour).Unix())
		require.NoError(t, err)
		a.AccountKeeper.SetAccount(ctx, va)
		for i, addr := range e.addrs {
			amt := int64(2_000_000_000)
			if i%3 == 0 {
				amt = 50_000
			}
			coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amt))
			require.NoError(t, a.BankKeeper.SendCoinsFromAccountToModule(ctx, addr, fibretypes.ModuleName, coins))
			a.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: addr.String(), Balance: coins[0], AvailableBalance: coins[0]})
		}
	})
	e.commit(nil)
	return e
}

// round admits new txs, commits a block of some of them, and rechecks the rest.
func (e *eqEnv) round(round int) {
	e.addNewTxs(20 + e.rng.Intn(60))
	txs := e.buildBlock(round)
	e.commit(txs)
	e.removeIncluded(txs)
	e.moveFeeCollector()
	e.recheck()
	e.requireSameCheckState(round)
}

// both applies a direct state write to both apps' committed stores, as if the next block made it.
func (e *eqEnv) both(fn func(a *app.App, ctx sdk.Context)) {
	for _, a := range []*app.App{e.stock, e.batch} {
		fn(a, a.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: a.LastBlockHeight(), Time: e.blockTime}))
	}
}

func committedCtx(a *app.App) sdk.Context {
	return sdk.NewContext(a.CommitMultiStore().CacheMultiStore(), cmtproto.Header{ChainID: testutil.ChainID, Height: a.LastBlockHeight()}, false, log.NewNopLogger())
}

func (e *eqEnv) committedSeq(i int) uint64 {
	return e.stock.AccountKeeper.GetAccount(committedCtx(e.stock), e.addrs[i]).GetSequence()
}

func (e *eqEnv) checkSeq(i int) uint64 {
	ctx, ok := e.stock.CheckState()
	require.True(e.t, ok)
	return e.stock.AccountKeeper.GetAccount(ctx, e.addrs[i]).GetSequence()
}

func (e *eqEnv) committedBalance(i int) math.Int {
	return e.stock.BankKeeper.GetBalance(committedCtx(e.stock), e.addrs[i], appconsts.BondDenom).Amount
}

func (e *eqEnv) commit(txs [][]byte) {
	e.blockTime = e.blockTime.Add(time.Duration(5+e.rng.Intn(20)) * time.Second)
	var hashes [2][]byte
	for i, a := range []*app.App{e.stock, e.batch} {
		resp, err := a.FinalizeBlock(testutil.WithBlockHash(&abci.RequestFinalizeBlock{
			Time: e.blockTime, Height: a.LastBlockHeight() + 1, Txs: txs,
		}))
		require.NoError(e.t, err)
		hashes[i] = resp.AppHash
	}
	// With per-connection ABCI mutexes, CheckTx can run between FinalizeBlock and Commit.
	if e.rng.Intn(3) == 0 && e.stock.LastBlockHeight() > 2 {
		e.inWindow = true
		e.addNewTxs(1 + e.rng.Intn(10))
		e.inWindow = false
	}
	for _, a := range []*app.App{e.stock, e.batch} {
		_, err := a.Commit()
		require.NoError(e.t, err)
	}
	if !bytes.Equal(hashes[0], hashes[1]) {
		ka, kb := storeKeys(e.stock), storeKeys(e.batch)
		for name, k := range ka {
			if _, ok := k.(*storetypes.KVStoreKey); ok {
				requireSameStore(e.t, -2, "committed/"+name, committedCtx(e.stock).MultiStore().GetKVStore(k), committedCtx(e.batch).MultiStore().GetKVStore(kb[name]))
			}
		}
		e.t.Fatal("app hash diverged but no store differs")
	}
}

// gas is generous, or in a quarter of the cases tight: around the gas a previous tx of kind used.
func (e *eqEnv) gas(kind string, generous uint64) uint64 {
	if seen, ok := e.gasSeen[kind]; ok && e.rng.Intn(4) == 0 {
		if g := seen + int64(e.rng.Intn(600)) - 100; g > 0 {
			return uint64(g)
		}
	}
	return generous
}

func (e *eqEnv) fee(gas uint64) uint64 {
	f := uint64(float64(gas)*0.004) + 1 + uint64(e.rng.Intn(5000))
	if e.rng.Intn(40) == 0 {
		f = uint64(float64(gas) * 0.001) // below the minimum
	}
	if e.rng.Intn(10) == 0 {
		f *= 1000 // long fee collector balances
	}
	return f
}

func (e *eqEnv) newPromise(i int) *fibretypes.PaymentPromise {
	pub := e.signer.Account(e.names[i]).PubKey().(*secp256k1.PubKey)
	msg := blobfactory.NewMsgPayForFibre(e.t, pub, testutil.ChainID)
	pp := &msg.PaymentPromise
	pp.Height = max(e.stock.LastBlockHeight()-int64(e.rng.Intn(10)), 1)
	pp.CreationTimestamp = e.blockTime.Add(-time.Duration(e.rng.Intn(150)) * time.Second).Truncate(time.Second)
	pp.BlobSize = uint32(1 + e.rng.Intn(4_000_000))
	commitment := make([]byte, share.FibreCommitmentSize)
	e.rng.Read(commitment)
	pp.Commitment = commitment
	fp := fibre.PaymentPromise{}
	require.NoError(e.t, fp.FromProto(pp))
	signBytes, err := fp.SignBytes()
	require.NoError(e.t, err)
	pp.Signature, _, err = e.signer.Keyring().Sign(e.names[i], signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(e.t, err)
	return pp
}

// pffTx builds a PayForFibre submitted by sender i carrying promise pp.
func (e *eqEnv) pffTx(i int, seq uint64, pp *fibretypes.PaymentPromise, opts ...user.TxOption) []byte {
	fp := fibre.PaymentPromise{}
	require.NoError(e.t, fp.FromProto(pp))
	signBytes, err := fp.SignBytes()
	require.NoError(e.t, err)
	vs, err := testutil.GenesisValidatorPrivateKey().Sign(signBytes)
	require.NoError(e.t, err)
	msg := &fibretypes.MsgPayForFibre{Signer: e.addrs[i].String(), PaymentPromise: *pp, ValidatorSignatures: [][]byte{vs}}
	require.NoError(e.t, e.signer.SetSequence(e.names[i], seq))
	raw, _, err := e.signer.CreateTx([]sdk.Msg{msg}, opts...)
	require.NoError(e.t, err)
	return raw
}

func (e *eqEnv) sendTx(i int, seq uint64, to int, amount math.Int, opts ...user.TxOption) []byte {
	msg := banktypes.NewMsgSend(e.addrs[i], e.addrs[to], sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amount)))
	require.NoError(e.t, e.signer.SetSequence(e.names[i], seq))
	raw, _, err := e.signer.CreateTx([]sdk.Msg{msg}, opts...)
	require.NoError(e.t, err)
	return raw
}

func (e *eqEnv) pfbTx(i int, seq uint64) []byte {
	require.NoError(e.t, e.signer.SetSequence(e.names[i], seq))
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{byte(1 + e.rng.Intn(200))}, share.NamespaceVersionZeroIDSize))
	data := make([]byte, 1+e.rng.Intn(4000))
	e.rng.Read(data)
	b, err := share.NewBlob(ns, data, share.ShareVersionZero, nil)
	require.NoError(e.t, err)
	gas := e.gas("pfb", 400_000)
	raw, _, err := e.signer.CreatePayForBlobs(e.names[i], []*share.Blob{b}, user.SetGasLimit(gas), user.SetFee(e.fee(gas)))
	require.NoError(e.t, err)
	return raw
}

// addNewTxs admits new txs as the mempool would, on both apps.
func (e *eqEnv) addNewTxs(n int) {
	for range n {
		i := e.rng.Intn(len(e.names))
		if e.rng.Intn(2) == 0 {
			i = e.rng.Intn(4) // spammers
		}
		seq := e.checkSeq(i)
		if p := e.pool[i]; len(p) > 0 {
			seq = p[len(p)-1].seq + 1
		}
		if e.rng.Intn(30) == 0 {
			seq += uint64(1 + e.rng.Intn(3)) // gap: rejected
		}
		var opts []user.TxOption
		if (i == 4 || i == 5) && e.rng.Intn(3) == 0 {
			opts = append(opts, user.SetFeeGranter(e.addrs[len(e.addrs)-1]))
		}
		if e.rng.Intn(10) == 0 {
			opts = append(opts, user.SetTimeoutHeight(uint64(e.stock.LastBlockHeight()+int64(e.rng.Intn(4)))))
		}
		var ptx eqPoolTx
		switch r := e.rng.Intn(100); {
		case r < 45:
			gas := e.gas("pff", 1_000_000)
			pp := e.newPromise(i)
			ptx = eqPoolTx{raw: e.pffTx(i, seq, pp, append(opts, user.SetGasLimit(gas), user.SetFee(e.fee(gas)))...), kind: "pff", promise: pp}
		case r < 55:
			ptx = eqPoolTx{raw: e.pfbTx(i, seq), kind: "pfb"}
		default:
			amt := math.NewInt(int64(1 + e.rng.Intn(100_000)))
			if r < 58 { // drains most of the balance once executed
				amt = math.MaxInt(e.committedBalance(i).MulRaw(9).QuoRaw(10), math.OneInt())
			}
			gas := e.gas("send", 200_000)
			ptx = eqPoolTx{raw: e.sendTx(i, seq, e.rng.Intn(len(e.names)), amt, append(opts, user.SetGasLimit(gas), user.SetFee(e.fee(gas)))...), kind: "send"}
		}
		ptx.seq = seq
		rA := e.checkTx(e.stock, ptx.raw, abci.CheckTxType_New)
		rB := e.checkTx(e.batch, ptx.raw, abci.CheckTxType_New)
		if e.inWindow && (rA.Code != rB.Code || rA.GasUsed != rB.GasUsed) {
			// In this window a checkState read sees the old or the new block depending on cache history.
			continue
		}
		require.Equal(e.t, rA.Code, rB.Code, "new CheckTx code diverged: %s | %s", rA.Log, rB.Log)
		if rA.Code != abci.CodeTypeOK {
			continue
		}
		e.gasSeen[ptx.kind] = rA.GasUsed
		if e.rng.Intn(30) == 0 {
			continue // passed CheckTx but the pool was full
		}
		e.pool[i] = append(e.pool[i], ptx)
	}
}

func (e *eqEnv) checkTx(a *app.App, raw []byte, typ abci.CheckTxType) *abci.ResponseCheckTx {
	resp, err := a.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: typ})
	if err != nil {
		require.NotNil(e.t, resp)
		require.NotEqual(e.t, abci.CodeTypeOK, resp.Code)
	}
	return resp
}

// buildBlock picks block txs and applies out-of-band block writes.
func (e *eqEnv) buildBlock(round int) [][]byte {
	txs := make([][]byte, 0, len(e.names))
	included := map[string]bool{}
	for i := range e.names {
		p := e.pool[i]
		if len(p) == 0 || e.rng.Intn(3) != 0 {
			continue
		}
		for _, ptx := range p[:1+e.rng.Intn(len(p))] {
			if ptx.kind == "pfb" {
				break
			}
			txs = append(txs, ptx.raw)
			included[string(ptx.raw)] = true
		}
	}
	// Txs from pool senders that are not in the pool: sequence jumps and transfers into pool senders.
	for range e.rng.Intn(3) {
		i, to := e.rng.Intn(len(e.names)), e.rng.Intn(len(e.names))
		amt := math.NewInt(int64(1 + e.rng.Intn(1_000_000)))
		if e.rng.Intn(5) == 0 {
			amt = e.committedBalance(i).QuoRaw(2)
		}
		txs = append(txs, e.sendTx(i, e.committedSeq(i), to, amt, user.SetGasLimit(200_000), user.SetFee(2_000)))
	}
	// A pool promise settled by someone else's tx.
	if e.rng.Intn(3) == 0 {
	settle:
		for i := range e.names {
			for _, ptx := range e.pool[i] {
				if ptx.promise != nil && !included[string(ptx.raw)] {
					j := (i + 1 + e.rng.Intn(len(e.names)-1)) % len(e.names)
					txs = append(txs, e.pffTx(j, e.committedSeq(j), ptx.promise, user.SetGasLimit(1_000_000), user.SetFee(10_000)))
					break settle
				}
			}
		}
	}
	// Escrow drained out of band (withdrawals in BeginBlock).
	if e.rng.Intn(3) == 0 {
		i := e.rng.Intn(len(e.names))
		div := int64(2 + e.rng.Intn(1000))
		e.both(func(a *app.App, ctx sdk.Context) {
			if esc, ok := a.FibreKeeper.GetEscrowAccount(ctx, e.addrs[i].String()); ok {
				esc.Balance.Amount = esc.Balance.Amount.QuoRaw(div)
				a.FibreKeeper.SetEscrowAccount(ctx, esc)
			}
		})
	}
	if round%7 == 6 {
		price := math.LegacyNewDecWithPrec(int64(1+e.rng.Intn(6)), 3)
		e.both(func(a *app.App, ctx sdk.Context) {
			a.MinFeeKeeper.SetParams(ctx, minfeetypes.Params{NetworkMinGasPrice: price})
		})
	}
	return txs
}

// moveFeeCollector sets the committed fee collector balance just below a power of ten, or to
// zero, in some rounds, so that the pool's credits lengthen it during the recheck.
func (e *eqEnv) moveFeeCollector() {
	below := int64(1 + e.rng.Intn(500_000))
	switch e.rng.Intn(6) {
	case 0, 1:
		e.both(func(a *app.App, ctx sdk.Context) {
			cur := feeCollectorBalance(a, ctx)
			target := math.NewIntWithDecimal(1, len(cur.String())).SubRaw(below)
			if target.GT(cur) {
				coins := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, target.Sub(cur)))
				require.NoError(e.t, a.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
				require.NoError(e.t, a.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, coins))
			}
		})
	case 2:
		e.both(func(a *app.App, ctx sdk.Context) {
			coins := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, feeCollectorBalance(a, ctx)))
			require.NoError(e.t, a.BankKeeper.SendCoinsFromModuleToModule(ctx, authtypes.FeeCollectorName, minttypes.ModuleName, coins))
		})
	}
}

func (e *eqEnv) removeIncluded(txs [][]byte) {
	inBlock := map[string]bool{}
	for _, tx := range txs {
		inBlock[string(tx)] = true
	}
	for i, p := range e.pool {
		var keep []eqPoolTx
		for _, ptx := range p {
			if !inBlock[string(ptx.raw)] {
				keep = append(keep, ptx)
			}
		}
		e.pool[i] = keep
	}
	// TTL purge of a middle tx and eviction of a tail, outside any block.
	if e.rng.Intn(3) == 0 {
		i := e.rng.Intn(len(e.names))
		if p := e.pool[i]; len(p) > 2 {
			m := 1 + e.rng.Intn(len(p)-1)
			e.pool[i] = append(p[:m:m], p[m+1:]...)
		}
	}
	if e.rng.Intn(3) == 0 {
		i := e.rng.Intn(len(e.names))
		if p := e.pool[i]; len(p) > 1 {
			e.pool[i] = p[:e.rng.Intn(len(p))]
		}
	}
}

// recheck rechecks the pool per tx on the stock app and with RecheckTxs (sometimes split in two
// calls) on the batch app, then compares every response.
func (e *eqEnv) recheck() {
	var txs [][]byte
	var ptxs []eqPoolTx
	var owner []int
	for _, i := range e.rng.Perm(len(e.names)) {
		for _, ptx := range e.pool[i] {
			txs, ptxs, owner = append(txs, ptx.raw), append(ptxs, ptx), append(owner, i)
		}
	}
	committedFees := feeCollectorBalance(e.stock, committedCtx(e.stock))
	got := make([]*abci.ResponseCheckTx, 0, len(txs))
	split := 0
	if e.rng.Intn(4) == 0 {
		split = len(txs) / 2
	}
	for _, part := range [][][]byte{txs[:split], txs[split:]} {
		if len(part) == 0 {
			continue // core sends one call per height; an empty one would leave check state dirty
		}
		rsp, err := e.batch.RecheckTxs(context.Background(), &abci.RequestRecheckTxs{Txs: part})
		require.NoError(e.t, err)
		require.Len(e.t, rsp.Responses, len(part))
		got = append(got, rsp.Responses...)
	}
	resps := make([]*abci.ResponseCheckTx, len(txs))
	for k, raw := range txs {
		resps[k] = e.checkTx(e.stock, raw, abci.CheckTxType_Recheck)
	}
	// A branch sees a fee collector balance between the committed and the final one.
	ctx, _ := e.stock.CheckState()
	growth := int64(len(feeCollectorBalance(e.stock, ctx).String()) - len(committedFees.String()))
	if committedFees.IsZero() {
		growth++
	}
	cfg := storetypes.KVGasConfig()
	maxExtra := growth * int64(cfg.ReadCostPerByte+cfg.WriteCostPerByte)
	if maxExtra > 0 {
		e.boundRounds++
	}
	keep := map[int][]eqPoolTx{}
	for k, rA := range resps {
		rB := got[k]
		require.NotNil(e.t, rB, "tx %d: no batch response", k)
		require.Equal(e.t, rA.Code, rB.Code, "tx %d recheck code diverged: stock=%q batch=%q", k, rA.Log, rB.Log)
		require.Equal(e.t, rA.Codespace, rB.Codespace)
		require.Equal(e.t, rA.GasWanted, rB.GasWanted)
		require.Equal(e.t, rA.Priority, rB.Priority)
		require.Equal(e.t, rA.Data, rB.Data)
		require.Equal(e.t, rA.Info, rB.Info)
		require.Equal(e.t, rA.Address, rB.Address)
		require.Equal(e.t, rA.Sequence, rB.Sequence)
		require.Equal(e.t, rA.Events, rB.Events)
		// GasUsed may count the fee collector at its shorter branch value.
		require.LessOrEqual(e.t, rB.GasUsed, rA.GasUsed, "tx %d", k)
		extra := maxExtra
		if rA.Code != abci.CodeTypeOK { // its own discarded credit may add a byte
			extra += int64(cfg.WriteCostPerByte)
		}
		require.LessOrEqual(e.t, rA.GasUsed-rB.GasUsed, extra, "tx %d", k)
		require.Equal(e.t, rA.Log, rB.Log)
		if rA.Code == abci.CodeTypeOK {
			keep[owner[k]] = append(keep[owner[k]], ptxs[k])
		}
	}
	e.pool = keep
}

func feeCollectorBalance(a *app.App, ctx sdk.Context) math.Int {
	return a.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(authtypes.FeeCollectorName), appconsts.BondDenom).Amount
}

// requireSameCheckState compares every KV store of both apps' checkState.
func (e *eqEnv) requireSameCheckState(round int) {
	ctxA, _ := e.stock.CheckState()
	ctxB, _ := e.batch.CheckState()
	keysA, keysB := storeKeys(e.stock), storeKeys(e.batch)
	for name, k := range keysA {
		if _, ok := k.(*storetypes.MemoryStoreKey); ok {
			continue // capability keys hold per-process pointers
		}
		requireSameStore(e.t, round, name, ctxA.MultiStore().GetKVStore(k), ctxB.MultiStore().GetKVStore(keysB[name]))
	}
}

func storeKeys(a *app.App) map[string]storetypes.StoreKey {
	return a.CommitMultiStore().(interface {
		StoreKeysByName() map[string]storetypes.StoreKey
	}).StoreKeysByName()
}

func requireSameStore(t *testing.T, round int, name string, a, b storetypes.KVStore) {
	ia, ib := a.Iterator(nil, nil), b.Iterator(nil, nil)
	defer ia.Close()
	defer ib.Close()
	for ; ia.Valid(); ia.Next() {
		require.True(t, ib.Valid(), "round %d store %s: batch missing key %x", round, name, ia.Key())
		require.Equal(t, ia.Key(), ib.Key(), "round %d store %s: key differs", round, name)
		require.Equal(t, ia.Value(), ib.Value(), "round %d store %s key %x: value differs", round, name, ia.Key())
		ib.Next()
	}
	require.False(t, ib.Valid(), "round %d store %s: batch has an extra key", round, name)
}
