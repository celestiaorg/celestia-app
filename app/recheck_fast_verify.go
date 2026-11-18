package app

import (
	"bytes"
	"fmt"
	"strings"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// fastVerify counts shadow-verified fast hits (FlagFastRecheckShadowVerify).
type fastVerify struct {
	verified, mismatches           uint64
	totalVerified, totalMismatches uint64
}

// verifyReport logs the shadow-verify counters once per Commit.
func (f *fastRecheck) verifyReport() {
	v := f.verify
	if v == nil || v.verified == 0 {
		return
	}
	v.totalVerified += v.verified
	v.totalMismatches += v.mismatches
	f.app.Logger().Info("fast recheck verify", "verified", v.verified, "mismatches", v.mismatches,
		"total_verified", v.totalVerified, "total_mismatches", v.totalMismatches)
	v.verified, v.mismatches = 0, 0
}

// shadowVerify runs the full recheck ante for raw on a discarded branch of
// ctx's state, before the replay's writes, and logs every difference from
// what the replay is about to do. It never changes the replay.
func (f *fastRecheck) shadowVerify(ctx sdk.Context, raw []byte, rec *fastRecord, pbEnc, fbEnc []byte, d uint64) {
	var diffs []string
	defer func() {
		if r := recover(); r != nil {
			diffs = append(diffs, fmt.Sprintf("panic: %v", r))
		}
		f.trace = nil
		f.verify.verified++
		if len(diffs) > 0 {
			f.verify.mismatches++
			f.app.Logger().Error("fast recheck verify mismatch", "tx", lazyTxHash(raw), "height", ctx.BlockHeight(),
				"signer", rec.signer.String(), "seq", rec.seq, "details", strings.Join(diffs, "; "))
		}
	}()

	tx, err := f.app.checkTxDecoder()(raw)
	if err != nil {
		diffs = append(diffs, "decode: "+err.Error())
		return
	}
	branch := ctx.MultiStore().CacheMultiStore()
	// The context BaseApp builds for a recheck (getContextForTx), on the branch.
	vctx := ctx.WithMultiStore(branch).
		WithTxBytes(raw).
		WithGasMeter(storetypes.NewInfiniteGasMeter()).
		WithIsSigverifyTx(true).
		WithConsensusParams(f.app.GetConsensusParams(ctx)).
		WithIsReCheckTx(true).
		WithEventManager(sdk.NewEventManager())
	tr := newAnteTrace(rec.signer, f.fcAddr, accountNumberKey(rec.accNum), f.app.keys)
	f.trace = tr
	newCtx, err := f.app.AnteHandler()(vctx, tx, false)
	if err != nil {
		diffs = append(diffs, "full ante rejects: "+err.Error())
		return
	}
	gas := newCtx.GasMeter().GasConsumed()
	switch diff := int64(gas) - int64(rec.gasUsed); {
	case gas > rec.gasWanted:
		diffs = append(diffs, fmt.Sprintf("gas %d > limit %d", gas, rec.gasWanted))
	case d == 0 && diff != 0:
		diffs = append(diffs, fmt.Sprintf("gas used %d != recorded %d (lengths unchanged)", gas, rec.gasUsed))
	case max(diff, -diff) > int64(d*fastRecheckGasPerByte):
		diffs = append(diffs, fmt.Sprintf("gas used %d vs recorded %d exceeds %d/byte x %d", gas, rec.gasUsed, fastRecheckGasPerByte, d))
	}
	if p := newCtx.Priority(); p != rec.priority {
		diffs = append(diffs, fmt.Sprintf("priority %d != %d", p, rec.priority))
	}
	if tr.reject != "" {
		diffs = append(diffs, "ante footprint: "+tr.reject)
	}
	if acc := branch.GetKVStore(f.authKey).Get(rec.accKey); !bytes.Equal(acc, rec.accAfter) {
		diffs = append(diffs, fmt.Sprintf("account after %X != replay %X", acc, rec.accAfter))
	}
	bankKV := branch.GetKVStore(f.bankKey)
	if pb := bankKV.Get(rec.balKey); !bytes.Equal(pb, pbEnc) {
		diffs = append(diffs, fmt.Sprintf("payer balance %q != replay %q", pb, pbEnc))
	}
	if fb := bankKV.Get(f.fcBalKey); !bytes.Equal(fb, fbEnc) {
		diffs = append(diffs, fmt.Sprintf("fee collector balance %q != replay %q", fb, fbEnc))
	}
}
