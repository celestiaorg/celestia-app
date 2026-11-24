package app

import (
	"bytes"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	fibrekeeper "github.com/celestiaorg/celestia-app/v12/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v12/x/fibre/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cast"
)

// FlagFastRecheck is app.toml `[fast-recheck] enable`; fast recheck is on unless it is false.
const FlagFastRecheck = "fast-recheck.enable"

// fastRecheckEnabled reports whether appOpts leaves fast recheck on.
func fastRecheckEnabled(appOpts servertypes.AppOptions) bool {
	v := appOpts.Get(FlagFastRecheck)
	return v == nil || cast.ToBool(v)
}

// fastRecheck answers a recheck by replaying the writes of the tx's last passed
// ante pass when every store value that pass read is unchanged, so the full
// ante would read the same inputs and write the same values. Callers hold App.checkStateMu.
type fastRecheck struct {
	app     *App
	records map[string]*fastRecord
	maxRecs int
	// bytes bounds what records hold, like recheckTxCache's budget.
	bytes, maxBytes int
	commits         uint64
	bank            storetypes.StoreKey
	fcKey           []byte
	// trace is set while CheckTx runs the full ante of an eligible tx.
	trace        *anteTrace
	hits, misses uint64
}

type fastRecord struct {
	reads, writes []kvOp
	// The fee moves from the payer's balance to the fee collector's. Both change
	// between passes, so they are re-read and the transfer is redone.
	payerKey []byte
	fee      math.Int
	// lens are the value lengths of the payer and fee collector balances
	// before and after the recorded pass, which its gas depends on.
	lens          [4]int
	timeoutHeight uint64
	promise       *fibretypes.PaymentPromise
	gasWanted     uint64
	gasUsed       uint64
	priority      int64
	size          int
	used          uint64
}

func newFastRecheck(app *App, maxRecs int) *fastRecheck {
	return &fastRecheck{
		app:      app,
		records:  map[string]*fastRecord{},
		maxRecs:  maxRecs,
		maxBytes: maxRecs * recheckTxBytesPerEntry,
		bank:     app.keys[banktypes.StoreKey],
		fcKey:    balanceKey(authtypes.NewModuleAddress(authtypes.FeeCollectorName)),
	}
}

// balanceKey is the bank Balances key of addr's utia balance.
func balanceKey(addr sdk.AccAddress) []byte {
	k, err := collections.EncodeKeyWithPrefix(banktypes.BalancesPrefix,
		collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey), collections.Join(addr, appconsts.BondDenom))
	if err != nil {
		panic(err) // encoding a pair of byte keys cannot fail
	}
	return k
}

// begin starts tracing the ante pass of sdkTx if a passed pass can be replayed:
// one utia fee paid by a plain account (a vesting account's spendable balance and
// a fee grant depend on block time) and no IBC core message. It returns nil otherwise.
func (f *fastRecheck) begin(sdkTx sdk.Tx) *anteTrace {
	payer := utiaFeePayer(sdkTx)
	if payer == nil || hasIBCCoreMsg(sdkTx) {
		return nil
	}
	var plain bool
	f.app.ReadCheckState(func(ctx sdk.Context) {
		_, plain = f.app.AccountKeeper.GetAccount(ctx, payer).(*authtypes.BaseAccount)
	})
	if !plain {
		return nil
	}
	f.trace = newAnteTrace(f.bank, balanceKey(payer), f.fcKey)
	return f.trace
}

// utiaFeePayer returns the payer of sdkTx's fee if it is one utia coin without a
// granter. The accessors panic on some malformed txs, which the ante then
// rejects as in stock, so a panic only means "not eligible".
func utiaFeePayer(sdkTx sdk.Tx) (payer sdk.AccAddress) {
	defer func() {
		if recover() != nil {
			payer = nil
		}
	}()
	feeTx, ok := sdkTx.(sdk.FeeTx)
	if !ok {
		return nil
	}
	fee := feeTx.GetFee()
	if len(fee) != 1 || fee[0].Denom != appconsts.BondDenom || feeTx.FeeGranter() != nil {
		return nil
	}
	return feeTx.FeePayer()
}

