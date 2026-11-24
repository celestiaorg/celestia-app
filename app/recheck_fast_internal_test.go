package app

import (
	"maps"
	"slices"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v12/app/encoding"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func TestAnteTrace(t *testing.T) {
	bank, other := storetypes.NewKVStoreKey("bank"), storetypes.NewKVStoreKey("other")
	payer, fc := []byte("payer"), []byte("fc")
	newTrace := func() *anteTrace { return newAnteTrace(bank, payer, fc) }

	t.Run("a key read twice is recorded once", func(t *testing.T) {
		tr := newTrace()
		tr.onRead(other, []byte("k"), []byte("v"), true)
		tr.onRead(other, []byte("k"), []byte("v"), false)
		require.Equal(t, []kvOp{{store: other, key: []byte("k"), value: []byte("v")}}, tr.reads)
	})
	t.Run("a read of a key the pass wrote is not recorded", func(t *testing.T) {
		tr := newTrace()
		tr.onWrite(other, []byte("k"), []byte("v"))
		tr.onRead(other, []byte("k"), []byte("v"), false)
		require.Empty(t, tr.reads)
		require.Len(t, tr.writes, 1)
		require.False(t, tr.failed)
	})
	t.Run("balances must see one Get then one Set", func(t *testing.T) {
		for name, ops := range map[string]func(tr *anteTrace){
			"set first": func(tr *anteTrace) { tr.onWrite(bank, payer, []byte("1")) },
			"two gets":  func(tr *anteTrace) { tr.onRead(bank, fc, []byte("1"), false); tr.onRead(bank, fc, []byte("1"), false) },
			"has":       func(tr *anteTrace) { tr.onRead(bank, fc, []byte{}, true) },
			"delete":    func(tr *anteTrace) { tr.onRead(bank, payer, []byte("1"), false); tr.onWrite(bank, payer, nil) },
			"two writes": func(tr *anteTrace) {
				tr.onRead(bank, payer, []byte("2"), false)
				tr.onWrite(bank, payer, []byte("1"))
				tr.onWrite(bank, payer, []byte("0"))
			},
		} {
			tr := newTrace()
			ops(tr)
			require.True(t, tr.failed, name)
		}
		tr := newTrace()
		tr.onRead(bank, payer, []byte("2"), false)
		tr.onWrite(bank, payer, []byte("1"))
		require.False(t, tr.failed)
		require.Empty(t, tr.reads)
	})
	t.Run("paused reads are skipped and paused writes fail", func(t *testing.T) {
		tr := newTrace()
		tr.paused = true
		tr.onRead(other, []byte("k"), []byte("v"), false)
		require.Empty(t, tr.reads)
		tr.onWrite(other, []byte("k"), []byte("v"))
		require.True(t, tr.failed)
	})
}

// utiaFeePayer must not panic on txs whose accessors do (no fee, a bad granter).
func TestUtiaFeePayerMalformed(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	addr := sdk.AccAddress([]byte("fast-recheck-address"))
	msgAny, err := codectypes.NewAnyWithValue(banktypes.NewMsgSend(addr, addr, nil))
	require.NoError(t, err)
	single := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_DIRECT}}}
	for name, fee := range map[string]*txtypes.Fee{"no fee": nil, "bad granter": {Amount: sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)), Granter: "garbage"}} {
		raw, err := (&txtypes.Tx{
			Body:       &txtypes.TxBody{Messages: []*codectypes.Any{msgAny}},
			AuthInfo:   &txtypes.AuthInfo{SignerInfos: []*txtypes.SignerInfo{{ModeInfo: single}}, Fee: fee},
			Signatures: [][]byte{make([]byte, 64)},
		}).Marshal()
		require.NoError(t, err, name)
		sdkTx, err := enc.TxConfig.TxDecoder()(raw)
		require.NoError(t, err, name)
		if fee != nil {
			// The fee passes utiaFeePayer's checks, so it reaches the panicking accessor.
			require.Panics(t, func() { sdkTx.(sdk.FeeTx).FeeGranter() }, name)
		}
		require.Nil(t, utiaFeePayer(sdkTx), name)
	}
}

