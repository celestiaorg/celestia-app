package ante_test

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	appante "github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"
)

// sigCacheFixture holds an app whose accounts have recorded public keys and a
// few signed txs to mutate. It is built once per process; every iteration
// verifies on a cache context, so iterations do not interfere.
type sigCacheFixture struct {
	app      *app.App
	ctx      sdk.Context
	txConfig client.TxConfig
	txs      [][]byte
}

var (
	sigCacheOnce sync.Once
	sigCacheFx   *sigCacheFixture
)

func newSigCacheFixture(t *testing.T) *sigCacheFixture {
	t.Helper()
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	}).WithChainID(testutil.ChainID)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	var txs [][]byte
	for _, account := range accounts {
		acc := testApp.AccountKeeper.GetAccount(ctx, testfactory.GetAddress(kr, account))
		require.NotNil(t, acc)
		record, err := kr.Key(account)
		require.NoError(t, err)
		pubKey, err := record.GetPubKey()
		require.NoError(t, err)
		require.NoError(t, acc.SetPubKey(pubKey))
		testApp.AccountKeeper.SetAccount(ctx, acc)

		for _, sequence := range []uint64{acc.GetSequence(), acc.GetSequence() + 1} {
			raw, err := enc.TxConfig.TxEncoder()(signedTx(t, enc.TxConfig, kr, account, acc.GetAccountNumber(), sequence))
			require.NoError(t, err)
			txs = append(txs, raw)
		}
	}
	return &sigCacheFixture{app: testApp, ctx: ctx, txConfig: enc.TxConfig, txs: txs}
}

// mutateSigTxs applies up to eight edits to txs. Each edit consumes four bytes
// of ops: tx index, operation and a 16-bit offset.
func mutateSigTxs(txs [][]byte, ops, extra []byte) [][]byte {
	out := make([][]byte, len(txs))
	for i, tx := range txs {
		out[i] = bytes.Clone(tx)
	}
	for n := 0; n+4 <= len(ops) && n/4 < 8; n += 4 {
		i := int(ops[n]) % len(out)
		tx := out[i]
		at := 0
		if len(tx) > 0 {
			at = (int(ops[n+2])<<8 | int(ops[n+3])) % len(tx)
		}
		switch ops[n+1] % 6 {
		case 0:
			if len(tx) > 0 {
				tx[at] ^= 1 << (ops[n+3] % 8)
			}
		case 1:
			if len(tx) > 0 {
				tx[at] = ops[n+2]
			}
		case 2:
			out[i] = tx[:at]
		case 3:
			out[i] = append(append(append([]byte{}, tx[:at]...), extra...), tx[at:]...)
		case 4:
			// Splice the tail of another tx: mixes bodies, auth infos and
			// signatures across signers and sequences.
			other := out[(i+1)%len(out)]
			from := min(at, len(other))
			out[i] = append(append([]byte{}, tx[:at]...), other[from:]...)
		case 5:
			out = append(out, bytes.Clone(tx))
		}
	}
	return out
}

// reachesSigVerification reports whether the ante chain would hand tx to the
// signature verification decorator: earlier decorators run ValidateBasic.
func reachesSigVerification(tx sdk.Tx) (ok bool) {
	if v, has := tx.(sdk.HasValidateBasic); has && v.ValidateBasic() != nil {
		return false
	}
	sigTx, has := tx.(authsigning.Tx)
	if !has {
		return false
	}
	// GetSignaturesV2 panics on more signer infos than signatures and on
	// malformed mode infos; the ante chain recovers it. Remove this exclusion
	// with the fix: https://linear.app/celestia/issue/PROTOCO-3482.
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	_, err := sigTx.GetSignaturesV2()
	return err == nil
}

// fuzzSigCache warms a signature cache with the pre-pass ProcessProposal runs,
// then checks that the cached verification decorator decides every tx exactly
// as an uncached one does. A hit on a key that does not bind every sign-bytes
// input would accept a signature on this node only.
func fuzzSigCache(t *testing.T, ops, extra []byte) {
	t.Helper()
	sigCacheOnce.Do(func() { sigCacheFx = newSigCacheFixture(t) })
	fx := sigCacheFx

	var txs []sdk.Tx
	for _, raw := range mutateSigTxs(fx.txs, ops, extra) {
		if tx, err := fx.txConfig.TxDecoder()(raw); err == nil && reachesSigVerification(tx) {
			txs = append(txs, tx)
		}
	}
	ctx, _ := fx.ctx.CacheContext()
	handler := fx.txConfig.SignModeHandler()
	warmCache := sigcache.New(64)
	appante.PreverifyTxSignatures(ctx, fx.app.AccountKeeper, handler, warmCache, txs)

	for _, tx := range txs {
		cold := appante.NewCachedSigVerificationDecorator(fx.app.AccountKeeper, handler, sigcache.New(64))
		_, coldErr := cold.AnteHandle(ctx, tx, false, nextAnteHandler)
		warm := appante.NewCachedSigVerificationDecorator(fx.app.AccountKeeper, handler, warmCache)
		_, warmErr := warm.AnteHandle(ctx, tx, false, nextAnteHandler)
		require.Equal(t, coldErr == nil, warmErr == nil, "cold: %v, warm: %v", coldErr, warmErr)
		if coldErr != nil {
			require.Equal(t, coldErr.Error(), warmErr.Error())
		}
	}
}

// TestSigCacheFuzzSeeds checks the unmodified txs warm the cache, so the fuzz
// target compares cached hits rather than two cold runs.
func TestSigCacheFuzzSeeds(t *testing.T) {
	fuzzSigCache(t, nil, nil)
	txs := make([]sdk.Tx, 0, len(sigCacheFx.txs))
	for _, raw := range sigCacheFx.txs {
		tx, err := sigCacheFx.txConfig.TxDecoder()(raw)
		require.NoError(t, err)
		txs = append(txs, tx)
	}
	cache := sigcache.New(64)
	appante.PreverifyTxSignatures(sigCacheFx.ctx, sigCacheFx.app.AccountKeeper, sigCacheFx.txConfig.SignModeHandler(), cache, txs)
	require.Equal(t, len(txs), cache.Len(), "every signed tx must warm one entry")
}

// FuzzCachedSigVerification mutates signed txs from two accounts at two
// sequences each, warms a signature cache with all of them and requires the
// cached signature verification decorator to reach the uncached decision,
// with the same error, for every tx.
func FuzzCachedSigVerification(f *testing.F) {
	f.Add([]byte{}, []byte{})
	for op := range byte(6) {
		for txIndex := range byte(4) {
			f.Add([]byte{txIndex, op, 0, 60}, []byte{0x0a, 0x00})
			f.Add([]byte{txIndex, op, 0, 150, txIndex + 1, 4, 0, 90}, []byte{})
		}
	}
	f.Fuzz(func(t *testing.T, ops, extra []byte) {
		if len(extra) > 1<<12 {
			return
		}
		fuzzSigCache(t, ops, extra)
	})
}