// hasIBCCoreMsg reports whether a top-level message of sdkTx is an IBC core
// message: the IBC relay ante reads the block time and height and writes them.
func hasIBCCoreMsg(sdkTx sdk.Tx) bool {
	for _, msg := range sdkTx.GetMsgs() {
		if strings.HasPrefix(sdk.MsgTypeURL(msg), "/ibc.core.") {
			return true
		}
	}
	return false
}

// end stops the trace and records raw's pass if it passed and can be replayed.
func (f *fastRecheck) end(raw []byte, sdkTx sdk.Tx, tr *anteTrace, res *abci.ResponseCheckTx, err error) {
	f.trace = nil
	if err != nil || res.Code != abci.CodeTypeOK || tr.failed || tr.payer.sets != 1 || tr.fc.sets != 1 || res.GasWanted <= 0 || res.GasUsed < 0 {
		return
	}
	fee := sdkTx.(sdk.FeeTx).GetFee()[0].Amount
	pb, pa := decodeBalance(tr.payer.before), decodeBalance(tr.payer.after)
	fb, fa := decodeBalance(tr.fc.before), decodeBalance(tr.fc.after)
	// The replay redoes exactly this transfer; anything else is not replayable.
	if pb.IsNil() || pa.IsNil() || fb.IsNil() || fa.IsNil() || !pb.Sub(pa).Equal(fee) || !fa.Sub(fb).Equal(fee) {
		return
	}
	rec := &fastRecord{
		reads:     tr.reads,
		writes:    tr.writes,
		payerKey:  tr.payer.key,
		fee:       fee,
		lens:      [4]int{len(tr.payer.before), len(tr.payer.after), len(tr.fc.before), len(tr.fc.after)},
		gasWanted: uint64(res.GasWanted),
		gasUsed:   uint64(res.GasUsed),
		priority:  res.Priority,
		used:      f.commits,
		size:      len(raw),
	}
	if t, ok := sdkTx.(interface{ GetTimeoutHeight() uint64 }); ok {
		rec.timeoutHeight = t.GetTimeoutHeight()
	}
	if msg := fibretypes.PayForFibreMsg(sdkTx); msg != nil {
		promise := msg.PaymentPromise // a copy, so the record does not pin the decoded tx
		rec.promise = &promise
	}
	for _, ops := range [][]kvOp{rec.reads, rec.writes} {
		for _, op := range ops {
			rec.size += len(op.key) + len(op.value)
		}
	}
	if old := f.records[string(raw)]; old != nil {
		f.bytes -= old.size
		delete(f.records, string(raw))
	}
	if len(f.records) < f.maxRecs && f.bytes+rec.size <= f.maxBytes {
		f.bytes += rec.size
		f.records[string(raw)] = rec
	}
}

// blobTxSuffix ends every canonical BlobTx (type_id "BLOB"), so plain txs skip the blob parse.
var blobTxSuffix = []byte("\x1a\x04" + blobtx.ProtoBlobTxTypeID)

// recheck answers the recheck of tx from its record if nothing the record read changed.
// Blob txs are recorded under their inner sdk tx.
func (f *fastRecheck) recheck(tx []byte) (*abci.ResponseCheckTx, bool) {
	key := tx
	if bytes.HasSuffix(tx, blobTxSuffix) {
		if btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(tx); isBlob && err == nil {
			key = btx.Tx
		}
	}
	rec := f.records[string(key)]
	if rec == nil {
		f.misses++
		return nil, false
	}
	rec.used = f.commits
	// Under BaseApp's lock, so Simulate never sees a half-applied replay.
	unlock := f.app.LockCheckState()
	defer unlock()
	ctx, ok := f.app.CheckState()
	var res *abci.ResponseCheckTx
	if ok {
		res, ok = f.replay(ctx, rec)
	}
	if !ok {
		f.misses++
		return nil, false
	}
	f.app.recheckTxs.get(key) // keep the decode for a later full recheck
	f.hits++
	return res, true
}

