package app

import (
	"errors"
	"math"
	"sync"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

const (
	execFuzzMaxMsgs = 8
	execFuzzRecord  = 6 // bytes per message: action, key, value, gas (uint16), event flag
)

// Message actions. Each handler first writes its key and may emit an event,
// then acts; a write therefore precedes every failure.
const (
	execSucceed = iota
	execError
	execPanic
	execConsumeGas
	execUnregistered
	execActions
)

// execFuzzTxConfig is built once; the registry it builds is costly per input.
var execFuzzTxConfig = sync.OnceValue(func() client.TxConfig {
	return encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
})

type execStep struct {
	action, key, value byte
	gas                uint64
	event              bool
}

// scriptRouter routes message i of the tx to the handler of script step i,
// identified by the send amount i+1.
type scriptRouter struct {
	handlers []baseapp.MsgServiceHandler
}

func (r scriptRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	i := msg.(*banktypes.MsgSend).Amount[0].Amount.Int64() - 1
	return r.handlers[i]
}

func (r scriptRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return nil }

// FuzzExecuteTxMsgs runs a tx of up to eight messages whose handlers write a
// key, optionally emit an event, then succeed, fail, panic, consume gas or
// turn out to be unregistered, under a fuzz-chosen gas limit. runMsgs and
// executeTxMsgs must stop at the first failing message, turn every panic
// (including running out of gas) into an error, and return an error exactly
// when a message failed. runMsgs keeps the writes made up to the failure on
// the context it was given; executeTxMsgs keeps the store and the events of
// the parent context unchanged on failure and applies every write and event,
// in order, on success.
func FuzzExecuteTxMsgs(f *testing.F) {
	f.Add(uint32(0), fuzzutil.Records(execFuzzRecord, []byte{execSucceed, 1, 1, 0, 0, 1}))
	for action := range byte(execActions) {
		f.Add(uint32(50_000), fuzzutil.Records(execFuzzRecord,
			[]byte{execSucceed, 1, 1, 0, 10, 1}, []byte{action, 2, 2, 0xff, 0xff, 1}, []byte{execSucceed, 3, 3}))
		f.Add(uint32(0), fuzzutil.Records(execFuzzRecord, []byte{action, 1, 1, 1, 0, 0}, []byte{action, 1, 2, 1, 0, 1}))
	}
	f.Fuzz(func(t *testing.T, gasLimit uint32, script []byte) {
		if len(script) > execFuzzMaxMsgs*execFuzzRecord {
			return
		}
		_ = fuzzExecuteTxMsgs(t, uint64(gasLimit), script, false)
		_ = fuzzExecuteTxMsgs(t, uint64(gasLimit), script, true)
	})
}

func fuzzExecuteTxMsgs(t *testing.T, gasLimit uint64, script []byte, branch bool) error {
	var steps []execStep
	for c := fuzzutil.New(script); len(steps) < execFuzzMaxMsgs && !c.Exhausted(); {
		r := c.Record(execFuzzRecord)
		steps = append(steps, execStep{action: r.Byte() % execActions, key: r.Byte() % 4, value: r.Byte(), gas: uint64(r.Uint16()) * 64, event: r.Bool()})
	}
	if len(steps) == 0 {
		return nil
	}

	storeKey := storetypes.NewKVStoreKey("exec")
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())
	meter := storetypes.NewInfiniteGasMeter()
	if gasLimit != 0 {
		meter = storetypes.NewGasMeter(gasLimit)
	}
	ctx := sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger()).WithGasMeter(meter)
	// Store access is charged to a separate meter, so only the scripted
	// charges reach the tx meter and the model can predict running out.
	kv := func(ctx sdk.Context) storetypes.KVStore {
		return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).KVStore(storeKey)
	}
	kv(ctx).Set([]byte{0xff}, []byte("pre-existing"))
	ctx.EventManager().EmitEvent(sdk.NewEvent("pre-existing"))

	// The model: run the steps up to and including the first failure.
	want := map[byte]byte{}
	var wantEvents sdk.Events
	wantFail, calls, gas := false, 0, uint64(0)
	for _, s := range steps {
		if s.action == execUnregistered {
			wantFail = true
			break
		}
		calls++
		want[s.key] = s.value
		if s.event {
			wantEvents = append(wantEvents, sdk.NewEvent("step", sdk.NewAttribute("key", string(rune('a'+s.key)))))
		}
		if s.action == execConsumeGas {
			gas += s.gas
		}
		if s.action == execError || s.action == execPanic || (s.action == execConsumeGas && gasLimit != 0 && gas > gasLimit) {
			wantFail = true
			break
		}
	}

	handlers := make([]baseapp.MsgServiceHandler, len(steps))
	gotCalls := 0
	for i, s := range steps {
		if s.action == execUnregistered {
			continue
		}
		handlers[i] = func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			gotCalls++
			kv(ctx).Set([]byte{s.key}, []byte{s.value})
			if s.event {
				ctx.EventManager().EmitEvent(sdk.NewEvent("step", sdk.NewAttribute("key", string(rune('a'+s.key)))))
			}
			switch s.action {
			case execError:
				return nil, errors.New("scripted failure")
			case execPanic:
				panic("scripted panic")
			case execConsumeGas:
				ctx.GasMeter().ConsumeGas(s.gas, "scripted")
			}
			return &sdk.Result{}, nil
		}
	}
	router := scriptRouter{handlers: handlers}
	msgs := make([]sdk.Msg, len(steps))
	addr := sdk.AccAddress(make([]byte, 20))
	for i := range msgs {
		msgs[i] = banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin("utia", int64(i+1))))
	}

	var err error
	if branch {
		builder := execFuzzTxConfig().NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		err = executeTxMsgs(ctx, builder.GetTx(), router)
	} else {
		err = runMsgs(ctx, msgs, router)
	}
	require.Equal(t, wantFail, err != nil, "steps %+v: %v", steps, err)
	require.Equal(t, calls, gotCalls, "handlers after the first failure must not run")
	if !wantFail && gasLimit != 0 {
		require.Equal(t, gas, meter.GasConsumed())
	}

	got := map[byte]byte{}
	it := kv(ctx).Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		if it.Key()[0] != 0xff {
			got[it.Key()[0]] = it.Value()[0]
		}
	}
	require.NoError(t, it.Close())
	require.Equal(t, []byte("pre-existing"), kv(ctx).Get([]byte{0xff}))
	events := ctx.EventManager().Events()
	require.Equal(t, "pre-existing", events[0].Type)
	if branch && wantFail {
		require.Empty(t, got, "a failed tx left writes behind")
		require.Len(t, events, 1, "a failed tx left events behind")
		return err
	}
	require.Equal(t, want, got)
	require.Len(t, events, 1+len(wantEvents))
	for i, e := range wantEvents {
		require.Equal(t, e, events[1+i])
	}
	return err
}

