package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"cosmossdk.io/math"
	"cosmossdk.io/store/cachekv"
	"cosmossdk.io/store/cachemulti"
	"cosmossdk.io/store/dbadapter"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/parallel"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// maxRecheckWorkers bounds the workers: more did not recheck faster on 16 cores.
const maxRecheckWorkers = 8

// recheckParallel rechecks txs in groups that share no account, on worker goroutines that read the
// committed state through stores recording every access. It merges the results into check state
// unless the groups turn out to depend on each other; then it returns nil, having changed nothing.
func (app *App) recheckParallel(txs [][]byte) []*abci.ResponseCheckTx {
	workers := min(maxRecheckWorkers, runtime.GOMAXPROCS(0))
	// The branches read the committed stores, which equal check state only until it is written.
	if workers < 2 || len(txs) < 2 || app.checkStateDirty {
		return nil
	}
	groups := groupRecheckTxs(app.recheckConflictKeys(txs))
	if len(groups) < 2 {
		return nil
	}
	resps, err := app.recheckGroups(txs, groups, workers)
	if err != nil {
		app.Logger().Debug("parallel recheck fell back to sequential", "txs", len(txs), "groups", len(groups), "err", err)
		return nil
	}
	return resps
}

// recheckGroups runs groups on workers, largest first, then merges their writes unless they conflict.
func (app *App) recheckGroups(txs [][]byte, groups [][]int, workers int) ([]*abci.ResponseCheckTx, error) {
	cms := app.CommitMultiStore()
	named, ok := cms.(interface {
		StoreKeysByName() map[string]storetypes.StoreKey
	})
	if !ok {
		return nil, fmt.Errorf("commit multistore %T does not list its stores", cms)
	}
	keys := named.StoreKeysByName()
	workers = min(workers, len(groups))
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return len(groups[b]) - len(groups[a]) })

	resps := make([]*abci.ResponseCheckTx, len(txs))
	logs := make([]*recheckLog, workers)
	var next atomic.Int64
	var panicked atomic.Bool
	var wg sync.WaitGroup
	for w := range logs {
		log := &recheckLog{}
		logs[w] = log
		stores := make(map[storetypes.StoreKey]storetypes.CacheWrapper, len(keys))
		for _, k := range keys {
			stores[k] = trackStore{KVStore: cms.GetKVStore(k), key: k, log: log}
		}
		ms := cachemulti.NewFromKVStore(dbadapter.Store{DB: dbm.NewMemDB()}, stores, keys, nil, nil)
		run := func(req *abci.RequestCheckTx, sdkTx sdk.Tx) (*abci.ResponseCheckTx, error) {
			return app.RecheckTxOn(ms, req.Tx, sdkTx), nil
		}
		wg.Go(func() {
			defer func() {
				// the sequential rerun reproduces the panic
				if recover() != nil {
					panicked.Store(true)
				}
			}()
			for n := int(next.Add(1)) - 1; n < len(order) && !panicked.Load(); n = int(next.Add(1)) - 1 {
				log.group = order[n]
				for _, i := range groups[order[n]] {
					if res, err := app.checkTx(&abci.RequestCheckTx{Tx: txs[i], Type: abci.CheckTxType_Recheck}, run); err == nil {
						resps[i] = res
					}
				}
				// hands the group's writes to log and empties ms for the next group
				ms.Write()
			}
		})
	}
	wg.Wait()
	if panicked.Load() {
		return nil, errors.New("a recheck panicked")
	}
	if err := validateRecheck(logs); err != nil {
		return nil, err
	}
	unlock := app.LockCheckState()
	defer unlock()
	ctx, _ := app.CheckState()
	bank := keys[banktypes.StoreKey]
	fees, credited, gasBound, err := sumFeeCollectorCredits(logs, cms.GetKVStore(bank).Get([]byte(feeCollectorBalanceKey)), ctx.KVGasConfig())
	if err != nil {
		return nil, err
	}
	for i, r := range resps {
		// Sequentially a tx pays up to gasBound more, which must change no verdict, code or log. A failed
		// tx's own credit is not in the sum, so the balance it wrote may be one byte longer still.
		need := gasBound
		if r != nil && r.Code != abci.CodeTypeOK {
			need += int64(ctx.KVGasConfig().WriteCostPerByte)
		}
		if r != nil && r.GasWanted > 0 && r.GasUsed+need > r.GasWanted {
			return nil, fmt.Errorf("tx %d: gas left %d < fee collector bound %d", i, r.GasWanted-r.GasUsed, need)
		}
	}
	if app.beforeRecheckMerge != nil {
		if err := app.beforeRecheckMerge(); err != nil {
			return nil, err
		}
	}

	checkState := ctx.MultiStore()
	for _, log := range logs {
		for _, w := range log.writes {
			if !isFeeCollectorBalance(w.storeKey) {
				setOrDelete(checkState.GetKVStore(w.store), w.key, w.value)
			}
		}
	}
	if credited {
		setOrDelete(checkState.GetKVStore(bank), feeCollectorBalanceKey, fees)
	}
	return resps, nil
}

