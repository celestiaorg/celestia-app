package app

import (
	"errors"
	"hash/fnv"
	"slices"
	"sync"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// FuzzAnteTraceReplay checks the rule fast recheck rests on, using the real
// trace, record and replay code but no app: a pass whose recorded reads all read
// the same later makes the same writes and uses the same gas, except for the fee
// transfer, which the replay redoes on the current balances.
//
// A program decoded from the input stands in for the ante. Its writes and its
// verdict depend on every value it read. It runs traced on state S0 to make a
// record. The input then edits S0 into S1. Wherever the replay on S1 succeeds,
// the program run untraced on S1 must pass and leave byte-identical stores
// with the same gas, and on unchanged state the replay must succeed.
func FuzzAnteTraceReplay(f *testing.F) {
	for _, seed := range traceSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		runTraceReplay(t, data)
	})
}

// TestAnteTraceReplaySeeds checks that the seeds reach the outcomes they are named for.
func TestAnteTraceReplaySeeds(t *testing.T) {
	want := []traceOutcome{traceReplayed, traceMissed, traceNotRecorded, traceReplayed, traceMissed}
	seeds := traceSeeds()
	require.Len(t, seeds, len(want))
	for i, seed := range seeds {
		require.Equal(t, want[i], runTraceReplay(t, seed), "seed %d", i)
	}
}

const (
	traceHeaderWidth = 16
	traceRecordWidth = 4 // an op or an edit: kind, store, key, value
	traceMaxEdits    = 8
	traceMaxOps      = 24
	traceKeysPerKind = 5
)

// Program ops.
const (
	opGet = iota
	opHas
	opSet
	opSetEmpty
	opDelete
	opSkip   // skip the next ops when the reads so far hash to the op's parity
	opReject // fail the pass when the reads so far hash to the op's value
	opGas    // consume a fixed amount of gas, as signature checks do
	opFee    // the fee transfer, as bank's SendCoinsFromAccountToModule does it
	numOps
)

// State edits. The plain edits also reach the balances, making them absent,
// empty or a malformed one-byte value.
const (
	editSet = iota
	editSetEmpty
	editDelete
	editPayer // set the payer balance to a value around the fee or a decimal-length boundary
	editCollector
	numEdits
)

type traceOutcome int

const (
	traceNotRecorded traceOutcome = iota
	traceMissed
	traceReplayed
)

var errTracePass = errors.New("the program rejected the pass")

// traceEnv is the fixed part of every run: three stores, the first one bank.
type traceEnv struct {
	keys                       []*storetypes.KVStoreKey
	base                       storetypes.CommitMultiStore
	payer, payerIdx, fc, fcIdx []byte
}

var getTraceEnv = sync.OnceValue(func() *traceEnv {
	e := &traceEnv{base: store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())}
	for _, name := range []string{"bank", "acc", "params"} {
		k := storetypes.NewKVStoreKey(name)
		e.keys = append(e.keys, k)
		e.base.MountStoreWithDB(k, storetypes.StoreTypeIAVL, nil)
	}
	if err := e.base.LoadLatestVersion(); err != nil {
		panic(err)
	}
	payer := sdk.AccAddress("fast-recheck-payer-0")
	fc := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	e.payer, e.fc = balanceKey(payer), balanceKey(fc)
	e.payerIdx, e.fcIdx = append([]byte("denom/"), payer...), append([]byte("denom/"), fc...)
	return e
})

// key maps a key index to a key. Indexes 0-3 of the bank store are the two
// balances and their denom index entries. The program's own writes never reach
// the balances: only its fee transfer writes them.
func (e *traceEnv) key(store, idx byte, write bool) (storetypes.StoreKey, []byte) {
	s := int(store) % len(e.keys)
	i := int(idx) % (traceKeysPerKind + 4)
	if s == 0 {
		switch {
		case i == 0 && !write:
			return e.keys[0], e.payer
		case i == 1 && !write:
			return e.keys[0], e.fc
		case i == 2:
			return e.keys[0], e.payerIdx
		case i == 3:
			return e.keys[0], e.fcIdx
		}
	}
	return e.keys[s], []byte{'k', byte(i)}
}

type traceOp struct{ kind, store, key, val byte }

type traceRun struct {
	fee              math.Int
	gasLimit         uint64
	h0, h1           int64
	timeout          uint64
	init, ops, edits []traceOp
}

func decodeTraceRun(data []byte) traceRun {
	c := fuzzutil.New(data)
	h := c.Record(traceHeaderWidth)
	r := traceRun{
		fee:      math.NewInt(1 + int64(h.Uint16())),
		gasLimit: 1 + uint64(h.Uint32()%2_000_000),
		h0:       2 + int64(h.Intn(100)),
	}
	r.h1 = r.h0 + int64(h.Intn(4))
	if h.Bool() {
		r.timeout = uint64(r.h0 + int64(h.Intn(4)))
	}
	read := func(n int) []traceOp {
		ops := make([]traceOp, n)
		for i := range ops {
			rc := c.Record(traceRecordWidth)
			ops[i] = traceOp{kind: rc.Byte(), store: rc.Byte(), key: rc.Byte(), val: rc.Byte()}
		}
		return ops
	}
	nInit, nOps, nEdits := h.Intn(traceMaxEdits+1), h.Intn(traceMaxOps+1), h.Intn(traceMaxEdits+1)
	r.init, r.ops, r.edits = read(nInit), read(nOps), read(nEdits)
	return r
}

