package keeper_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const (
	pffServerHeight     = 100
	pffServerDeliveries = 6
	pffServerRecord     = 3 + pffRecord // source, time advance, edit flag, then one pffMutator edit
	pffServerHeader     = 8
)

// pffServerPowers are unequal so the quorum prefix depends on which
// validators signed: 2/3 of 10 is reached by {4,3}, {4,2,1} or {3,2,1}+4.
var pffServerPowers = []int64{1_000_000, 2_000_000, 3_000_000, 4_000_000}

var (
	pffServerValKeys = func() []ed25519.PrivKey {
		keys := make([]ed25519.PrivKey, len(pffServerPowers))
		for i := range keys {
			keys[i] = ed25519.GenPrivKeyFromSecret([]byte{'p', 'f', 'f', 'v', byte(i)})
		}
		return keys
	}()
	pffServerPayers = []*secp256k1.PrivKey{
		secp256k1.GenPrivKeyFromSecret([]byte("pff-payer-0")),
		secp256k1.GenPrivKeyFromSecret([]byte("pff-payer-1")),
	}
	pffServerNamespace = share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes()
)

type pffServerFixture struct {
	s      *MsgServerTestSuite
	ledger *escrowLedger
	total  math.Int
	m      *pffMutator
}

// newPFFServerFixture builds a fibre keeper with a signature cache, a staking
// mock that stores only the validator set signing pffServerHeight, and a bank
// ledger. Each payer deposits a fuzz-chosen amount, optionally locking part of
// it in a pending withdrawal.
func newPFFServerFixture(t *testing.T, header *fuzzutil.Consumer) *pffServerFixture {
	s := new(MsgServerTestSuite)
	s.SetT(t)
	s.SetupTest()
	s.ctx = s.ctx.WithBlockTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)).WithBlockHeight(pffServerHeight).WithLogger(log.NewNopLogger())
	s.keeper = keeper.NewKeeper(s.cdc, s.storeKey, s.bankKeeper, s.stakingKeeper, s.authority, false, sigcache.New(64))
	s.msgServer = keeper.NewMsgServerImpl(*s.keeper)

	valset := make([]stakingtypes.Validator, len(pffServerValKeys))
	for i, k := range pffServerValKeys {
		pk, err := cryptocodec.FromCmtPubKeyInterface(k.PubKey())
		require.NoError(t, err)
		anyPK, err := codectypes.NewAnyWithValue(pk)
		require.NoError(t, err)
		valset[i] = stakingtypes.Validator{ConsensusPubkey: anyPK, Tokens: math.NewInt(pffServerPowers[i])}
	}
	info := stakingtypes.HistoricalInfo{Header: cmtproto.Header{Height: pffServerHeight - 1}, Valset: valset}
	s.stakingKeeper.GetHistoricalInfoFn = func(_ context.Context, h int64) (stakingtypes.HistoricalInfo, error) {
		if h != pffServerHeight-1 {
			return stakingtypes.HistoricalInfo{}, stakingtypes.ErrNoHistoricalInfo
		}
		return info, nil
	}

	f := &pffServerFixture{s: s, total: math.NewInt(escrowFuzzUserBalance * int64(len(pffServerPayers)))}
	f.ledger = &escrowLedger{balances: map[string]math.Int{types.ModuleName: math.ZeroInt(), authtypes.FeeCollectorName: math.ZeroInt()}}
	for _, k := range pffServerPayers {
		f.ledger.balances[sdk.AccAddress(k.PubKey().Address()).String()] = math.NewInt(escrowFuzzUserBalance)
	}
	s.bankKeeper.SendCoinsFromAccountToModuleFn = func(_ context.Context, from sdk.AccAddress, to string, amt sdk.Coins) error {
		return f.ledger.move(from.String(), to, amt)
	}
	s.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, from string, to sdk.AccAddress, amt sdk.Coins) error {
		return f.ledger.move(from, to.String(), amt)
	}
	s.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, from, to string, amt sdk.Coins) error {
		return f.ledger.move(from, to, amt)
	}
	f.m = &pffMutator{f: &preverifyFixture{valPrivKeys: pffServerValKeys}, signers: pffServerPayers}

	for _, k := range pffServerPayers {
		signer := sdk.AccAddress(k.PubKey().Address()).String()
		deposit := int64(header.Uint16()) * 100
		if deposit > 0 {
			_, err := s.msgServer.DepositToEscrow(s.ctx, &types.MsgDepositToEscrow{Signer: signer, Amount: sdk.NewInt64Coin(appconsts.BondDenom, deposit)})
			require.NoError(t, err)
		}
		if lock := deposit * int64(header.Byte()) / 255; lock > 0 {
			_, err := s.msgServer.RequestWithdrawal(s.ctx, &types.MsgRequestWithdrawal{Signer: signer, Amount: sdk.NewInt64Coin(appconsts.BondDenom, lock)})
			require.NoError(t, err)
		}
	}
	return f
}