func setOrDelete(s storetypes.KVStore, key string, value []byte) {
	if value == nil {
		s.Delete([]byte(key))
	} else {
		s.Set([]byte(key), value)
	}
}

type storeKey struct {
	store storetypes.StoreKey
	key   string
}

type keyAccess struct {
	storeKey
	group int
	value []byte // writes only: the value written, nil if deleted
}

type rangeAccess struct {
	store      storetypes.StoreKey
	group      int
	start, end []byte
}

// recheckLog is what a worker's groups read, iterated over and wrote.
type recheckLog struct {
	group  int // the group running now
	reads  []keyAccess
	writes []keyAccess
	ranges []rangeAccess
}

// trackStore is a worker's view of a committed store. The worker's cachekv above it serves the
// running group's own reads and writes, so what reaches it is that group's first read of a key,
// an iterated range, or, when the group ends, its final writes.
type trackStore struct {
	storetypes.KVStore
	key storetypes.StoreKey
	log *recheckLog
}

func (s trackStore) read(key []byte) {
	// every group credits the fee collector; sumFeeCollectorCredits sums the credits
	if k := (storeKey{s.key, string(key)}); !isFeeCollectorBalance(k) {
		s.log.reads = append(s.log.reads, keyAccess{storeKey: k, group: s.log.group})
	}
}

func (s trackStore) Get(key []byte) []byte {
	s.read(key)
	return s.KVStore.Get(key)
}

func (s trackStore) Has(key []byte) bool {
	s.read(key)
	return s.KVStore.Has(key)
}

func (s trackStore) Set(key, value []byte) {
	s.log.writes = append(s.log.writes, keyAccess{storeKey: storeKey{s.key, string(key)}, group: s.log.group, value: value})
}

func (s trackStore) Delete(key []byte) {
	s.log.writes = append(s.log.writes, keyAccess{storeKey: storeKey{s.key, string(key)}, group: s.log.group})
}

func (s trackStore) iterated(start, end []byte) {
	s.log.ranges = append(s.log.ranges, rangeAccess{s.key, s.log.group, bytes.Clone(start), bytes.Clone(end)})
}

func (s trackStore) Iterator(start, end []byte) storetypes.Iterator {
	s.iterated(start, end)
	return s.KVStore.Iterator(start, end)
}

func (s trackStore) ReverseIterator(start, end []byte) storetypes.Iterator {
	s.iterated(start, end)
	return s.KVStore.ReverseIterator(start, end)
}

// CacheWrap is not called (cachemulti wraps trackStores itself); it keeps branches from bypassing the log.
func (s trackStore) CacheWrap() storetypes.CacheWrap { return cachekv.NewStore(s) }

func (s trackStore) CacheWrapWithTrace(io.Writer, storetypes.TraceContext) storetypes.CacheWrap {
	return s.CacheWrap()
}

// validateRecheck fails unless no group read or iterated over a key another group wrote, and
// groups that wrote the same key wrote the same value (fee collector balances are summed instead).
func validateRecheck(logs []*recheckLog) error {
	type writer struct {
		group  int
		value  []byte
		shared bool // written by several groups
	}
	written := make(map[storeKey]*writer)
	for _, log := range logs {
		for _, w := range log.writes {
			prev, ok := written[w.storeKey]
			if !ok {
				written[w.storeKey] = &writer{group: w.group, value: w.value}
				continue
			}
			if !isFeeCollectorBalance(w.storeKey) && ((prev.value == nil) != (w.value == nil) || !bytes.Equal(prev.value, w.value)) {
				return fmt.Errorf("groups %d and %d wrote %s/%X", prev.group, w.group, w.store.Name(), w.key)
			}
			prev.shared = true
		}
	}
	for _, log := range logs {
		for _, r := range log.reads {
			if w, ok := written[r.storeKey]; ok && (w.shared || w.group != r.group) {
				return fmt.Errorf("group %d read %s/%X written by group %d", r.group, r.store.Name(), r.key, w.group)
			}
		}
		for _, r := range log.ranges {
			for k, w := range written {
				if k.store == r.store && (w.shared || w.group != r.group) &&
					(r.start == nil || k.key >= string(r.start)) && (r.end == nil || k.key < string(r.end)) {
					return fmt.Errorf("group %d iterated over %s/%X written by group %d", r.group, r.store.Name(), k.key, w.group)
				}
			}
		}
	}
	return nil
}

