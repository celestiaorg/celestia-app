package app_test

import (
	"bytes"
	"fmt"
	"maps"
	"math/rand"
	"os"
	"slices"
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
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
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

// The equivalence test drives a full-recheck app and a fast-recheck app through the same
// blocks and mempool traffic; after every recheck both must agree on every verdict and
// hold byte-identical checkState.

type eqPoolTx struct {
	raw     []byte
	seq     uint64
	kind    string
	promise *fibretypes.PaymentPromise
}

// eqRand is the randomness eqEnv draws from: a seeded *rand.Rand, or fuzz input.
type eqRand interface {
	Intn(n int) int
	Perm(n int) []int
	Read(p []byte) (int, error)
}

type eqEnv struct {
	t         *testing.T
	rng       eqRand
	full      *app.App
	fast      *app.App
	kr        keyring.Keyring
	enc       encoding.Config
	names     []string
	addrs     []sdk.AccAddress
	signer    *user.Signer
	blockTime time.Time
	pool      map[int][]eqPoolTx
	gasSeen   map[string]int64
	inWindow  bool

	hits, misses int
	ctxReplays   int
}

func newEqEnv(t *testing.T, seed int64, nAccounts int) *eqEnv {
	accounts := testfactory.GenerateDeterministicAccounts(nAccounts)
	cparams := app.DefaultConsensusParams()
	full, kr := testutil.SetupDeterministicTestAppWithOptions(simtestutil.AppOptionsMap{app.FlagFastRecheck: false}, cparams, accounts...)
	fast, _ := testutil.SetupDeterministicTestAppWithOptions(simtestutil.AppOptionsMap{app.FlagFastRecheck: true}, cparams, accounts...)
	require.Equal(t, full.LastCommitID().Hash, fast.LastCommitID().Hash)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	e := &eqEnv{
		t: t, rng: rand.New(rand.NewSource(seed)), full: full, fast: fast, kr: kr, enc: enc,
		names: accounts, pool: map[int][]eqPoolTx{}, gasSeen: map[string]int64{},
		blockTime: testutil.GenesisTime.Add(time.Hour),
	}
	accs := make([]*user.Account, len(accounts))
	for i, name := range accounts {
		addr := testfactory.GetAddress(kr, name)
		e.addrs = append(e.addrs, addr)
		acc := testutil.DirectQueryAccount(full, addr)
		accs[i] = user.NewAccount(name, acc.GetAccountNumber(), acc.GetSequence())
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, accs...)
	require.NoError(t, err)
	e.signer = signer

	// Short promise lifetimes so expiry and the height window trigger within the test.
	e.both(func(a *app.App, ctx sdk.Context) {
		p := a.FibreKeeper.GetParams(ctx)
		p.PaymentPromiseTimeout = 3 * time.Minute
		p.PaymentPromiseHeightWindow = 12
		a.FibreKeeper.SetParams(ctx, p)
		// A fee granter paying for senders 4 and 5 (when their txs ask), and a
		// continuous vesting account at sender 6: both must take the full ante.
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
				amt = 50_000 // drains below some promises
			}
			coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amt))
			require.NoError(t, a.BankKeeper.SendCoinsFromAccountToModule(ctx, addr, fibretypes.ModuleName, coins))
			a.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: addr.String(), Balance: coins[0], AvailableBalance: coins[0]})
		}
	})
	e.commit(nil)
	return e
}

// both applies a direct state write to both apps' committed stores, as if the
// next block made it.
func (e *eqEnv) both(fn func(a *app.App, ctx sdk.Context)) {
	for _, a := range []*app.App{e.full, e.fast} {
		ctx := a.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: a.LastBlockHeight(), Time: e.blockTime})
		fn(a, ctx)
	}
}

func committedCtx(a *app.App) sdk.Context {
	return sdk.NewContext(a.CommitMultiStore().CacheMultiStore(), cmtproto.Header{ChainID: testutil.ChainID, Height: a.LastBlockHeight()}, false, log.NewNopLogger())
}

