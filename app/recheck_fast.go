package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"sync"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	signaltypes "github.com/celestiaorg/celestia-app/v11/x/signal/types"
	valaddrtypes "github.com/celestiaorg/celestia-app/v11/x/valaddr/types"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

const (
	// FlagFastRecheck turns on fast recheck: app.toml `[fast-recheck] enable = true`.
	FlagFastRecheck = "fast-recheck.enable"
	// FlagFastRecheckShadowVerify keeps the stock recheck authoritative and only
	// compares the fast path's answer with it (overrides enable): `shadow-verify = true`.
	FlagFastRecheckShadowVerify = "fast-recheck.shadow-verify"
)

// fastRecheckGasPerByte bounds the gas the ante charges per byte of length
// difference in the payer's or fee collector's balance value.
const fastRecheckGasPerByte = 200

// fastRecheckGasSlack is extra headroom required whenever a balance length
// differs from the one the recorded ante pass saw.
const fastRecheckGasSlack = 1_000

// fastRecheck answers an eligible tx's recheck by replaying its last passed ante's writes
// when every ante input is re-read here or unchanged: no block wrote a traced read, and a
// block write outside fastRecheckBumps' allowlist drops every record. Callers hold App.checkStateMu;
// a parallel recheck's workers only call recheck, eligible, readSigner and build.
type fastRecheck struct {
	app *App

	commits uint64
	records map[string]*fastRecord
	maxRecs int
	// bytes are the tx bytes records are keyed by, bounded like recheckTxCache's.
	bytes, maxBytes int

	fcAddr           sdk.AccAddress
	authKey, bankKey storetypes.StoreKey
	fcBalKey         []byte

	// trace is set while CheckTx runs the full ante of an eligible tx (wrapAnte);
	// a worker branch passes it in the tx's context instead.
	trace   *anteTrace
	depsets map[string]*depSet
	// watched maps each dependency key to the last commit counter at which a
	// block wrote it.
	watched map[string]uint64

	// Block-level promise inputs and escrow accounts, memoized until Commit
	// (escrows nil): CheckTx never writes the fibre store. Guarded by mu.
	mu      sync.Mutex
	envOK   bool
	env     fibrekeeper.PromiseCheckEnv
	escrows map[string]escrowMemo

	shadow *fastShadow // nil unless shadow-verify is on

	// forceBumpEvery drops every record every n commits (tests only).
	forceBumpEvery uint64
	// lastDelta is the balance-length difference behind the last hit (tests).
	lastDelta uint64

	stats fastRecheckStats
}

type fastRecheckStats struct {
	hits, misses, bumps uint64
	missReasons         map[string]uint64
	recordRejects       map[string]uint64
	lastBump            string
}

type fastRecord struct {
	signer    sdk.AccAddress
	accNum    uint64
	seq       uint64
	fee       math.Int
	gasWanted uint64
	gasUsed   uint64
	priority  int64
	// lens are the stored lengths of the payer balance before/after and the
	// fee collector balance before/after the recorded fee transfer.
	lens          [4]int
	timeoutHeight uint64
	promise       *fibretypes.PaymentPromise
	promiseCache  *fibrekeeper.PromiseCheckCache
	used          uint64
	// deps are the recorded pass's reads that are not re-read here;
	// recordedAt is the commit counter when it ran.
	deps       *depSet
	recordedAt uint64
	// Raw account value before/after the recorded pass, and the raw keys.
	accKey, accBefore, accAfter, balKey []byte
}

// fastPre is what an eligible tx's ante pass is about to read.
type fastPre struct {
	signer sdk.AccAddress
	accNum uint64
	seq    uint64
	fee    math.Int
	gas    uint64
	tmo    uint64
	pff    *fibretypes.PaymentPromise
	lenPB  int
	lenFB  int
	accKey []byte
	balKey []byte
	accRaw []byte
	trace  *anteTrace
}

func newFastRecheck(app *App, maxRecs int, shadow bool) *fastRecheck {
	f := &fastRecheck{
		app:      app,
		records:  make(map[string]*fastRecord),
		maxRecs:  maxRecs,
		maxBytes: maxRecs * recheckTxBytesPerEntry,
		fcAddr:   feeCollectorAddr,
		authKey:  app.keys[authtypes.StoreKey],
		bankKey:  app.keys[banktypes.StoreKey],
		fcBalKey: balanceKey(feeCollectorAddr, appconsts.BondDenom),
		depsets:  map[string]*depSet{},
		watched:  map[string]uint64{},
		stats:    fastRecheckStats{missReasons: map[string]uint64{}, recordRejects: map[string]uint64{}},
	}
	if shadow {
		f.shadow = &fastShadow{}
	}
	return f
}

