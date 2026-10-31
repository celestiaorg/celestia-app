package ante_test

import (
	"bytes"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	appante "github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"
)

// sigCacheFixture holds an app whose accounts have recorded public keys, the
// keys to re-sign with and a few signed txs to mutate. It is built once per
// process; every iteration verifies on a cache context, so iterations do not
// interfere.
type sigCacheFixture struct {
	app      *app.App
	ctx      sdk.Context
	txConfig client.TxConfig
	kr       keyring.Keyring
	names    []string
	addrs    []string
	accNums  []uint64
	pubKeys  []cryptotypes.PubKey
	txs      []sigFuzzTx
}

// sigFuzzTx is an encoded tx and the index of the account that signed it.
type sigFuzzTx struct {
	raw   []byte
	owner int
}

const (
	sigFuzzRecord = 8 // bytes per edit: tx index, op, flags, then op-specific bytes
	sigFuzzOps    = 11

	sigFuzzReplace  = 1 << 0 // replace the source tx instead of appending the edited copy
	sigFuzzResign   = 1 << 1 // re-sign after the edit
	sigFuzzOtherKey = 1 << 2 // re-sign with the next account's key
)

var sigFuzzModes = []signing.SignMode{
	signing.SignMode_SIGN_MODE_DIRECT, signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
	signing.SignMode_SIGN_MODE_TEXTUAL, signing.SignMode_SIGN_MODE_DIRECT_AUX,
	signing.SignMode_SIGN_MODE_UNSPECIFIED, signing.SignMode(999),
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

	fx := &sigCacheFixture{app: testApp, ctx: ctx, txConfig: enc.TxConfig, kr: kr, names: accounts}
	for owner, account := range accounts {
		acc := testApp.AccountKeeper.GetAccount(ctx, testfactory.GetAddress(kr, account))
		require.NotNil(t, acc)
		record, err := kr.Key(account)
		require.NoError(t, err)
		pubKey, err := record.GetPubKey()
		require.NoError(t, err)
		require.NoError(t, acc.SetPubKey(pubKey))
		testApp.AccountKeeper.SetAccount(ctx, acc)
		fx.addrs = append(fx.addrs, acc.GetAddress().String())
		fx.accNums = append(fx.accNums, acc.GetAccountNumber())
		fx.pubKeys = append(fx.pubKeys, pubKey)

		for _, sequence := range []uint64{acc.GetSequence(), acc.GetSequence() + 1} {
			raw, err := enc.TxConfig.TxEncoder()(signedTx(t, enc.TxConfig, kr, account, acc.GetAccountNumber(), sequence))
			require.NoError(t, err)
			fx.txs = append(fx.txs, sigFuzzTx{raw: raw, owner: owner})
		}
	}
	return fx
}

// mutateSigTxs applies up to eight edits to the fixture txs, each read from a
// fixed-width record. Op 0 flips a bit of the encoding, so malformed
// encodings stay covered; the others decode the tx, change one typed field
// and re-encode it, re-signing it if the flags ask. An edited copy is appended
// unless the flags ask to replace its source, so the batch usually holds a
// valid tx next to a variant whose signature may still hit its cache entry.
func (fx *sigCacheFixture) mutateSigTxs(script []byte) [][]byte {
	txs := slices.Clone(fx.txs)
	c := fuzzutil.New(script)
	for n := 0; n < 8 && !c.Exhausted(); n++ {
		r := c.Record(sigFuzzRecord)
		i := r.Intn(len(txs))
		op, flags := r.Byte()%sigFuzzOps, r.Byte()
		edited := sigFuzzTx{owner: txs[i].owner}
		if op == 0 {
			edited.raw = bytes.Clone(txs[i].raw)
			at, bit := r.Uint16(), r.Byte()
			if len(edited.raw) > 0 {
				edited.raw[int(at)%len(edited.raw)] ^= 1 << (bit % 8)
			}
		} else {
			var ok bool
			if edited.raw, ok = fx.editFields(txs[i], txs[(i+1)%len(txs)].raw, op, flags, r); !ok {
				continue
			}
		}
		if flags&sigFuzzReplace != 0 {
			txs[i] = edited
		} else {
			txs = append(txs, edited)
		}
	}
	out := make([][]byte, len(txs))
	for i, tx := range txs {
		out[i] = tx.raw
	}
	return out
}