func (e *eqEnv) committedSeq(i int) uint64 {
	acc := e.full.AccountKeeper.GetAccount(committedCtx(e.full), e.addrs[i])
	return acc.GetSequence()
}

func (e *eqEnv) checkSeq(i int) uint64 {
	ctx, ok := e.full.CheckState()
	require.True(e.t, ok)
	return e.full.AccountKeeper.GetAccount(ctx, e.addrs[i]).GetSequence()
}

func (e *eqEnv) committedBalance(i int) math.Int {
	return e.full.BankKeeper.GetBalance(committedCtx(e.full), e.addrs[i], appconsts.BondDenom).Amount
}

func (e *eqEnv) commit(txs [][]byte) {
	e.blockTime = e.blockTime.Add(time.Duration(5+e.rng.Intn(20)) * time.Second)
	if e.rng.Intn(30) == 0 {
		// A long gap: promises expire and vesting progresses between rechecks.
		e.blockTime = e.blockTime.Add(time.Duration(1+e.rng.Intn(48)) * time.Hour)
	}
	var hashes [2][]byte
	for i, a := range []*app.App{e.full, e.fast} {
		resp, err := a.FinalizeBlock(testutil.WithBlockHash(&abci.RequestFinalizeBlock{
			Time: e.blockTime, Height: a.LastBlockHeight() + 1, Txs: txs,
		}))
		require.NoError(e.t, err)
		hashes[i] = resp.AppHash
	}
	// With per-connection ABCI mutexes, CheckTx can run after FinalizeBlock
	// wrote the block into the root store but before Commit: admit txs in
	// that window too.
	if e.rng.Intn(3) == 0 && e.full.LastBlockHeight() > 2 {
		e.inWindow = true
		e.addNewTxs(1 + e.rng.Intn(10))
		e.inWindow = false
	}
	for _, a := range []*app.App{e.full, e.fast} {
		_, err := a.Commit()
		require.NoError(e.t, err)
	}
	if !bytes.Equal(hashes[0], hashes[1]) {
		ca, cb := committedCtx(e.full), committedCtx(e.fast)
		for _, name := range e.full.KVStoreKeyNames() {
			requireSameStore(e.t, -1, "committed/"+name, ca.KVStore(e.full.GetKey(name)), cb.KVStore(e.fast.GetKey(name)))
		}
		e.t.Fatalf("app hash diverged but no store differs")
	}
}