// eligible returns the signer data of a tx whose ante footprint the fast path
// models exactly, or nil. Its accessors panic on some malformed txs, which the
// ante then rejects as in stock, so a panic only means "not eligible".
func (f *fastRecheck) eligible(sdkTx sdk.Tx) (pre *fastPre) {
	defer func() {
		if recover() != nil {
			pre = nil
		}
	}()
	msgs := sdkTx.GetMsgs()
	if len(msgs) == 0 {
		return nil
	}
	var pff *fibretypes.PaymentPromise
	for _, m := range msgs {
		switch msg := m.(type) {
		case *banktypes.MsgSend, *blobtypes.MsgPayForBlobs:
		case *fibretypes.MsgPayForFibre:
			if len(msgs) != 1 {
				return nil
			}
			promise := msg.PaymentPromise // a copy, so the record does not pin the decoded tx
			pff = &promise
		default:
			return nil
		}
	}
	sigTx, ok := sdkTx.(authsigning.Tx)
	if !ok {
		return nil
	}
	signers, err := sigTx.GetSigners()
	if err != nil || len(signers) != 1 {
		return nil
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 {
		return nil
	}
	if _, single := sigs[0].Data.(*signing.SingleSignatureData); !single {
		return nil
	}
	if sigTx.FeeGranter() != nil || !bytes.Equal(sigTx.FeePayer(), signers[0]) {
		return nil
	}
	fee := sigTx.GetFee()
	if len(fee) != 1 || fee[0].Denom != appconsts.BondDenom || !fee[0].Amount.IsPositive() {
		return nil
	}
	return &fastPre{
		signer: signers[0],
		seq:    sigs[0].Sequence,
		fee:    fee[0].Amount,
		gas:    sigTx.GetGas(),
		tmo:    sigTx.GetTimeoutHeight(),
		pff:    pff,
	}
}

// peek reads the per-signer state the coming ante pass of sdkTx will see and
// starts tracing it. It returns nil if the tx cannot be recorded.
func (f *fastRecheck) peek(sdkTx sdk.Tx) *fastPre {
	if f.shadow != nil && f.shadow.predictedHit {
		return nil // production skips the ante on a hit, so the pass is not recorded
	}
	pre := f.eligible(sdkTx)
	if pre == nil {
		return nil
	}
	// BaseApp's lock: FinalizeBlock writes the same IAVL tree from another ABCI connection.
	var ok bool
	if !f.app.ReadCheckState(func(ctx sdk.Context) { ok = f.readSigner(ctx.MultiStore(), pre) }) || !ok {
		return nil
	}
	f.trace = pre.trace
	return pre
}

// readSigner fills pre with the signer's account and the payer and fee
// collector balances, and a trace for its ante pass; false if the tx cannot be recorded.
func (f *fastRecheck) readSigner(ms storetypes.MultiStore, pre *fastPre) bool {
	// The account must be a plain BaseAccount (a vesting account's spendable
	// balance depends on block time) at the tx's sequence.
	pre.accKey, pre.balKey = accountKey(pre.signer), balanceKey(pre.signer, appconsts.BondDenom)
	pre.accRaw = ms.GetKVStore(f.authKey).Get(pre.accKey)
	var ok bool
	if pre.accNum, ok = baseAccountNumber(pre.accRaw, pre.seq); !ok {
		return false
	}
	bankKV := ms.GetKVStore(f.bankKey)
	pb := bankKV.Get(pre.balKey)
	if _, ok := decodeBalance(pb); !ok {
		return false
	}
	pre.lenPB, pre.lenFB = len(pb), len(bankKV.Get(f.fcBalKey))
	pre.trace = newAnteTrace(pre.signer, f.fcAddr, accountNumberKey(pre.accNum), f.app.keys)
	return true
}

// record ends the trace peek started and, if the full ante passed, stores
// what the fast path needs to replay it for raw.
func (f *fastRecheck) record(raw []byte, pre *fastPre, res *abci.ResponseCheckTx, err error) {
	f.trace = nil
	if err != nil {
		return
	}
	var rec *fastRecord
	f.app.ReadCheckState(func(ctx sdk.Context) { rec = f.build(ctx.MultiStore(), pre, res) })
	f.keep(raw, pre, res, rec)
}

// passed reports whether res is a recordable pass of pre's tx.
func (pre *fastPre) passed(res *abci.ResponseCheckTx) bool {
	return res.Code == abci.CodeTypeOK && res.GasUsed >= 0 && res.GasWanted > 0 && uint64(res.GasWanted) == pre.gas
}

// build returns the record of pre's passed ante pass, reading its writes from ms, or nil.
func (f *fastRecheck) build(ms storetypes.MultiStore, pre *fastPre, res *abci.ResponseCheckTx) *fastRecord {
	if !pre.passed(res) || pre.trace.reject != "" {
		return nil
	}
	rec := &fastRecord{
		signer:        pre.signer,
		accNum:        pre.accNum,
		seq:           pre.seq,
		fee:           pre.fee,
		gasWanted:     uint64(res.GasWanted),
		gasUsed:       uint64(res.GasUsed),
		priority:      res.Priority,
		timeoutHeight: pre.tmo,
		promise:       pre.pff,
		used:          f.commits,
		recordedAt:    f.commits,
		accKey:        pre.accKey,
		accBefore:     pre.accRaw,
		balKey:        pre.balKey,
	}
	rec.accAfter = ms.GetKVStore(f.authKey).Get(rec.accKey)
	bankKV := ms.GetKVStore(f.bankKey)
	pb, fb := bankKV.Get(rec.balKey), bankKV.Get(f.fcBalKey)
	if rec.accAfter == nil || pb == nil {
		// A drained balance is deleted, not written: not what the replay does.
		return nil
	}
	rec.lens = [4]int{pre.lenPB, len(pb), pre.lenFB, len(fb)}
	if pre.pff != nil {
		pc, err := fibrekeeper.NewPromiseCheckCache(pre.pff)
		if err != nil {
			return nil
		}
		rec.promiseCache = &pc
	}
	return rec
}

// keep stores rec, built from pre's pass with response res, for raw within the record budget.
func (f *fastRecheck) keep(raw []byte, pre *fastPre, res *abci.ResponseCheckTx, rec *fastRecord) {
	if rec == nil {
		if pre.passed(res) && pre.trace.reject != "" {
			f.stats.recordRejects[pre.trace.reject]++
		}
		return
	}
	old, exists := f.records[string(raw)]
	if !exists && (len(f.records) >= f.maxRecs || f.bytes+len(raw) > f.maxBytes) {
		return
	}
	rec.deps = f.internDeps(pre.trace.deps)
	if old == nil {
		f.bytes += len(raw)
	}
	f.records[string(raw)] = rec
}

func (f *fastRecheck) miss(reason string) (*abci.ResponseCheckTx, bool) {
	f.stats.misses++
	f.stats.missReasons[reason]++
	return nil, false
}

// blobTxSuffix ends every canonical BlobTx (type_id "BLOB"), so plain txs skip the blob parse.
var blobTxSuffix = []byte("\x1a\x04" + blobtx.ProtoBlobTxTypeID)

// lookup returns tx's record key (a blob tx's inner sdk tx) and record, if any.
func (f *fastRecheck) lookup(tx []byte) ([]byte, *fastRecord) {
	key := tx
	if bytes.HasSuffix(tx, blobTxSuffix) {
		btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(tx)
		if isBlob && err == nil {
			key = btx.Tx
		}
	}
	return key, f.records[string(key)]
}

// recheck answers the recheck of tx from its record if the fast path applies,
// on p's worker branch if p (which looked the record up).
func (f *fastRecheck) recheck(tx []byte, p *parallelTx) (*abci.ResponseCheckTx, bool) {
	if p != nil {
		if p.fastRec == nil {
			p.fastMiss = "norecord"
			return nil, false
		}
		res, reason := f.try(p.w.Context(), nil, p.fastRec, p)
		p.fastHit, p.fastMiss = res != nil, reason
		return res, p.fastHit
	}
	key, rec := f.lookup(tx)
	if rec == nil {
		return f.miss("norecord")
	}
	// Under BaseApp's lock, so Simulate never sees a half-applied replay.
	unlock := f.app.LockCheckState()
	defer unlock()
	ctx, hasState := f.app.CheckState()
	if !hasState {
		return f.miss("nostate")
	}
	return f.trySeq(ctx, key, rec)
}

// trySeq is try off the parallel workers, counting its outcome.
func (f *fastRecheck) trySeq(ctx sdk.Context, key []byte, rec *fastRecord) (*abci.ResponseCheckTx, bool) {
	res, reason := f.try(ctx, key, rec, nil)
	if res == nil {
		return f.miss(reason)
	}
	f.stats.hits++
	if rec.promise != nil {
		// Keep the PayForFibre decode for a later full recheck.
		f.app.recheckTxs.get(key)
	}
	return res, true
}

// try replays rec, raw's record, into ctx's state if nothing it depends on changed,
// or returns why not. p is the worker running it, if any.
func (f *fastRecheck) try(ctx sdk.Context, raw []byte, rec *fastRecord, p *parallelTx) (*abci.ResponseCheckTx, string) {
	rec.used = f.commits
	if f.stale(rec) {
		k := rec.deps.lastKey
		if i := strings.IndexByte(k, 0); i >= 0 && i+1 < len(k) {
			k = fmt.Sprintf("%s/%x", k[:i], k[i+1])
		}
		return nil, "deps:" + k
	}
	if rec.timeoutHeight > 0 && uint64(ctx.BlockHeight()) > rec.timeoutHeight {
		return nil, "timeout"
	}
	authKV := ctx.MultiStore().GetKVStore(f.authKey)
	bankKV := ctx.MultiStore().GetKVStore(f.bankKey)
	// Equal raw account bytes imply the same type, number, pubkey and sequence.
	if !bytes.Equal(authKV.Get(rec.accKey), rec.accBefore) {
		return nil, "account"
	}
	pbRaw := bankKV.Get(rec.balKey)
	pb, ok := decodeBalance(pbRaw)
	if !ok || pb.LTE(rec.fee) {
		return nil, "balance"
	}
	// An empty fee collector also needs its denom index entry: full ante.
	fbRaw := bankKV.Get(f.fcBalKey)
	fb, ok := decodeBalance(fbRaw)
	if !ok {
		return nil, "fcbalance"
	}
	newFB, err := fb.SafeAdd(rec.fee)
	if err != nil {
		return nil, "fcbalance"
	}
	newPB := pb.Sub(rec.fee) // pb > fee
	pbEnc, err := sdk.IntValue.Encode(newPB)
	if err != nil {
		return nil, "encode"
	}
	fbEnc, err := sdk.IntValue.Encode(newFB)
	if err != nil {
		return nil, "encode"
	}
	// Gas is charged per stored byte, so a length change shifts GasUsed.
	lens := [4]int{len(pbRaw), len(pbEnc), len(fbRaw), len(fbEnc)}
	var d uint64
	if lens != rec.lens {
		for i := range lens {
			if lens[i] > rec.lens[i] {
				d += uint64(lens[i] - rec.lens[i])
			} else {
				d += uint64(rec.lens[i] - lens[i])
			}
		}
		if rec.gasUsed+d*fastRecheckGasPerByte+fastRecheckGasSlack > rec.gasWanted {
			return nil, "gas"
		}
	}
	if rec.promise != nil {
		ctx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		env, ok := f.promiseEnv(ctx)
		if !ok {
			return nil, "promiseenv"
		}
		escrow := func(signer string) (fibretypes.EscrowAccount, bool) { return f.escrow(ctx, signer) }
		if _, err := f.app.FibreKeeper.ValidatePaymentPromiseStatefulCached(ctx, rec.promise, env, rec.promiseCache, escrow); err != nil {
			return nil, "promise"
		}
	}

	// The ante's writes: the fee moves from the payer to the fee collector
	// (both stay positive, so their denom index entries are unchanged) and the
	// account's sequence advances.
	bankKV.Set(f.fcBalKey, fbEnc)
	bankKV.Set(rec.balKey, pbEnc)
	authKV.Set(rec.accKey, rec.accAfter)

	if p == nil {
		f.lastDelta = d
	}
	// Signer fields stay unset, as on the PayForFibre path: CAT ignores them on recheck.
	return &abci.ResponseCheckTx{
		Code:      abci.CodeTypeOK,
		GasWanted: int64(rec.gasWanted),
		GasUsed:   int64(rec.gasUsed),
		Priority:  rec.priority,
	}, ""
}

// sweep runs at Commit, before BaseApp's: records unused since the previous
// Commit belong to txs that left the mempool.
func (f *fastRecheck) sweep() {
	f.logStats()
	for k, r := range f.records {
		if r.used < f.commits {
			delete(f.records, k)
			f.bytes -= len(k)
		}
	}
	if f.commits%64 == 0 {
		f.gcDeps()
	}
	f.commits++
	f.escrows = nil
	f.stats.hits, f.stats.misses, f.stats.bumps = 0, 0, 0
	clear(f.stats.missReasons)
	clear(f.stats.recordRejects)
}

func (f *fastRecheck) logStats() {
	f.shadow.report(f.app)
	if f.stats.hits+f.stats.misses == 0 && f.stats.bumps == 0 {
		return
	}
	f.app.Logger().Info("fast recheck", "hits", f.stats.hits, "misses", f.stats.misses,
		"reasons", countsString(f.stats.missReasons), "record_rejects", countsString(f.stats.recordRejects),
		"records", len(f.records), "bumps", f.stats.bumps, "last_bump", f.stats.lastBump)
}

func countsString(m map[string]uint64) string {
	s := make([]string, 0, len(m))
	for k, n := range m {
		s = append(s, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(s)
	return strings.Join(s, ",")
}

// reset drops every record and dependency: the state they were taken against
// was replaced (InitChain, snapshot restore, LoadHeight).
func (f *fastRecheck) reset() {
	clear(f.records)
	f.bytes = 0
	clear(f.depsets)
	clear(f.watched)
	f.escrows = nil
}

// --- commit changeset classification ---

// fastRecheckAllowedStores are stores that no untraced read of an eligible
// tx's CheckTx touches, so a block write there only invalidates the records
// that traced a read of the written key.
var fastRecheckAllowedStores = map[string]bool{
	stakingtypes.StoreKey: true, minttypes.StoreKey: true, distrtypes.StoreKey: true,
	slashingtypes.StoreKey: true, govtypes.StoreKey: true, evidencetypes.StoreKey: true,
	feegrant.StoreKey: true, authzkeeper.StoreKey: true, capabilitytypes.StoreKey: true,
	ibctransfertypes.StoreKey: true, ibcexported.StoreKey: true, packetforwardtypes.StoreKey: true,
	icahosttypes.StoreKey: true, signaltypes.StoreKey: true, hyperlanetypes.ModuleName: true,
	warptypes.ModuleName: true, zkismtypes.StoreKey: true, valaddrtypes.StoreKey: true,
}

// fastRecheckBumps reports why a block write drops every record, or "".
func fastRecheckBumps(storeKey string, key []byte) string {
	if fastRecheckAllowedStores[storeKey] {
		return ""
	}
	if len(key) == 0 {
		return storeKey + "/empty"
	}
	switch storeKey {
	case authtypes.StoreKey:
		// Accounts and their indexes are re-read or traced; params are not.
		if key[0] == authtypes.AddressStoreKeyPrefix[0] || key[0] == authtypes.GlobalAccountNumberKey[0] ||
			bytes.HasPrefix(key, authtypes.AccountNumberStoreKeyPrefix) {
			return ""
		}
	case banktypes.StoreKey:
		switch key[0] {
		case banktypes.SupplyKey[0], banktypes.BalancesPrefix[0], banktypes.DenomAddressPrefix[0]:
			return ""
		}
	case fibretypes.StoreKey:
		// The promise check is re-run; only params are read by the rest of the ante.
		if isFibreParamsKey(key) {
			return fmt.Sprintf("%s/%x", storeKey, key[:1])
		}
		return ""
	}
	// Everything else, including the blob, minfee, consensus, circuit, params and
	// upgrade stores and any store added later.
	return fmt.Sprintf("%s/%x", storeKey, key[:1])
}

// ListenFinalizeBlock implements storetypes.ABCIListener.
func (f *fastRecheck) ListenFinalizeBlock(context.Context, abci.RequestFinalizeBlock, abci.ResponseFinalizeBlock) error {
	return nil
}

// ListenCommit implements storetypes.ABCIListener: it stamps the watched keys the
// block wrote, or drops every record on a write outside the allowlist. It runs
// inside BaseApp.Commit, under App.checkStateMu.
func (f *fastRecheck) ListenCommit(_ context.Context, _ abci.ResponseCommit, changeSet []*storetypes.StoreKVPair) error {
	if f.forceBumpEvery > 0 && f.commits%f.forceBumpEvery == 0 {
		f.bump("forced")
		return nil
	}
	for _, kv := range changeSet {
		if reason := fastRecheckBumps(kv.StoreKey, kv.Key); reason != "" {
			f.bump(reason)
			return nil
		}
	}
	// Records taken later have recordedAt == commits, so stamps only drop older ones.
	var buf []byte
	for _, kv := range changeSet {
		buf = append(append(append(buf[:0], kv.StoreKey...), 0), kv.Key...)
		if _, ok := f.watched[string(buf)]; ok {
			f.watched[string(buf)] = f.commits
		}
	}
	return nil
}

// bump drops every record: the block wrote state the fast path does not track per key.
func (f *fastRecheck) bump(reason string) {
	f.reset()
	f.stats.bumps++
	f.stats.lastBump = reason
}

// registerListener has every committed write recorded, and appends f to the
// ABCI listeners, so ListenCommit sees the block's changeset.
func (f *fastRecheck) registerListener() {
	keys := make([]storetypes.StoreKey, 0, len(f.app.keys))
	for _, k := range f.app.keys {
		keys = append(keys, k)
	}
	f.app.CommitMultiStore().AddListeners(keys)
	sm := f.app.StreamingManager()
	sm.ABCIListeners = append(sm.ABCIListeners, f)
	f.app.SetStreamingManager(sm)
}

// accountKey is the auth Accounts collection key of addr.
func accountKey(addr sdk.AccAddress) []byte {
	return append(append(make([]byte, 0, len(authtypes.AddressStoreKeyPrefix)+len(addr)), authtypes.AddressStoreKeyPrefix...), addr...)
}

// accountNumberKey is the auth account-number index key of num.
func accountNumberKey(num uint64) []byte {
	return binary.BigEndian.AppendUint64(append([]byte(nil), authtypes.AccountNumberStoreKeyPrefix...), num)
}

// balanceKey is the bank Balances collection key of (addr, denom): prefix,
// length-prefixed address, denom.
func balanceKey(addr sdk.AccAddress, denom string) []byte {
	k := append(make([]byte, 0, len(banktypes.BalancesPrefix)+1+len(addr)+len(denom)), banktypes.BalancesPrefix...)
	k = append(k, byte(len(addr)))
	k = append(k, addr...)
	return append(k, denom...)
}

type escrowMemo struct {
	acc   fibretypes.EscrowAccount
	found bool
}

// promiseEnv returns the fibre params and freshness floor of ctx's state,
// loaded once per Commit.
func (f *fastRecheck) promiseEnv(ctx sdk.Context) (fibrekeeper.PromiseCheckEnv, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.escrows == nil {
		env, err := f.app.FibreKeeper.PromiseCheckEnv(ctx)
		f.env, f.envOK = env, err == nil
		f.escrows = make(map[string]escrowMemo)
	}
	return f.env, f.envOK
}

// escrow memoizes escrow lookups for the current state (CheckTx never writes
// the fibre store). promiseEnv resets the memo.
func (f *fastRecheck) escrow(ctx sdk.Context, signer string) (fibretypes.EscrowAccount, bool) {
	f.mu.Lock()
	m, ok := f.escrows[signer]
	f.mu.Unlock()
	if ok {
		return m.acc, m.found
	}
	acc, found := f.app.FibreKeeper.GetEscrowAccount(ctx, signer)
	f.mu.Lock()
	f.escrows[signer] = escrowMemo{acc, found}
	f.mu.Unlock()
	return acc, found
}

var baseAccountTypeURL = sdk.MsgTypeURL(&authtypes.BaseAccount{})

// baseAccountNumber decodes a raw Accounts value (an Any) and returns the
// account number if it is a BaseAccount at sequence seq.
func baseAccountNumber(raw []byte, seq uint64) (uint64, bool) {
	if raw == nil {
		return 0, false
	}
	var a codectypes.Any
	if err := a.Unmarshal(raw); err != nil || a.TypeUrl != baseAccountTypeURL {
		return 0, false
	}
	var ba authtypes.BaseAccount
	if err := ba.Unmarshal(a.Value); err != nil || ba.Sequence != seq {
		return 0, false
	}
	return ba.AccountNumber, true
}

// decodeBalance decodes a raw Balances value; false if missing or not a plain
// decimal (whose stored length the gas bound relies on).
func decodeBalance(raw []byte) (math.Int, bool) {
	if raw == nil {
		return math.Int{}, false
	}
	v, err := sdk.IntValue.Decode(raw)
	return v, err == nil
}

// isFibreParamsKey reports whether key in the fibre store holds its params.
func isFibreParamsKey(key []byte) bool {
	return bytes.Equal(key, []byte(fibretypes.ParamsKey)) || (len(key) > 0 && key[0] == fibretypes.ParamsKeyPrefix[0])
}