// freshTx signs a new promise of payer with a full certificate and wraps it
// in a signed tx.
func (f *pffServerFixture) freshTx(t *testing.T, payer int, blobSize uint32, nonce uint64) []byte {
	key := pffServerPayers[payer]
	promise := types.PaymentPromise{
		ChainId:           f.s.ctx.ChainID(),
		Height:            pffServerHeight,
		Namespace:         pffServerNamespace,
		BlobSize:          blobSize,
		Commitment:        make([]byte, 32),
		CreationTimestamp: f.s.ctx.BlockTime(),
		SignerPublicKey:   *key.PubKey().(*secp256k1.PubKey),
	}
	binary.BigEndian.PutUint64(promise.Commitment, nonce)
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, err = key.Sign(signBytes)
	require.NoError(t, err)
	sigs := make([][]byte, len(pffServerValKeys))
	for i, k := range pffServerValKeys {
		sigs[i], err = k.Sign(signBytes)
		require.NoError(t, err)
	}
	msg := &types.MsgPayForFibre{Signer: sdk.AccAddress(key.PubKey().Address()).String(), PaymentPromise: promise, ValidatorSignatures: sigs}
	return f.m.signedTx(t, msg)
}

// requireDecodeConsistent checks DecodePayForFibre and DerivePayForFibre
// against the parser and against keys derived independently of decode.go,
// and that re-encoding the decoded message keys it identically.
func requireDecodeConsistent(t *testing.T, raw []byte) *types.DecodedPayForFibre {
	parsed, isPFF, err := types.ParsePayForFibreTx(raw)
	d := keeper.DecodePayForFibre(raw)
	if !isPFF || err != nil {
		require.Nil(t, d, "decoded a tx the parser rejects")
		return nil
	}
	require.NotNil(t, d, "the parser accepts a tx decode rejects")
	require.True(t, bytes.Equal(raw, d.Raw))
	want, err := parsed.Marshal()
	require.NoError(t, err)
	got, err := d.Msg.Marshal()
	require.NoError(t, err)
	require.Equal(t, want, got, "decoded message differs from the parsed one")

	certKey, certErr := parsed.SigCacheKey()
	require.Equal(t, certErr == nil, d.CertKeyed)
	if certErr == nil {
		require.Equal(t, certKey, d.CertKey)
	}
	pp := fibre.PaymentPromise{}
	var signBytes []byte
	if pp.FromProto(&parsed.PaymentPromise) == nil && pp.SignerKey != nil {
		signBytes, _ = pp.SignBytes()
	}
	require.Equal(t, len(signBytes) > 0, d.PromiseKeyed(), "promise keyed iff it converts and has sign bytes")
	if len(signBytes) > 0 {
		require.Equal(t, signBytes, d.PromiseSignBytes)
		require.Equal(t, sigcache.NewKey(sigcache.PromiseSignature, pp.SignerKey.Bytes(), signBytes, pp.Signature), d.PromiseKey)
	}

	derived := keeper.DerivePayForFibre(raw, parsed)
	require.Equal(t, d.CertKey, derived.CertKey)
	require.Equal(t, d.CertKeyed, derived.CertKeyed)
	require.Equal(t, d.PromiseKey, derived.PromiseKey)
	require.Equal(t, d.PromiseSignBytes, derived.PromiseSignBytes)

	// The keys depend on the message, not on how raw encoded it.
	again := keeper.DecodePayForFibre(minimalPFFTx(t, d.Msg))
	require.NotNil(t, again, "a decoded message must decode after re-encoding")
	require.Equal(t, d.CertKey, again.CertKey)
	require.Equal(t, d.PromiseKey, again.PromiseKey)
	return d
}

func minimalPFFTx(t *testing.T, msg *types.MsgPayForFibre) []byte {
	return (&preverifyFixture{}).txBytesFor(t, msg)
}

// modelCertificate decides a certificate without the keeper: only the set
// stored for pffServerHeight-1 signs, signatures are positional, empty ones
// are skipped, an invalid one before the quorum is reached rejects, and
// verification stops at 2/3 of the voting power (rounded down).
func modelCertificate(signBytes []byte, height int64, sigs [][]byte) bool {
	if height != pffServerHeight || len(sigs) > len(pffServerValKeys) {
		return false
	}
	total := int64(0)
	for _, p := range pffServerPowers {
		total += p
	}
	required, power := total*2/3, int64(0)
	for i, sig := range sigs {
		if len(sig) == 0 {
			continue
		}
		if !pffServerValKeys[i].PubKey().VerifySignature(signBytes, sig) {
			return false
		}
		if power += pffServerPowers[i]; power >= required {
			return true
		}
	}
	return power >= required
}

