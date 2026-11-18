package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/parallel"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

var _ abci.RecheckTxsApplication = (*App)(nil)

// RecheckTxs rechecks the mempool in one call, with the verdicts and check state of
// CheckTx(Type=Recheck) on each tx in order. A nil response keeps the tx, as a CheckTx error does.
func (app *App) RecheckTxs(_ context.Context, req *abci.RequestRecheckTxs) (*abci.ResponseRecheckTxs, error) {
	app.checkStateMu.Lock()
	defer app.checkStateMu.Unlock()
	resps := app.recheckParallel(req.Txs)
	if resps == nil {
		resps = make([]*abci.ResponseCheckTx, len(req.Txs))
		for i, tx := range req.Txs {
			if res, err := app.checkTxLocked(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_Recheck}, nil); err == nil {
				resps[i] = res
			}
		}
	}
	return &abci.ResponseRecheckTxs{Responses: resps}, nil
}

// parallelTx is one tx of a parallel recheck, run on worker w's branch of check state. Its
// cache and fast recheck record updates wait here until the branches are merged.
type parallelTx struct {
	raw []byte
	w   *baseapp.RecheckWorker
	// cached is raw's admission decode; dec decodes decRaw (raw or a blob tx's inner tx).
	cached  sdk.Tx
	decRaw  []byte
	dec     sdk.Tx
	decErr  error
	fastRec *fastRecord
	hit     bool // fastRec is predicted to hit
	// keys tie the tx to txs that must run in its group; fee is what it credits the fee collector.
	keys []string
	fee  sdk.Coins

	res *abci.ResponseCheckTx
	err error

	drop     bool
	refill   sdk.Tx
	pre      *fastPre
	recKey   []byte
	rec      *fastRecord
	fastHit  bool
	fastMiss string
}

// decodeTx decodes raw, reusing p's decode of the same slice.
func (app *App) decodeTx(raw []byte, p *parallelTx) (sdk.Tx, error) {
	if p != nil && len(raw) > 0 && len(raw) == len(p.decRaw) && &raw[0] == &p.decRaw[0] {
		return p.dec, p.decErr
	}
	return app.checkTxDecoder()(raw)
}

// recheckOnBranch is checkDecoded on p's worker branch, keeping a fast record in p.
func (app *App) recheckOnBranch(raw []byte, sdkTx sdk.Tx, p *parallelTx) *abci.ResponseCheckTx {
	f := app.fastRecheck
	if f == nil || sdkTx == nil {
		return p.w.RecheckTx(raw, sdkTx)
	}
	pre := f.eligible(sdkTx)
	if pre == nil || !f.readSigner(p.w.Context().MultiStore(), pre) {
		return p.w.RecheckTx(raw, sdkTx)
	}
	res := p.w.RecheckTxWith(raw, sdkTx, func(ctx sdk.Context) sdk.Context { return ctx.WithValue(fastTraceKey{}, pre.trace) })
	p.pre, p.recKey, p.rec = pre, raw, f.build(p.w.Context().MultiStore(), pre, res)
	return res
}