func (e *eqEnv) gas(kind string, generous uint64) uint64 {
	if seen, ok := e.gasSeen[kind]; ok && e.rng.Intn(4) == 0 {
		// Tight: around the gas a previous tx of this kind used.
		g := seen + int64(e.rng.Intn(600)) - 100
		if g > 0 {
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
		f *= 1000 // long fee amounts
	}
	return f
}

func (e *eqEnv) newPromise(i int) *fibretypes.PaymentPromise {
	pub := e.signer.Account(e.names[i]).PubKey().(*secp256k1.PubKey)
	msg := blobfactory.NewMsgPayForFibre(e.t, pub, testutil.ChainID)
	pp := &msg.PaymentPromise
	pp.Height = max(e.full.LastBlockHeight()-int64(e.rng.Intn(10)), 1)
	pp.CreationTimestamp = e.blockTime.Add(-time.Duration(e.rng.Intn(150)) * time.Second).Truncate(time.Second)
	pp.BlobSize = uint32(1 + e.rng.Intn(4_000_000))
	commitment := make([]byte, share.FibreCommitmentSize)
	_, _ = e.rng.Read(commitment)
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
	_, _ = e.rng.Read(data)
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
		var i int
		if e.rng.Intn(2) == 0 {
			i = e.rng.Intn(4) // spammers
		} else {
			i = e.rng.Intn(len(e.names))
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
			opts = append(opts, user.SetTimeoutHeight(uint64(e.full.LastBlockHeight()+int64(e.rng.Intn(4)))))
		}
		var ptx eqPoolTx
		switch r := e.rng.Intn(100); {
		case r < 45:
			gas := e.gas("pff", 1_000_000)
			pp := e.newPromise(i)
			ptx = eqPoolTx{raw: e.pffTx(i, seq, pp, append(opts, user.SetGasLimit(gas), user.SetFee(e.fee(gas)))...), kind: "pff", promise: pp}
		case r < 55:
			ptx = eqPoolTx{raw: e.pfbTx(i, seq), kind: "pfb"}
		case r < 58:
			// Drains most of the balance once executed (CheckTx does not run it),
			// often leaving less than the fee of the sender's next pooled tx.
			amt := e.committedBalance(i).MulRaw(9).QuoRaw(10)
			if e.rng.Intn(2) == 0 {
				amt = e.committedBalance(i).SubRaw(int64(e.rng.Intn(20_000)))
			}
			if !amt.IsPositive() {
				amt = math.OneInt()
			}
			gas := e.gas("send", 200_000)
			ptx = eqPoolTx{raw: e.sendTx(i, seq, e.rng.Intn(len(e.names)), amt, append(opts, user.SetGasLimit(gas), user.SetFee(e.fee(gas)))...), kind: "send"}
		default:
			gas := e.gas("send", 200_000)
			ptx = eqPoolTx{raw: e.sendTx(i, seq, e.rng.Intn(len(e.names)), math.NewInt(int64(1+e.rng.Intn(100_000))), append(opts, user.SetGasLimit(gas), user.SetFee(e.fee(gas)))...), kind: "send"}
		}
		ptx.seq = seq
		rA := e.checkTx(e.full, ptx.raw, abci.CheckTxType_New)
		rB := e.checkTx(e.fast, ptx.raw, abci.CheckTxType_New)
		if e.inWindow && (rA.Code != rB.Code || rA.GasUsed != rB.GasUsed) {
			// In this window each checkState read sees the old or the new block
			// value depending on cache history, so two stock apps differ too.
			continue
		}
		require.Equal(e.t, rA.Code, rB.Code, "new CheckTx code diverged: %s | %s", rA.Log, rB.Log)
		require.Equal(e.t, rA.GasUsed, rB.GasUsed, "new CheckTx gas diverged")
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
		k := 1 + e.rng.Intn(len(p))
		for _, ptx := range p[:k] {
			if ptx.kind == "pfb" {
				break
			}
			txs = append(txs, ptx.raw)
			included[string(ptx.raw)] = true
		}
	}
	// Txs from pool senders that are not in the pool: sequence jumps and
	// transfers into pool senders.
	for range e.rng.Intn(3) {
		i := e.rng.Intn(len(e.names))
		to := e.rng.Intn(len(e.names))
		amt := math.NewInt(int64(1 + e.rng.Intn(1_000_000)))
		if e.rng.Intn(5) == 0 {
			amt = e.committedBalance(i).QuoRaw(2)
		}
		txs = append(txs, e.sendTx(i, e.committedSeq(i), to, amt, user.SetGasLimit(200_000), user.SetFee(2_000)))
	}
	// A pool promise settled by someone else's tx.
	if e.rng.Intn(3) == 0 {
		for i := range e.names {
			for _, ptx := range e.pool[i] {
				if ptx.promise != nil && !included[string(ptx.raw)] {
					j := (i + 1 + e.rng.Intn(len(e.names)-1)) % len(e.names)
					txs = append(txs, e.pffTx(j, e.committedSeq(j), ptx.promise, user.SetGasLimit(1_000_000), user.SetFee(10_000)))
					goto settled
				}
			}
		}
	settled:
	}
	// Escrow drained out of band (withdrawals in BeginBlock).
	if e.rng.Intn(3) == 0 {
		i := e.rng.Intn(len(e.names))
		div := int64(2 + e.rng.Intn(1000))
		e.both(func(a *app.App, ctx sdk.Context) {
			esc, ok := a.FibreKeeper.GetEscrowAccount(ctx, e.addrs[i].String())
			if ok {
				esc.Balance.Amount = esc.Balance.Amount.QuoRaw(div)
				a.FibreKeeper.SetEscrowAccount(ctx, esc)
			}
		})
	}
	// Block max gas, which the ante reads from the context: lowered below the
	// PFF and PFB gas limits every 11th round, restored the round after.
	if round%11 == 10 || round%11 == 0 {
		maxGas := int64(300_000)
		if round%11 == 0 {
			maxGas = -1
		}
		e.both(func(a *app.App, ctx sdk.Context) {
			cp, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
			require.NoError(e.t, err)
			cp.Block.MaxGas = maxGas
			require.NoError(e.t, a.ConsensusKeeper.ParamsStore.Set(ctx, cp))
		})
	}
	// Param change: every 7th round.
	if round%7 == 6 {
		price := math.LegacyNewDecWithPrec(int64(1+e.rng.Intn(6)), 3)
		e.both(func(a *app.App, ctx sdk.Context) {
			a.MinFeeKeeper.SetParams(ctx, minfeetypes.Params{NetworkMinGasPrice: price})
		})
	}
	return txs
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

// recheck rechecks the pool on both apps. Before each recheck it also checks
// the record the fast app would replay against a full recheck under moved block
// times and heights.
func (e *eqEnv) recheck() {
	order := e.rng.Perm(len(e.names))
	for _, i := range order {
		var keep []eqPoolTx
		for _, ptx := range e.pool[i] {
			replayed, unsound := e.fast.FastRecheckGuardedContext(ptx.raw)
			require.Empty(e.t, unsound, "a replay of a %s tx disagrees with the full recheck", ptx.kind)
			e.ctxReplays += replayed
			hitsBefore, _ := e.fast.FastRecheckStats()
			rA := e.checkTx(e.full, ptx.raw, abci.CheckTxType_Recheck)
			rB := e.checkTx(e.fast, ptx.raw, abci.CheckTxType_Recheck)
			hitsAfter, _ := e.fast.FastRecheckStats()
			// Every response field must match except Events, Data and the
			// signer fields, which CAT ignores on recheck.
			require.Equal(e.t, len(rA.Data), len(rB.Data))
			rA.Events, rB.Events, rA.Data, rB.Data = nil, nil, nil, nil
			if hitsAfter > hitsBefore {
				rA.Address, rA.Sequence = nil, 0
			}
			require.Equal(e.t, rA, rB, "recheck response diverged (kind %s, fast hit %v)", ptx.kind, hitsAfter > hitsBefore)
			if rA.Code == abci.CodeTypeOK {
				keep = append(keep, ptx)
			}
		}
		e.pool[i] = keep
	}
	hits, misses := e.fast.FastRecheckStats()
	e.hits += int(hits)
	e.misses += int(misses)
}

// requireSameCheckState compares every KV store of both apps' checkState.
func (e *eqEnv) requireSameCheckState(round int) {
	ctxA, _ := e.full.CheckState()
	ctxB, _ := e.fast.CheckState()
	for _, name := range e.full.KVStoreKeyNames() {
		sa := ctxA.MultiStore().GetKVStore(e.full.GetKey(name))
		sb := ctxB.MultiStore().GetKVStore(e.fast.GetKey(name))
		requireSameStore(e.t, round, name, sa, sb)
	}
}

func requireSameStore(t *testing.T, round int, name string, a, b storetypes.KVStore) {
	ia, ib := a.Iterator(nil, nil), b.Iterator(nil, nil)
	defer ia.Close()
	defer ib.Close()
	for ; ia.Valid(); ia.Next() {
		require.True(t, ib.Valid(), "round %d store %s: fast missing key %x", round, name, ia.Key())
		require.Equal(t, ia.Key(), ib.Key(), "round %d store %s: key differs", round, name)
		require.Equal(t, ia.Value(), ib.Value(), "round %d store %s key %x: value differs", round, name, ia.Key())
		ib.Next()
	}
	require.False(t, ib.Valid(), "round %d store %s: fast has extra key %x", round, name, func() []byte {
		if ib.Valid() {
			return ib.Key()
		}
		return nil
	}())
}

func TestFastRecheckEquivalence(t *testing.T) {
	seeds, rounds := eqSeeds()
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			e := newEqEnv(t, seed, 16)
			defer e.full.Close()
			defer e.fast.Close()
			e.runEq(rounds)
			total := 0
			for _, p := range e.pool {
				total += len(p)
			}
			t.Logf("seed %d: fast hits=%d misses=%d pool=%d context replays=%d", seed, e.hits, e.misses, total, e.ctxReplays)
			require.Positive(t, e.hits, "the fast path never fired")
			require.Positive(t, e.ctxReplays, "no record replayed under a moved context")
		})
	}
}