// Every way out of the traced multistore that the replay cannot follow must
// mark the pass unrecordable.
func TestAnteTraceUntracedAccessFails(t *testing.T) {
	k := storetypes.NewKVStoreKey("other")
	base := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
	base.MountStoreWithDB(k, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, base.LoadLatestVersion())
	traced := func() (traceMS, *anteTrace) {
		tr := newAnteTrace(storetypes.NewKVStoreKey("bank"), []byte("payer"), []byte("fc"))
		return traceMS{cms: base.CacheMultiStore(), tr: tr}, tr
	}
	for name, access := range map[string]func(ms traceMS){
		"iterator":         func(ms traceMS) { ms.GetKVStore(k).Iterator(nil, nil).Close() },
		"reverse iterator": func(ms traceMS) { ms.GetKVStore(k).ReverseIterator(nil, nil).Close() },
		"kv cache wrap":    func(ms traceMS) { ms.GetKVStore(k).CacheWrap() },
		"kv traced wrap":   func(ms traceMS) { ms.GetKVStore(k).CacheWrapWithTrace(nil, nil) },
		"nested branch":    func(ms traceMS) { ms.CacheMultiStore() },
		"cache wrap":       func(ms traceMS) { ms.CacheWrap() },
		"traced wrap":      func(ms traceMS) { ms.CacheWrapWithTrace(nil, nil) },
		"versioned branch": func(ms traceMS) {
			defer func() { _ = recover() }() // a cache multistore has no versions
			_, _ = ms.CacheMultiStoreWithVersion(1)
		},
	} {
		ms, tr := traced()
		access(ms)
		require.True(t, tr.failed, name)
	}
	ms, tr := traced()
	ms.GetStore(k).(storetypes.KVStore).Get([]byte("k"))
	require.False(t, tr.failed, "GetStore of a KV store is traced")
	require.Len(t, tr.reads, 1)
}

// A missing setting enables fast recheck, and only an explicit false disables it.
func TestFastRecheckEnabled(t *testing.T) {
	require.True(t, fastRecheckEnabled(simtestutil.AppOptionsMap{}), "a missing setting enables it")
	require.True(t, fastRecheckEnabled(simtestutil.AppOptionsMap{FlagFastRecheck: true}))
	require.True(t, fastRecheckEnabled(simtestutil.AppOptionsMap{FlagFastRecheck: "true"}))
	require.False(t, fastRecheckEnabled(simtestutil.AppOptionsMap{FlagFastRecheck: false}))
	require.False(t, fastRecheckEnabled(simtestutil.AppOptionsMap{FlagFastRecheck: "false"}))
}

// The records stay within their entry and byte budgets, and a Commit drops the
// records no recheck used since the previous Commit.
func TestFastRecheckRecordBudget(t *testing.T) {
	bank, other := storetypes.NewKVStoreKey("bank"), storetypes.NewKVStoreKey("other")
	payer, fc := []byte("payer"), []byte("fc")
	fee := math.NewInt(10)
	f := &fastRecheck{records: map[string]*fastRecord{}, maxRecs: 3, maxBytes: 100, bank: bank, fcKey: fc}
	record := func(raw string, valueLen int) {
		tr := newAnteTrace(bank, payer, fc)
		tr.onRead(bank, payer, encodeBalance(math.NewInt(100)), false)
		tr.onWrite(bank, payer, encodeBalance(math.NewInt(90)))
		tr.onRead(bank, fc, encodeBalance(math.NewInt(0)), false)
		tr.onWrite(bank, fc, encodeBalance(fee))
		tr.onRead(other, []byte("k"), make([]byte, valueLen), false)
		f.end([]byte(raw), traceTx{fee: fee}, tr, &abci.ResponseCheckTx{GasWanted: 1, GasUsed: 1}, nil)
	}
	total := func() (n int) {
		for _, r := range f.records {
			n += r.size
		}
		return n
	}
	record("a", 10)
	record("b", 10)
	require.Len(t, f.records, 2)
	record("a", 20) // a second pass of a replaces its record
	require.Len(t, f.records, 2)
	require.Equal(t, total(), f.bytes)
	record("c", 80) // over the byte budget
	require.NotContains(t, f.records, "c")
	record("c", 10)
	record("d", 1) // over the entry budget
	require.Len(t, f.records, 3)
	require.NotContains(t, f.records, "d")
	require.Equal(t, total(), f.bytes)
	record("a", 200) // a replacement that no longer fits drops the old record
	require.NotContains(t, f.records, "a")
	require.Equal(t, total(), f.bytes)

	f.commit() // records made before this Commit count as used in its block
	f.records["b"].used = f.commits
	f.commit()
	require.Equal(t, []string{"b"}, slices.Collect(maps.Keys(f.records)))
	require.Equal(t, total(), f.bytes)
}

func encodeBalance(v math.Int) []byte {
	b, err := sdk.IntValue.Encode(v)
	if err != nil {
		panic(err)
	}
	return b
}

// traceTx is a fee-paying tx, with what fastRecheck.end reads from one.
type traceTx struct {
	fee     math.Int
	timeout uint64
}

func (traceTx) GetMsgs() []sdk.Msg                    { return nil }
func (traceTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (traceTx) GetGas() uint64                        { return 0 }
func (tx traceTx) GetFee() sdk.Coins                  { return sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, tx.fee)) }
func (traceTx) FeePayer() []byte                      { return nil }
func (traceTx) FeeGranter() []byte                    { return nil }
func (tx traceTx) GetTimeoutHeight() uint64           { return tx.timeout }