// replay applies rec's writes to ctx's state if the full ante would read the
// same values, and returns the response that pass would give.
func (f *fastRecheck) replay(ctx sdk.Context, rec *fastRecord) (*abci.ResponseCheckTx, bool) {
	if rec.timeoutHeight > 0 && uint64(ctx.BlockHeight()) > rec.timeoutHeight {
		return nil, false
	}
	ms := ctx.MultiStore()
	for _, r := range rec.reads {
		// nil is absent, not empty
		if v := ms.GetKVStore(r.store).Get(r.key); (v == nil) != (r.value == nil) || !bytes.Equal(v, r.value) {
			return nil, false
		}
	}
	// The ante charges gas per byte of the two balances it reads and writes.
	bank := ms.GetKVStore(f.bank)
	pbRaw, fbRaw := bank.Get(rec.payerKey), bank.Get(f.fcKey)
	pb, fb := decodeBalance(pbRaw), decodeBalance(fbRaw)
	// A drained payer or empty fee collector also writes the denom index: full ante.
	if pb.IsNil() || fb.IsNil() || !pb.GT(rec.fee) {
		return nil, false
	}
	pbNew, err1 := sdk.IntValue.Encode(pb.Sub(rec.fee))
	fbNew, err2 := sdk.IntValue.Encode(fb.Add(rec.fee))
	if err1 != nil || err2 != nil {
		return nil, false
	}
	gc := ctx.KVGasConfig()
	read, write := int64(gc.ReadCostPerByte), int64(gc.WriteCostPerByte)
	gasUsed := int64(rec.gasUsed) +
		read*int64(len(pbRaw)-rec.lens[0]) + write*int64(len(pbNew)-rec.lens[1]) +
		read*int64(len(fbRaw)-rec.lens[2]) + write*int64(len(fbNew)-rec.lens[3])
	if gasUsed > int64(rec.gasWanted) {
		return nil, false
	}
	if rec.promise != nil {
		ctx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		if _, err := f.app.FibreKeeper.ValidatePaymentPromiseStateful(ctx, rec.promise); err != nil {
			return nil, false
		}
	}
	for _, w := range rec.writes {
		if w.value == nil {
			ms.GetKVStore(w.store).Delete(w.key)
		} else {
			ms.GetKVStore(w.store).Set(w.key, w.value)
		}
	}
	bank.Set(rec.payerKey, pbNew)
	bank.Set(f.fcKey, fbNew)
	// Signer fields stay unset: CAT ignores them on recheck.
	return &abci.ResponseCheckTx{
		Code:      abci.CodeTypeOK,
		GasWanted: int64(rec.gasWanted),
		GasUsed:   gasUsed,
		Priority:  rec.priority,
	}, true
}

// commit runs at Commit: records unused since the previous Commit belong to
// txs that left the mempool.
func (f *fastRecheck) commit() {
	if f.hits+f.misses > 0 {
		f.app.Logger().Info("fast recheck", "hits", f.hits, "misses", f.misses, "records", len(f.records))
	}
	for k, r := range f.records {
		if r.used < f.commits {
			delete(f.records, k)
			f.bytes -= r.size
		}
	}
	f.commits++
	f.hits, f.misses = 0, 0
}

// decodeBalance decodes a raw Balances value; nil if missing or malformed.
func decodeBalance(raw []byte) math.Int {
	if raw == nil {
		return math.Int{}
	}
	v, err := sdk.IntValue.Decode(raw)
	if err != nil {
		return math.Int{}
	}
	return v
}

// untracedPromises runs the payment promise check outside the trace: it reads
// the escrow, which other txs' settlement changes, so the replay re-runs it.
type untracedPromises struct {
	*fibrekeeper.Keeper
	f *fastRecheck
}

func (k untracedPromises) ValidatePaymentPromiseStateful(ctx sdk.Context, p *fibretypes.PaymentPromise) (time.Time, error) {
	if mode := ctx.ExecMode(); (mode == sdk.ExecModeCheck || mode == sdk.ExecModeReCheck) && k.f.trace != nil {
		tr := k.f.trace
		tr.paused = true
		defer func() { tr.paused = false }()
	}
	return k.Keeper.ValidatePaymentPromiseStateful(ctx, p)
}