// TestExecuteTxMsgsFuzzSeeds keeps the oracle non-vacuous: a tx of
// successful messages executes, and each failure kind is reported.
func TestExecuteTxMsgsFuzzSeeds(t *testing.T) {
	ok := fuzzutil.Records(execFuzzRecord, []byte{execSucceed, 1, 1, 0, 10, 1}, []byte{execConsumeGas, 2, 2, 0, 10, 1})
	require.NoError(t, fuzzExecuteTxMsgs(t, 0, ok, true))
	require.NoError(t, fuzzExecuteTxMsgs(t, 50_000, ok, false))
	for _, action := range []byte{execError, execPanic, execConsumeGas, execUnregistered} {
		bad := fuzzutil.Records(execFuzzRecord, []byte{execSucceed, 1, 1, 0, 10, 1}, []byte{action, 2, 2, 0xff, 0xff, 1})
		require.Error(t, fuzzExecuteTxMsgs(t, 50_000, bad, true), "action %d", action)
		require.Error(t, fuzzExecuteTxMsgs(t, 50_000, bad, false), "action %d", action)
	}
	require.NoError(t, fuzzExecuteTxMsgs(t, math.MaxUint32, fuzzutil.Records(execFuzzRecord, []byte{execConsumeGas, 1, 1, 0xff, 0xff}), true))
}