// editFields applies one typed edit to tx. other supplies a signature, body
// or auth info from a different tx. It reports false if tx does not decode
// into its parts.
func (fx *sigCacheFixture) editFields(tx sigFuzzTx, other []byte, op, flags byte, r *fuzzutil.Consumer) ([]byte, bool) {
	var raw, otherRaw txtypes.TxRaw
	var body txtypes.TxBody
	var auth txtypes.AuthInfo
	if raw.Unmarshal(tx.raw) != nil || body.Unmarshal(raw.BodyBytes) != nil || auth.Unmarshal(raw.AuthInfoBytes) != nil {
		return nil, false
	}
	_ = otherRaw.Unmarshal(other)
	key := tx.owner
	if flags&sigFuzzOtherKey != 0 {
		key = (key + 1) % len(fx.names)
	}
	chainID, accNum, mode := testutil.ChainID, fx.accNums[key], signing.SignMode_SIGN_MODE_DIRECT
	resign := flags&sigFuzzResign != 0
	var swapBody, swapAuth bool

	switch op {
	case 1: // sequence
		for _, si := range auth.SignerInfos {
			si.Sequence += uint64(int64(int8(r.Byte())))
		}
	case 2: // account number, which only the sign bytes carry
		accNum += uint64(int64(int8(r.Byte())))
		resign = true
	case 3: // chain ID, which only the sign bytes carry
		chainID = []string{"", testutil.ChainID + "-fork", "other-chain", testutil.ChainID}[r.Intn(4)]
		resign = true
	case 4: // sign mode
		mode = sigFuzzModes[r.Intn(len(sigFuzzModes))]
		for _, si := range auth.SignerInfos {
			si.ModeInfo = &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: mode}}}
		}
	case 5: // signer-info and signature counts, set independently
		auth.SignerInfos = resize(auth.SignerInfos, r.Intn(4))
		raw.Signatures = resize(raw.Signatures, r.Intn(4))
	case 6: // fee
		if auth.Fee == nil || len(auth.Fee.Amount) == 0 {
			return nil, false
		}
		switch v := r.Byte(); v % 3 {
		case 0:
			auth.Fee.Amount[0].Amount = auth.Fee.Amount[0].Amount.AddRaw(int64(r.Uint16()))
		case 1:
			auth.Fee.GasLimit = uint64(r.Uint32())
		default:
			auth.Fee.Amount = nil
		}
	case 7: // memo
		body.Memo = string(r.Bytes(sigFuzzRecord))
	case 8: // signature bytes
		if len(raw.Signatures) == 0 {
			return nil, false
		}
		sig := bytes.Clone(raw.Signatures[0])
		switch v := r.Byte(); v % 3 {
		case 0:
			if len(sig) > 0 {
				sig[r.Intn(len(sig))] ^= 1 << (v % 8)
			}
		case 1:
			if len(otherRaw.Signatures) > 0 {
				sig = otherRaw.Signatures[0]
			}
		default:
			sig = sig[:r.Intn(len(sig))]
		}
		raw.Signatures[0] = sig
	case 9: // public key: another account's, or none
		if len(auth.SignerInfos) == 0 {
			return nil, false
		}
		auth.SignerInfos[0].PublicKey = nil
		if k := r.Intn(len(fx.pubKeys) + 1); k < len(fx.pubKeys) {
			pk, err := codectypes.NewAnyWithValue(fx.pubKeys[k])
			if err != nil {
				panic(err)
			}
			auth.SignerInfos[0].PublicKey = pk
		}
	case 10: // body or auth info of another tx
		swapBody = r.Bool()
		swapAuth = !swapBody
	}

	var err error
	if raw.BodyBytes, err = body.Marshal(); err != nil {
		return nil, false
	}
	if raw.AuthInfoBytes, err = auth.Marshal(); err != nil {
		return nil, false
	}
	if swapBody {
		raw.BodyBytes = otherRaw.BodyBytes
	}
	if swapAuth {
		raw.AuthInfoBytes = otherRaw.AuthInfoBytes
	}
	if resign {
		fx.resign(&raw, &auth, key, chainID, accNum, mode)
	}
	out, err := raw.Marshal()
	return out, err == nil
}

// resign replaces the first signature of raw with one by account key over the
// given chain ID, account number and sign mode. It leaves raw unchanged if the
// tx cannot be signed in that mode.
func (fx *sigCacheFixture) resign(raw *txtypes.TxRaw, auth *txtypes.AuthInfo, key int, chainID string, accNum uint64, mode signing.SignMode) {
	unsigned, err := raw.Marshal()
	if err != nil {
		return
	}
	tx, err := fx.txConfig.TxDecoder()(unsigned)
	if err != nil {
		return
	}
	var sequence uint64
	if len(auth.SignerInfos) > 0 {
		sequence = auth.SignerInfos[0].Sequence
	}
	signerData := authsigning.SignerData{Address: fx.addrs[key], ChainID: chainID, AccountNumber: accNum, Sequence: sequence, PubKey: fx.pubKeys[key]}
	signBytes, err := func() (b []byte, err error) {
		defer func() {
			if recover() != nil {
				err = errSigFuzzUnsignable
			}
		}()
		return authsigning.GetSignBytesAdapter(fx.ctx, fx.txConfig.SignModeHandler(), mode, signerData, tx)
	}()
	if err != nil {
		return
	}
	sig, _, err := fx.kr.Sign(fx.names[key], signBytes, mode)
	if err != nil {
		return
	}
	if len(raw.Signatures) == 0 {
		raw.Signatures = [][]byte{sig}
	} else {
		raw.Signatures[0] = sig
	}
}