// eqSeeds returns seeds 1-3 (EQ_SEEDS=n: 100..100+n-1) and the round count.
func eqSeeds() ([]int64, int) {
	seeds, rounds := []int64{1, 2, 3}, 40
	if n, err := strconv.Atoi(os.Getenv("EQ_SEEDS")); err == nil && n > 0 {
		seeds = seeds[:0]
		for i := range n {
			seeds = append(seeds, int64(100+i))
		}
	}
	if testing.Short() {
		seeds, rounds = seeds[:1], 15
	}
	return seeds, rounds
}

// runEq drives e for rounds rounds.
func (e *eqEnv) runEq(rounds int) {
	for round := range rounds {
		e.round(round)
	}
}

// round admits txs, commits a block, rechecks the pool, and checks checkState
// after each step.
func (e *eqEnv) round(round int) {
	e.addNewTxs(10 + e.rng.Intn(40))
	e.requireSameCheckState(round)
	txs := e.buildBlock(round)
	e.commit(txs)
	e.removeIncluded(txs)
	e.recheck()
	e.requireSameCheckState(round)
}

// TestFastRecheckAllMiss checks that dropped records send every recheck down
// the full path once, with the same verdicts and checkState, then hit again.
func TestFastRecheckAllMiss(t *testing.T) {
	e := newEqEnv(t, 11, 8)
	defer e.full.Close()
	defer e.fast.Close()
	for round := range 12 {
		e.addNewTxs(20)
		txs := e.buildBlock(round)
		e.commit(txs)
		e.removeIncluded(txs)
		reset := round%3 == 0
		if reset {
			e.fast.ResetFastRecheck()
		}
		hits, misses := e.hits, e.misses
		e.recheck()
		e.requireSameCheckState(round)
		if reset {
			require.Equal(t, hits, e.hits, "round %d: a recheck hit after the records were dropped", round)
			require.Greater(t, e.misses, misses, "round %d: nothing was rechecked", round)
		}
	}
	require.Positive(t, e.hits)
}