// recheckParallel rechecks txs on parallel branches of check state, or returns nil, having
// changed nothing, if it expects sequential to be faster or the branches turn out dependent.
func (app *App) recheckParallel(txs [][]byte) []*abci.ResponseCheckTx {
	workers := app.recheckWorkers
	if workers <= 1 || len(txs) < 2 {
		return nil
	}
	f := app.fastRecheck
	if f != nil && f.shadow != nil {
		return nil // shadow mode compares each recheck with stock, sequentially
	}
	ps := make([]*parallelTx, len(txs))
	misses := 0
	for i, raw := range txs {
		p := &parallelTx{raw: raw}
		ps[i] = p
		if len(raw) > appconsts.MaxTxSize {
			continue
		}
		if !bytes.HasSuffix(raw, blobTxSuffix) && pffDecodeCandidate(raw) {
			p.cached, _ = app.recheckTxs.get(raw)
		}
		if f != nil {
			// stale here also keeps the workers from writing the shared depSets.
			_, p.fastRec = f.lookup(raw)
			if p.fastRec == nil || f.stale(p.fastRec) {
				misses++
			} else {
				// eligible txs have one signer, who pays the fee in utia: no decode needed
				p.hit = true
				p.keys = []string{"a:" + string(p.fastRec.signer)}
				p.fee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, p.fastRec.fee))
			}
		}
	}
	// Mostly fast hits: a parallel pass costs more than it saves.
	if f != nil && !app.recheckForceParallel && (misses < 64 || misses*6 < len(txs)) {
		return nil
	}

	parallel.ForEach(len(ps), func(i int) { ps[i].prepare(app) })()
	keys := make([][]string, len(ps))
	for i, p := range ps {
		keys[i] = p.keys
	}
	groups := groupRecheckTxs(keys)
	if len(groups) < 2 {
		return nil
	}
	unlock := app.LockCheckState()
	resps, err := app.runParallel(ps, groups, workers)
	unlock()
	if err != nil {
		app.Logger().Debug("parallel recheck fell back to sequential", "txs", len(txs), "groups", len(groups), "err", err)
		return nil
	}
	app.parallelRechecks++
	for _, p := range ps {
		if p.drop {
			app.recheckTxs.drop(p.raw)
		}
		if p.refill != nil {
			app.recheckTxs.add(p.raw, p.refill)
		}
		if f != nil {
			f.applyParallel(p)
		}
	}
	return resps
}

// runParallel runs groups on workers branches, largest first, and merges them unless they conflict.
// The caller holds LockCheckState.
func (app *App) runParallel(ps []*parallelTx, groups [][]int, workers int) ([]*abci.ResponseCheckTx, error) {
	exec, err := app.NewRecheckExecutor(len(groups), isFeeCollectorBalance)
	if err != nil {
		return nil, err
	}
	ctx, _ := app.CheckState()
	if app.fastRecheck != nil {
		app.fastRecheck.promiseEnv(ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())) // memoized before the workers share it
	}
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return len(groups[b]) - len(groups[a]) })
	var next atomic.Int64
	var panicked atomic.Pointer[string]
	var wg sync.WaitGroup
	for range min(workers, len(groups)) {
		wg.Go(func() {
			w := exec.NewWorker()
			for n := int(next.Add(1)) - 1; n < len(order); n = int(next.Add(1)) - 1 {
				w.BeginGroup(order[n])
				for _, i := range groups[order[n]] {
					p := ps[i]
					p.w = w
					func() {
						// The sequential rerun reproduces a panic outside the ante's recover.
						defer func() {
							if r := recover(); r != nil {
								msg := fmt.Sprintf("tx %d panicked: %v", i, r)
								panicked.Store(&msg)
							}
						}()
						p.res, p.err = app.checkTxLocked(&abci.RequestCheckTx{Tx: p.raw, Type: abci.CheckTxType_Recheck}, p)
					}()
				}
				w.EndGroup()
			}
		})
	}
	wg.Wait()
	if msg := panicked.Load(); msg != nil {
		return nil, errors.New(*msg)
	}
	if err := exec.Validate(workers); err != nil {
		return nil, err
	}
	merged, err := mergeFeeCollector(exec, ps, groups, ctx.KVGasConfig())
	if err != nil {
		return nil, err
	}
	exec.Merge(merged)
	resps := make([]*abci.ResponseCheckTx, len(ps))
	for i, p := range ps {
		if p.err == nil {
			resps[i] = p.res
		}
	}
	return resps, nil
}