// modelPaymentAmount is the escrow charge: fixed plus per-256KiB-chunk gas at
// the default minimum gas price, rounded up to whole utia.
func modelPaymentAmount(blobSize uint32) math.Int {
	chunks := (uint64(blobSize) + uint64(appconsts.PFBFibreChunkSize) - 1) / uint64(appconsts.PFBFibreChunkSize)
	gas := new(big.Int).SetUint64(appconsts.PFBFibreGasFixedCost + appconsts.PFBFibreGasPerChunk*chunks)
	microPrice := float64(appconsts.DefaultMinGasPrice) * 1e6
	price := new(big.Rat).SetFrac64(int64(microPrice+0.5), 1e6)
	charge := new(big.Rat).Mul(price, new(big.Rat).SetInt(gas))
	q, r := new(big.Int).QuoRem(charge.Num(), charge.Denom(), new(big.Int))
	if r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return math.NewIntFromBigInt(q)
}

// FuzzPayForFibreMsgServer delivers up to six pay-for-fibre txs, fresh or
// replayed from earlier deliveries, each after one typed pffMutator edit
// (re-signed or not) and an optional block time advance. Every tx goes
// through DecodePayForFibre, ValidateBasic, the ante certificate check
// ValidatePayForFibreSignatures and msgServer.PayForFibre on a cache context,
// as DeliverTx runs them. Decode must agree with the parser and with keys
// derived independently; the certificate check must accept exactly when the
// promise validates and a model of the 2/3 quorum walk accepts; settlement
// must accept exactly when a model of the stateful window, escrow and replay
// checks does, charge exactly the modelled payment, emit the typed event and
// reject an immediate replay. Escrow invariants hold after every delivery.
func FuzzPayForFibreMsgServer(f *testing.F) {
	header := []byte{0x10, 0, 0, 0x10, 0, 0, 0, 0}
	seed := func(records ...[]byte) []byte {
		return append(bytes.Clone(header), fuzzutil.Records(pffServerRecord, records...)...)
	}
	f.Add(seed([]byte{0, 0}, []byte{1, 0}, []byte{2, 0}))
	for op := range byte(pffOps) {
		f.Add(seed([]byte{0, 0, 1, 0, op, 0, 1, 0, 7}, []byte{3, 0}))
		f.Add(seed([]byte{1, 1, 1, 0, op, pffResign | pffResignCert | pffReplace, 2, 0, 120}, []byte{2, 0}))
	}
	// Remove the strongest validator's signature, then also the next one.
	f.Add(seed([]byte{0, 0, 1, 0, 5, pffReplace, 3, 3, 1}, []byte{2, 0, 1, 0, 5, pffReplace, 3, 2, 1}))
	// An overdrawn payer with a pending withdrawal, then a time advance past expiry.
	f.Add(append([]byte{0, 1, 0xff, 0, 1, 0x80, 0, 0}, fuzzutil.Records(pffServerRecord, []byte{0, 0}, []byte{1, 0}, []byte{2, 61})...))
	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) > pffServerHeader+pffServerDeliveries*pffServerRecord {
			return
		}
		fuzzPayForFibreMsgServer(t, script)
	})
}