// TestFastRecheckGuardedReads checks that a change to any value the recorded ante
// read, in any store, stops the replay: no store or key needs classifying.
func TestFastRecheckGuardedReads(t *testing.T) {
	e := newEqEnv(t, 21, 8)
	defer e.full.Close()
	defer e.fast.Close()
	e.runEq(3)
	e.addNewTxs(30)
	// A block with one outside tx, so the fee collector holds fees; check state is
	// then back at committed state and each sender's next tx replays.
	last := len(e.names) - 1
	e.commit([][]byte{e.sendTx(last, e.committedSeq(last), 0, math.OneInt(), user.SetGasLimit(200_000), user.SetFee(2_000))})
	reads, unguarded, replayable, stores := e.fast.FastRecheckGuardedReads()
	t.Logf("reads=%d replayable=%d stores=%v", reads, replayable, slices.Sorted(maps.Keys(stores)))
	require.Positive(t, replayable)
	require.Positive(t, reads)
	require.Zero(t, unguarded)
}

// TestFastRecheckUnrecordedState checks that the full recheck ante depends on
// no stored value the record did not read: for a replayable record of each tx
// kind, every other key changed in turn either stops the replay or leaves it
// agreeing with the full recheck. Each record costs about two full antes per
// key, so this runs on a few records only.
func TestFastRecheckUnrecordedState(t *testing.T) {
	e := newEqEnv(t, 51, 8)
	defer e.full.Close()
	defer e.fast.Close()
	// Senders with a plain account, a funded escrow and no fee grant.
	txs := map[string][]byte{
		"send": e.sendTx(1, e.checkSeq(1), 2, math.OneInt(), user.SetGasLimit(200_000), user.SetFee(2_000)),
		"pfb":  e.pfbTx(2, e.checkSeq(2)),
		"pff":  e.pffTx(4, e.checkSeq(4), e.newPromise(4), user.SetGasLimit(1_000_000), user.SetFee(10_000)),
	}
	for kind, raw := range txs {
		require.Equal(t, abci.CodeTypeOK, e.checkTx(e.fast, raw, abci.CheckTxType_New).Code, kind)
	}
	// A block without them but with a fee, so the fee collector holds fees and
	// check state is back at committed state: each record replays.
	last := len(e.names) - 1
	e.commit([][]byte{e.sendTx(last, e.committedSeq(last), 0, math.OneInt(), user.SetGasLimit(200_000), user.SetFee(2_000))})
	for _, kind := range slices.Sorted(maps.Keys(txs)) {
		changes, replayed, unsound := e.fast.FastRecheckUnrecordedState(txs[kind], 1)
		t.Logf("%s: changes=%d replayed=%d", kind, changes, replayed)
		require.Positive(t, changes, "%s: the record does not replay", kind)
		require.Positive(t, replayed, "%s: no change left the record replayable", kind)
		require.Empty(t, unsound, "%s: a replay disagrees with the full recheck after a change to a key the record did not read", kind)
	}
}