// prepare decodes p's tx (unless its admission decode is cached) and derives its conflict
// keys, unless a fast hit is predicted (the worker decodes on a miss).
func (p *parallelTx) prepare(app *App) {
	if p.hit {
		return
	}
	tx := p.cached
	if tx == nil && len(p.raw) <= appconsts.MaxTxSize {
		p.decRaw = p.raw
		if bytes.HasSuffix(p.raw, blobTxSuffix) {
			btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(p.raw)
			if isBlob && err != nil {
				return // rejected before any state read
			}
			if isBlob {
				p.decRaw = btx.Tx
			}
		}
		p.dec, p.decErr = app.checkTxDecoder()(p.decRaw)
		tx = p.dec
	}
	p.keys, p.fee = recheckConflictKeys(tx)
}

// applyParallel counts p's fast recheck outcome and keeps its new record.
func (f *fastRecheck) applyParallel(p *parallelTx) {
	if p.fastHit {
		f.stats.hits++
	} else if p.fastMiss != "" {
		f.miss(p.fastMiss)
	}
	if p.pre != nil && p.res != nil {
		f.keep(p.recKey, p.pre, p.res, p.rec)
	}
}

// recheckConflictKeys returns the accounts that tie tx to other txs (signers, fee
// payer and granter, plus one lane for all IBC txs) and its fee. A tx that cannot
// be inspected, or whose accessors panic, gets the global lane.
func recheckConflictKeys(tx sdk.Tx) (keys []string, fee sdk.Coins) {
	if tx == nil {
		return nil, nil
	}
	defer func() {
		if recover() != nil {
			keys, fee = []string{"lane:global"}, nil
		}
	}()
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return []string{"lane:global"}, nil
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return []string{"lane:global"}, nil
	}
	for _, s := range signers {
		keys = append(keys, "a:"+string(s))
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		if p := feeTx.FeePayer(); len(p) > 0 {
			keys = append(keys, "a:"+string(p))
		}
		if g := feeTx.FeeGranter(); len(g) > 0 {
			keys = append(keys, "a:"+string(g))
		}
		fee = feeTx.GetFee()
	}
	for _, m := range tx.GetMsgs() {
		if strings.HasPrefix(sdk.MsgTypeURL(m), "/ibc.") {
			keys = append(keys, "lane:ibc")
			break
		}
	}
	return keys, fee
}

// groupRecheckTxs partitions txs, given each tx's conflict keys, so that txs sharing a key
// share a group. Groups list tx indices in ascending order; a tx without keys is alone.
func groupRecheckTxs(keys [][]string) [][]int {
	parent := make([]int, len(keys))
	for i := range parent {
		parent[i] = i
	}
	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	owner := make(map[string]int)
	for i, ks := range keys {
		for _, k := range ks {
			j, ok := owner[k]
			if !ok {
				owner[k] = i
				continue
			}
			if a, b := find(i), find(j); a != b {
				parent[max(a, b)] = min(a, b)
			}
		}
	}
	index := make(map[int]int)
	var groups [][]int
	for i := range keys {
		r := find(i)
		gi, ok := index[r]
		if !ok {
			gi = len(groups)
			index[r] = gi
			groups = append(groups, nil)
		}
		groups[gi] = append(groups[gi], i)
	}
	return groups
}

var feeCollectorAddr = authtypes.NewModuleAddress(authtypes.FeeCollectorName)

// feeCollectorBalancePrefix prefixes the fee collector's bank balances, which every
// fee-paying tx credits; groups write them additively and mergeFeeCollector sums them.
var feeCollectorBalancePrefix = balanceKey(feeCollectorAddr, "")

func isFeeCollectorBalance(k storetypes.StoreKey, key []byte) bool {
	return k.Name() == banktypes.StoreKey && bytes.HasPrefix(key, feeCollectorBalancePrefix)
}

// storedBalance decodes a stored balance; absent is zero.
func storedBalance(b []byte) (math.Int, error) {
	if b == nil {
		return math.ZeroInt(), nil
	}
	if v, ok := decodeBalance(b); ok {
		return v, nil
	}
	return math.Int{}, errors.New("malformed fee collector balance")
}