// fuzzPayForFibreMsgServer runs one script and returns how many deliveries
// settled.
func fuzzPayForFibreMsgServer(t *testing.T, script []byte) int {
	c := fuzzutil.New(script)
	fx := newPFFServerFixture(t, c.Record(pffServerHeader))
	s := fx.s
	var pool [][]byte
	settled := make(map[string]bool)
	accepted := 0

	for n := 0; n < pffServerDeliveries && !c.Exhausted(); n++ {
		r := c.Record(pffServerRecord)
		src, advance, edit := r.Byte(), r.Byte(), r.Bool()
		s.ctx = s.ctx.WithBlockTime(s.ctx.BlockTime().Add(time.Duration(advance) * time.Minute))
		var raw []byte
		if src%4 < 2 || len(pool) == 0 {
			raw = fx.freshTx(t, int(src%2), uint32(src)*4096+1, uint64(n))
		} else {
			raw = pool[int(src>>2)%len(pool)]
		}
		if edit {
			record := make([]byte, pffRecord)
			for i := range record {
				record[i] = r.Byte()
			}
			if txs := fx.m.mutate([][]byte{raw}, record); len(txs) > 0 {
				raw = txs[len(txs)-1]
			}
		}
		pool = append(pool, raw)

		d := requireDecodeConsistent(t, raw)
		if d == nil || d.Msg.ValidateBasic() != nil {
			continue
		}
		msg := d.Msg
		pp := fibre.PaymentPromise{}
		require.NoError(t, pp.FromProto(&msg.PaymentPromise), "a promise passing ValidateBasic must convert")
		promiseOK := pp.Validate() == nil
		signBytes, err := pp.SignBytes()
		require.NoError(t, err)

		cacheCtx, write := s.ctx.CacheContext()
		anteErr := s.keeper.ValidatePayForFibreSignatures(cacheCtx, msg)
		wantAnte := promiseOK && modelCertificate(signBytes, msg.PaymentPromise.Height, msg.ValidatorSignatures)
		require.Equal(t, wantAnte, anteErr == nil, "certificate verdict: %v", anteErr)
		if anteErr != nil {
			continue
		}

		payer := sdk.AccAddress(msg.PaymentPromise.SignerPublicKey.Address()).String()
		hash, err := pp.Hash()
		require.NoError(t, err)
		before, found := s.keeper.GetEscrowAccount(s.ctx, payer)
		charge := modelPaymentAmount(msg.PaymentPromise.BlobSize)
		require.True(t, types.PaymentAmount(msg.PaymentPromise.BlobSize).Amount.Equal(charge), "payment amount for %d bytes", msg.PaymentPromise.BlobSize)
		params := s.keeper.GetParams(s.ctx)
		floor, err := s.keeper.GetPromiseFreshnessFloor(s.ctx)
		require.NoError(t, err)
		now, created, h := s.ctx.BlockTime(), msg.PaymentPromise.CreationTimestamp, msg.PaymentPromise.Height
		cutoff := now.Add(-params.WithdrawalDelay)
		if floor.After(cutoff) {
			cutoff = floor
		}
		wantSettle := msg.PaymentPromise.ChainId == s.ctx.ChainID() &&
			created.After(cutoff) && !created.After(now.Add(types.MaxPromiseClockSkew)) &&
			now.Before(created.Add(params.PaymentPromiseTimeout)) &&
			s.ctx.BlockHeight()-h <= int64(params.PaymentPromiseHeightWindow) && h <= s.ctx.BlockHeight()+1 &&
			!settled[string(hash)] && found && before.Balance.Amount.GTE(charge)

		feeBefore, moduleBefore := fx.ledger.balances[authtypes.FeeCollectorName], fx.ledger.balances[types.ModuleName]
		saved := fx.ledger.snapshot()
		_, err = s.msgServer.PayForFibre(cacheCtx, msg)
		require.Equal(t, wantSettle, err == nil, "settlement verdict: %v", err)
		if err != nil {
			*fx.ledger = saved
			require.False(t, s.keeper.IsPaymentProcessedByHash(s.ctx, hash) && !settled[string(hash)])
			continue
		}
		write()
		accepted++
		settled[string(hash)] = true
		require.True(t, s.keeper.IsPaymentProcessedByHash(s.ctx, hash))
		after, _ := s.keeper.GetEscrowAccount(s.ctx, payer)
		require.True(t, before.Balance.Amount.Sub(charge).Equal(after.Balance.Amount), "escrow charged the wrong amount")
		require.True(t, fx.ledger.balances[authtypes.FeeCollectorName].Sub(feeBefore).Equal(charge))
		require.True(t, moduleBefore.Sub(fx.ledger.balances[types.ModuleName]).Equal(charge))

		typed, err := sdk.TypedEventToEvent(&types.EventPayForFibre{
			Signer: payer, Namespace: msg.PaymentPromise.Namespace, Commitment: msg.PaymentPromise.Commitment,
			ValidatorCount: uint32(len(msg.ValidatorSignatures)),
		})
		require.NoError(t, err)
		events := cacheCtx.EventManager().Events()
		require.NotEmpty(t, events)
		require.Equal(t, typed, events[len(events)-1], "PayForFibreEvent differs from the typed event")

		replayCtx, _ := s.ctx.CacheContext()
		saved = fx.ledger.snapshot()
		_, err = s.msgServer.PayForFibre(replayCtx, msg)
		require.Error(t, err, "a settled promise settled again")
		*fx.ledger = saved
		requireEscrowInvariants(t, s, fx.ledger, fx.total)
	}
	return accepted
}

// TestPayForFibreMsgServerFuzzSeeds keeps the oracle non-vacuous: fresh,
// unedited promises from funded payers settle, and a replay does not.
func TestPayForFibreMsgServerFuzzSeeds(t *testing.T) {
	header := []byte{0x10, 0, 0, 0x10, 0, 0, 0, 0}
	script := append(bytes.Clone(header), fuzzutil.Records(pffServerRecord, []byte{0, 0}, []byte{1, 0}, []byte{2, 0})...)
	require.Equal(t, 2, fuzzPayForFibreMsgServer(t, script), "two fresh promises settle, the replay does not")
	unfunded := append(make([]byte, pffServerHeader), fuzzutil.Records(pffServerRecord, []byte{0, 0})...)
	require.Equal(t, 0, fuzzPayForFibreMsgServer(t, unfunded))
}