// edit applies one state edit to ms.
func (e *traceEnv) edit(ms storetypes.MultiStore, ed traceOp, fee math.Int) {
	store, key := e.key(ed.store, ed.key, false)
	kv := ms.GetKVStore(store)
	switch ed.kind % numEdits {
	case editSet:
		kv.Set(key, []byte{ed.val})
	case editSetEmpty:
		kv.Set(key, []byte{})
	case editDelete:
		kv.Delete(key)
	case editPayer:
		ms.GetKVStore(e.keys[0]).Set(e.payer, encodeBalance(traceBalance(fee, ed.val)))
	case editCollector:
		ms.GetKVStore(e.keys[0]).Set(e.fc, encodeBalance(traceBalance(fee, ed.val)))
	}
}

// traceBalance picks balances around the fee and around decimal-length
// boundaries, which change the gas the balance reads and writes cost.
func traceBalance(fee math.Int, v byte) math.Int {
	switch v % 4 {
	case 0:
		return fee.AddRaw(int64(v/4) - 2) // fee-2 .. fee+61
	case 1:
		pow := math.OneInt()
		for range v/4%18 + 1 {
			pow = pow.MulRaw(10)
		}
		return pow.AddRaw(int64(v/64%3) - 1) // 10^k-1 .. 10^k+1
	default:
		return fee.MulRaw(int64(v) + 1)
	}
}

// transfer moves fee from the payer to the fee collector with the store
// operations of bank's SendCoins on its Balances IndexedMap.
func (e *traceEnv) transfer(ctx sdk.Context, fee math.Int) error {
	bank := ctx.KVStore(e.keys[0])
	payer, err := traceDecode(bank.Get(e.payer))
	if err != nil || payer.LT(fee) {
		return errTracePass
	}
	if rest := payer.Sub(fee); rest.IsZero() {
		bank.Get(e.payer) // IndexedMap.Remove reads the value to unreference its indexes
		bank.Delete(e.payerIdx)
		bank.Delete(e.payer)
	} else {
		bank.Set(e.payerIdx, []byte{})
		bank.Set(e.payer, encodeBalance(rest))
	}
	fc, err := traceDecode(bank.Get(e.fc))
	if err != nil {
		return errTracePass
	}
	bank.Set(e.fcIdx, []byte{})
	bank.Set(e.fc, encodeBalance(fc.Add(fee)))
	return nil
}

// traceDecode decodes a balance as bank does; absent is zero, empty is malformed.
func traceDecode(raw []byte) (math.Int, error) {
	if raw == nil {
		return math.ZeroInt(), nil
	}
	v, err := sdk.IntValue.Decode(raw)
	if err == nil && v.IsNil() {
		err = errTracePass
	}
	return v, err
}

// program runs r.ops on ctx as an ante pass. Its verdict and writes depend on
// every value it read, so a replay is only right if it validated them all.
func (e *traceEnv) program(ctx sdk.Context, r traceRun) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(storetypes.ErrorOutOfGas); !ok {
				panic(p)
			}
			err = errTracePass
		}
	}()
	if r.timeout > 0 && uint64(ctx.BlockHeight()) > r.timeout {
		return errTracePass
	}
	h := fnv.New64a()
	mix := func(v []byte) {
		if v == nil {
			h.Write([]byte{0})
			return
		}
		h.Write([]byte{1, byte(len(v))})
		h.Write(v)
	}
	skip := 0
	for _, op := range r.ops {
		if skip > 0 {
			skip--
			continue
		}
		kind := op.kind % numOps
		store, key := e.key(op.store, op.key, kind == opSet || kind == opSetEmpty || kind == opDelete)
		kv := ctx.KVStore(store)
		switch kind {
		case opGet:
			mix(kv.Get(key))
		case opHas:
			if kv.Has(key) {
				mix([]byte{})
			} else {
				mix(nil)
			}
		case opSet:
			kv.Set(key, append([]byte{op.val}, h.Sum(nil)[:1]...))
		case opSetEmpty:
			kv.Set(key, []byte{})
		case opDelete:
			kv.Delete(key)
		case opSkip:
			if h.Sum64()%2 == uint64(op.val%2) {
				skip = int(op.val % 4)
			}
		case opReject:
			if h.Sum64()%16 == uint64(op.val%16) {
				return errTracePass
			}
		case opGas:
			ctx.GasMeter().ConsumeGas(uint64(op.val)*100, "fixed")
		case opFee:
			if err := e.transfer(ctx, r.fee); err != nil {
				return err
			}
		}
	}
	return nil
}