// storedLen is the byte length of v as a stored balance (0 when absent).
func storedLen(v math.Int) int {
	if v.IsZero() {
		return 0
	}
	b, err := sdk.IntValue.Encode(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// mergeFeeCollector sums the groups' fee collector credits and corrects each passing tx's
// GasUsed by the per-byte gas of the longer balance it sees sequentially (delta >= 0).
// It fails if that delta pushes a tx past its gas limit.
func mergeFeeCollector(exec *baseapp.RecheckExecutor, ps []*parallelTx, groups [][]int, gasCfg storetypes.GasConfig) (map[storetypes.StoreKey]map[string][]byte, error) {
	adds := exec.AdditiveWrites()
	var bankKey storetypes.StoreKey
	bases := make(map[string]math.Int) // denom -> base balance
	for k, m := range adds {
		bankKey = k
		for key, aw := range m {
			b, err := storedBalance(aw.Base)
			if err != nil {
				return nil, fmt.Errorf("decode fee collector balance: %w", err)
			}
			bases[key[len(feeCollectorBalancePrefix):]] = b
		}
	}
	if len(bases) == 0 {
		return nil, nil
	}

	groupOf := make([]int, len(ps))
	for gi, g := range groups {
		for _, i := range g {
			groupOf[i] = gi
		}
	}
	// running fee collector balance per denom, sequentially and in each group
	type groupDenom struct {
		group int
		denom string
	}
	seq := maps.Clone(bases)
	par := make(map[groupDenom]math.Int)
	read, write := int64(gasCfg.ReadCostPerByte), int64(gasCfg.WriteCostPerByte)
	for i, p := range ps {
		r := p.res
		if r == nil || p.err != nil || r.GasWanted <= 0 || p.fee.IsZero() {
			continue
		}
		passed := r.Code == abci.CodeTypeOK
		gi := groupOf[i]
		var delta int64
		for _, c := range p.fee {
			if _, ok := bases[c.Denom]; !ok {
				if !passed {
					continue
				}
				return nil, fmt.Errorf("fee denom %s has no fee collector write", c.Denom)
			}
			gd := groupDenom{gi, c.Denom}
			s, pv := seq[c.Denom], bases[c.Denom]
			if v, ok := par[gd]; ok {
				pv = v
			}
			sNext, pNext := s.Add(c.Amount), pv.Add(c.Amount)
			delta += read*int64(storedLen(s)-storedLen(pv)) + write*int64(storedLen(sNext)-storedLen(pNext))
			if !passed {
				continue // its credit was discarded
			}
			seq[c.Denom], par[gd] = sNext, pNext
		}
		if delta < 0 {
			return nil, fmt.Errorf("tx %d: negative fee collector gas delta", i)
		}
		// A failed tx may have credited the fee before failing: if the delta could push it out
		// of gas earlier, its sequential failure could differ. Its GasUsed is left uncorrected.
		if r.GasUsed+delta > r.GasWanted {
			return nil, fmt.Errorf("tx %d: fee collector gas delta %d exceeds its gas limit", i, delta)
		}
		if passed {
			r.GasUsed += delta
		}
	}

	// Cross-check: base plus every group's credits must equal base plus the passing txs' fees.
	merged := make(map[string][]byte)
	for key, aw := range adds[bankKey] {
		denom := key[len(feeCollectorBalancePrefix):]
		base := bases[denom]
		total := base
		for _, fv := range aw.Finals {
			v, err := storedBalance(fv)
			if err != nil {
				return nil, err
			}
			total = total.Add(v.Sub(base))
		}
		if !total.Equal(seq[denom]) {
			return nil, fmt.Errorf("fee collector %s: merged %s != base %s + fees = %s", denom, total, base, seq[denom])
		}
		if total.IsZero() {
			merged[key] = nil
			continue
		}
		b, err := sdk.IntValue.Encode(total)
		if err != nil {
			return nil, err
		}
		merged[key] = b
	}
	return map[storetypes.StoreKey]map[string][]byte{bankKey: merged}, nil
}