var errSigFuzzUnsignable = errors.New("tx cannot be signed in this mode")

// resize truncates s to n elements or grows it by repeating its first.
func resize[T any](s []T, n int) []T {
	if n <= len(s) {
		return s[:n]
	}
	if len(s) == 0 {
		return s
	}
	for len(s) < n {
		s = append(s, s[0])
	}
	return s
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
	if err != nil {
		return false
	}
	// Excluded pending a fix; see the Linear issue for this target.
	// GetSigningTxData panics on a signer info without a public key.
	if adaptable, has := tx.(authsigning.V2AdaptableTx); has {
		_ = adaptable.GetSigningTxData()
	}
	return true
}

// fuzzSigCache warms a signature cache with the pre-pass ProcessProposal runs,
// then checks that the cached verification decorator decides every tx exactly
// as an uncached one does. A hit on a key that does not bind every sign-bytes
// input would accept a signature on this node only.
//
// It returns how many txs reached the decorator and how many it accepted.
func fuzzSigCache(t *testing.T, script []byte) (reached, accepted int) {
	t.Helper()
	sigCacheOnce.Do(func() { sigCacheFx = newSigCacheFixture(t) })
	fx := sigCacheFx

	var txs []sdk.Tx
	for _, raw := range fx.mutateSigTxs(script) {
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
		} else {
			accepted++
		}
	}
	return len(txs), accepted
}

// TestSigCacheFuzzSeeds checks the unmodified txs warm the cache, so the fuzz
// target compares cached hits rather than two cold runs, and that typed edits
// reach the decorator: a stale signature is rejected and a re-signed tx is
// accepted.
func TestSigCacheFuzzSeeds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		script            []byte
		reached, accepted int
	}{
		{"fixture", nil, 4, 2},
		{"memo, stale signature", fuzzutil.Records(sigFuzzRecord, []byte{0, 7, 0, 1, 'x'}), 5, 2},
		{"memo, re-signed", fuzzutil.Records(sigFuzzRecord, []byte{0, 7, sigFuzzResign, 1, 'x'}), 5, 3},
		{"other key", fuzzutil.Records(sigFuzzRecord, []byte{0, 7, sigFuzzResign | sigFuzzOtherKey, 1, 'x'}), 5, 2},
		{"fee, re-signed in place", fuzzutil.Records(sigFuzzRecord, []byte{2, 6, sigFuzzResign | sigFuzzReplace, 1, 0, 0, 0, 1}), 4, 2},
		{"wrong chain ID", fuzzutil.Records(sigFuzzRecord, []byte{0, 3, 0, 1}), 5, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached, accepted := fuzzSigCache(t, tc.script)
			require.Equal(t, tc.reached, reached)
			require.Equal(t, tc.accepted, accepted)
		})
	}
	txs := make([]sdk.Tx, 0, len(sigCacheFx.txs))
	for _, fixture := range sigCacheFx.txs {
		tx, err := sigCacheFx.txConfig.TxDecoder()(fixture.raw)
		require.NoError(t, err)
		txs = append(txs, tx)
	}
	cache := sigcache.New(64)
	appante.PreverifyTxSignatures(sigCacheFx.ctx, sigCacheFx.app.AccountKeeper, sigCacheFx.txConfig.SignModeHandler(), cache, txs)
	require.Equal(t, len(txs), cache.Len(), "every signed tx must warm one entry")
}

// FuzzCachedSigVerification edits signed txs from two accounts at two
// sequences each, mostly by changing typed fields with or without re-signing,
// warms a signature cache with all of them and requires the cached signature
// verification decorator to reach the uncached decision, with the same error,
// for every tx.
func FuzzCachedSigVerification(f *testing.F) {
	f.Add([]byte{})
	for op := range byte(sigFuzzOps) {
		for txIndex := range byte(4) {
			f.Add(fuzzutil.Records(sigFuzzRecord, []byte{txIndex, op, 0, 1, 'x', 0, 60}))
			f.Add(fuzzutil.Records(sigFuzzRecord, []byte{txIndex, op, sigFuzzResign, 3, 1, 0, 150}, []byte{txIndex + 1, 8, 0, 1}))
		}
	}
	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) > 8*sigFuzzRecord {
			return
		}
		fuzzSigCache(t, script)
	})
}