// feeCollectorBalanceKey is the bank key of the fee collector's utia balance. Every fee-paying tx
// credits it (fees are utia only), so groups read it as committed and their credits are summed.
var feeCollectorBalanceKey = string(slices.Concat(banktypes.BalancesPrefix.Bytes(),
	address.MustLengthPrefix(authtypes.NewModuleAddress(authtypes.FeeCollectorName)), []byte(appconsts.BondDenom)))

func isFeeCollectorBalance(k storeKey) bool {
	return k.key == feeCollectorBalanceKey && k.store.Name() == banktypes.StoreKey
}

// sumFeeCollectorCredits returns the fee collector balance as committed plus every group's credit.
// A tx reads and writes it once, between committed and sum, so sequentially it pays at most
// gasBound = growth × (read + write per byte) more than on its branch.
func sumFeeCollectorCredits(logs []*recheckLog, committed []byte, gas storetypes.GasConfig) (sum []byte, credited bool, gasBound int64, err error) {
	// strict decimal, not the legacy coin format: the bound needs length to grow with the value
	decode := func(bz []byte) (math.Int, error) {
		if bz == nil {
			return math.ZeroInt(), nil
		}
		return sdk.IntValue.Decode(bz)
	}
	base, err := decode(committed)
	if err != nil {
		return nil, false, 0, err
	}
	total := base
	for _, log := range logs {
		for _, w := range log.writes {
			if isFeeCollectorBalance(w.storeKey) {
				v, err := decode(w.value)
				if err != nil {
					return nil, false, 0, err
				}
				if v.LT(base) {
					return nil, false, 0, fmt.Errorf("group %d lowered the fee collector balance", w.group)
				}
				total, credited = total.Add(v.Sub(base)), true
			}
		}
	}
	if !total.IsZero() {
		if sum, err = sdk.IntValue.Encode(total); err != nil {
			return nil, false, 0, err
		}
	}
	return sum, credited, int64(len(sum)-len(committed)) * int64(gas.ReadCostPerByte+gas.WriteCostPerByte), nil
}

// recheckConflictKeys decodes txs in parallel and returns, for each, the accounts that tie it to
// other txs (signers, fee payer and granter, plus one lane for all IBC txs). A tx that cannot be
// inspected gets the global lane; one that does not decode reads no state and gets no key.
func (app *App) recheckConflictKeys(txs [][]byte) [][]string {
	keys := make([][]string, len(txs))
	parallel.ForEach(len(txs), func(i int) {
		raw := txs[i]
		tx, cached := app.recheckTxs.get(raw)
		if !cached {
			if btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(raw); isBlob && err == nil {
				raw = btx.Tx
			}
			tx, _ = app.checkTxDecoder()(raw)
		}
		keys[i] = conflictKeys(tx)
	})()
	return keys
}

func conflictKeys(tx sdk.Tx) (keys []string) {
	if tx == nil {
		return nil
	}
	// a panic here would escape parallel.ForEach and stop the node; runTx recovers it later
	defer func() {
		if recover() != nil {
			keys = []string{"lane:global"}
		}
	}()
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return []string{"lane:global"}
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return []string{"lane:global"}
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
	}
	for _, m := range tx.GetMsgs() {
		if strings.HasPrefix(sdk.MsgTypeURL(m), "/ibc.") {
			return append(keys, "lane:ibc")
		}
	}
	return keys
}

// groupRecheckTxs partitions txs, given each tx's conflict keys, so that txs sharing a key share
// a group. Groups list tx indices in ascending order; a tx without keys is alone.
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
			} else if a, b := find(i), find(j); a != b {
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