// TestFastRecheckPayerDrained checks the two balances the replay must not
// reproduce: a payer left with less than the fee (rejected) or with exactly the
// fee (its balance is removed, not written).
func TestFastRecheckPayerDrained(t *testing.T) {
	e := newEqEnv(t, 31, 8)
	defer e.full.Close()
	defer e.fast.Close()
	const fee = 2_000
	block := make([][]byte, 0, 2)
	for i, leftover := range map[int]int64{1: fee - 1, 2: fee} {
		seq := e.checkSeq(i)
		amt := e.committedBalance(i).SubRaw(fee + leftover)
		first := e.sendTx(i, seq, 0, amt, user.SetGasLimit(200_000), user.SetFee(fee))
		second := e.sendTx(i, seq+1, 0, math.OneInt(), user.SetGasLimit(200_000), user.SetFee(fee))
		for _, raw := range [][]byte{first, second} {
			for _, a := range []*app.App{e.full, e.fast} {
				require.Equal(t, abci.CodeTypeOK, e.checkTx(a, raw, abci.CheckTxType_New).Code)
			}
		}
		e.pool[i] = []eqPoolTx{{raw: second, seq: seq + 1, kind: "send"}}
		block = append(block, first)
	}
	e.commit(block)
	e.recheck()
	e.requireSameCheckState(0)
	require.Empty(t, e.pool[1], "a payer left below the fee must be rejected")
	require.Len(t, e.pool[2], 1)
}

// TestFastRecheckConcurrentSimulate runs Simulate, whose ante shares the
// wrapper, alongside recording and replaying rechecks (run with -race).
func TestFastRecheckConcurrentSimulate(t *testing.T) {
	e := newEqEnv(t, 41, 8)
	defer e.full.Close()
	defer e.fast.Close()
	tx := e.sendTx(0, e.checkSeq(0), 1, math.OneInt(), user.SetGasLimit(200_000), user.SetFee(2_000))
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_, _, _ = e.fast.Simulate(tx)
			}
		}
	}()
	// Stop the goroutine before the deferred Close calls, also when a check fails.
	defer func() {
		close(stop)
		<-done
	}()
	e.runEq(5)
	require.Positive(t, e.hits)
}
