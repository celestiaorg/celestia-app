package app

import (
	"bytes"
	"fmt"
	"strings"

	storetypes "cosmossdk.io/store/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// fastShadow is shadow mode (FlagFastRecheckShadowVerify): the stock recheck
// decides, and the fast path's answer, computed on a dropped branch, is only compared.
type fastShadow struct {
	// predictedHit is set while the stock pass of a predicted hit runs.
	predictedHit bool

	hits, mismatches, missStockOK, missStockReject uint64
	totalHits, totalMismatches                     uint64
}

// prediction is the fast path's answer to one recheck; rec is nil on a miss.
type prediction struct {
	rec    *fastRecord
	res    *abci.ResponseCheckTx
	branch storetypes.CacheMultiStore
	d      uint64
	panic  string
}

// shadowRecheck answers req with the stock recheck, unchanged, and compares
// the fast path's prediction with it.
func (f *fastRecheck) shadowRecheck(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	p := f.predict(req.Tx)
	f.shadow.predictedHit = p.rec != nil
	res, err := f.app.checkTx(req)
	f.shadow.predictedHit = false
	f.compare(req.Tx, p, res, err)
	return res, err
}

// predict runs the fast path on a branch of checkState that is never written.
func (f *fastRecheck) predict(tx []byte) (p prediction) {
	defer func() {
		if r := recover(); r != nil {
			p = prediction{panic: fmt.Sprint(r)}
		}
	}()
	key, rec := f.lookup(tx)
	if rec == nil {
		f.miss("norecord")
		return p
	}
	f.app.ReadCheckState(func(ctx sdk.Context) {
		branch := ctx.MultiStore().CacheMultiStore()
		if res, ok := f.try(ctx.WithMultiStore(branch), key, rec); ok {
			p = prediction{rec: rec, res: res, branch: branch, d: f.lastDelta}
		}
	})
	return p
}

// compare checks a prediction against the stock pass that just ran. A predicted
// hit must match stock's verdict, gas and priority, and its writes stock's.
func (f *fastRecheck) compare(tx []byte, p prediction, res *abci.ResponseCheckTx, err error) {
	s := f.shadow
	var diffs []string
	defer func() {
		if r := recover(); r != nil {
			diffs = append(diffs, fmt.Sprintf("compare panicked: %v", r))
		}
		if len(diffs) > 0 {
			s.mismatches++
			f.app.Logger().Error("fast recheck shadow mismatch", "tx", lazyTxHash(tx), "details", strings.Join(diffs, "; "))
		}
	}()
	stockOK := err == nil && res != nil && res.Code == abci.CodeTypeOK
	switch {
	case p.panic != "":
		diffs = append(diffs, "prediction panicked: "+p.panic)
		return
	case p.rec == nil && stockOK:
		s.missStockOK++
		return
	case p.rec == nil:
		s.missStockReject++
		return
	}
	s.hits++
	if !stockOK {
		diffs = append(diffs, fmt.Sprintf("stock rejected a fast hit: err=%v res=%v", err, res))
		return
	}
	rec := p.rec
	if res.GasWanted != p.res.GasWanted || res.Priority != p.res.Priority {
		diffs = append(diffs, fmt.Sprintf("gas_wanted/priority stock=%d/%d fast=%d/%d", res.GasWanted, res.Priority, p.res.GasWanted, p.res.Priority))
	}
	if diff := max(res.GasUsed-p.res.GasUsed, p.res.GasUsed-res.GasUsed); (p.d == 0 && diff != 0) || uint64(diff) > p.d*fastRecheckGasPerByte {
		diffs = append(diffs, fmt.Sprintf("gas_used stock=%d fast=%d len_delta=%d", res.GasUsed, p.res.GasUsed, p.d))
	}
	f.app.ReadCheckState(func(ctx sdk.Context) {
		for _, w := range []struct {
			key   storetypes.StoreKey
			field []byte
		}{{f.authKey, rec.accKey}, {f.bankKey, rec.balKey}, {f.bankKey, f.fcBalKey}} {
			stock, fast := ctx.MultiStore().GetKVStore(w.key).Get(w.field), p.branch.GetKVStore(w.key).Get(w.field)
			if !bytes.Equal(stock, fast) {
				diffs = append(diffs, fmt.Sprintf("%s/%X stock=%X fast=%X", w.key.Name(), w.field, stock, fast))
			}
		}
	})
}

// report logs the shadow counters once per Commit.
func (s *fastShadow) report(app *App) {
	if s == nil || s.hits+s.missStockOK+s.missStockReject+s.mismatches == 0 {
		return
	}
	s.totalHits += s.hits
	s.totalMismatches += s.mismatches
	app.Logger().Info("fast recheck shadow", "hits", s.hits, "mismatches", s.mismatches,
		"miss_stock_ok", s.missStockOK, "miss_stock_reject", s.missStockReject,
		"total_hits", s.totalHits, "total_mismatches", s.totalMismatches)
	s.hits, s.mismatches, s.missStockOK, s.missStockReject = 0, 0, 0, 0
}