func runTraceReplay(t *testing.T, data []byte) traceOutcome {
	e := getTraceEnv()
	r := decodeTraceRun(data)
	nop := log.NewNopLogger()
	s0 := e.base.CacheMultiStore() // S0, edited into S1 after the record
	s0.GetKVStore(e.keys[0]).Set(e.payer, encodeBalance(r.fee.MulRaw(1000)))
	s0.GetKVStore(e.keys[0]).Set(e.fc, encodeBalance(math.NewInt(123)))
	for _, ed := range r.init {
		e.edit(s0, ed, r.fee)
	}

	f := &fastRecheck{records: map[string]*fastRecord{}, maxRecs: 1, maxBytes: 1 << 20, bank: e.keys[0], fcKey: e.fc}
	tr := newAnteTrace(e.keys[0], e.payer, e.fc)
	recCtx := sdk.NewContext(traceMS{cms: s0.CacheMultiStore(), tr: tr}, cmtproto.Header{Height: r.h0}, true, nop).
		WithGasMeter(storetypes.NewGasMeter(r.gasLimit))
	res := &abci.ResponseCheckTx{GasWanted: int64(r.gasLimit)}
	if err := e.program(recCtx, r); err != nil {
		res.Code = 1
	}
	res.GasUsed = int64(recCtx.GasMeter().GasConsumed())
	f.end([]byte("tx"), traceTx{fee: r.fee, timeout: r.timeout}, tr, res, nil)
	rec := f.records["tx"]
	if rec == nil {
		return traceNotRecorded
	}

	for _, ed := range r.edits {
		e.edit(s0, ed, r.fee)
	}
	replayCtx := sdk.NewContext(s0.CacheMultiStore(), cmtproto.Header{Height: r.h1}, true, nop)
	got, ok := f.replay(replayCtx, rec)
	if !ok {
		// Nothing changed and the tx has not timed out: the replay must not miss.
		require.False(t, len(r.edits) == 0 && (r.timeout == 0 || uint64(r.h1) <= r.timeout), "replay missed on unchanged state")
		return traceMissed
	}
	fullCtx := sdk.NewContext(s0.CacheMultiStore(), cmtproto.Header{Height: r.h1}, true, nop).
		WithGasMeter(storetypes.NewGasMeter(r.gasLimit))
	require.NoError(t, e.program(fullCtx, r), "replay accepted a pass the program rejects")
	require.Equal(t, int64(fullCtx.GasMeter().GasConsumed()), got.GasUsed, "gas used")
	require.Equal(t, int64(r.gasLimit), got.GasWanted)
	for _, k := range e.keys {
		require.Empty(t, firstKVDiff(replayCtx.MultiStore().GetKVStore(k), fullCtx.MultiStore().GetKVStore(k)), k.Name())
	}
	return traceReplayed
}

// traceSeeds lays out a header, then init edits, ops and edits.
func traceSeeds() [][]byte {
	header := func(nInit, nOps, nEdits byte) []byte {
		// fee 2000, gas limit 1M, h0, h1 delta 1, no timeout, then the counts.
		return []byte{0x07, 0xd0, 0x00, 0x0f, 0x42, 0x40, 10, 1, 0, nInit, nOps, nEdits}
	}
	op := func(kind, store, key, val byte) []byte { return []byte{kind, store, key, val} }
	// An ante-like pass: params and account reads, a signature's gas, the fee,
	// a sequence write read back, then a write that depends on everything read.
	ante := [][]byte{
		op(opGet, 2, 4, 0), op(opGet, 1, 5, 0), op(opHas, 2, 6, 0), op(opGas, 0, 0, 200),
		op(opFee, 0, 0, 0), op(opSet, 1, 6, 7), op(opGet, 1, 6, 0), op(opSet, 2, 8, 1),
	}
	init := [][]byte{op(editSet, 2, 4, 9), op(editSet, 1, 5, 3)}
	build := func(ops, edits [][]byte) []byte {
		out := fuzzutil.Records(traceHeaderWidth, header(byte(len(init)), byte(len(ops)), byte(len(edits))))
		return append(out, fuzzutil.Records(traceRecordWidth, slices.Concat(init, ops, edits)...)...)
	}
	return [][]byte{
		// Unread keys and both balances change, the latter across decimal lengths: replays.
		build(ante, [][]byte{op(editSet, 2, 7, 1), op(editPayer, 0, 0, 1+4*5), op(editCollector, 0, 0, 3)}),
		// A value the pass read changes: misses.
		build(ante, [][]byte{op(editSet, 2, 4, 8)}),
		// No fee transfer: nothing to replay.
		build(ante[:4], nil),
		// A key the pass wrote before reading it changes: still replays.
		build(ante, [][]byte{op(editSet, 1, 6, 200)}),
		// The payer is left with exactly the fee: the full pass removes the balance, so it misses.
		build(ante, [][]byte{op(editPayer, 0, 0, 8)}),
	}
}
