package keeper_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// numPreverifyValidators is chosen so the 2/3 quorum is reached before the last
// signature: with four equal validators the prefix is three, leaving index 3
// outside it.
const numPreverifyValidators = 4

type preverifyFixture struct {
	ctx           sdk.Context
	storeKey      storetypes.StoreKey
	stakingKeeper *MockStakingKeeper
	keeper        *keeper.Keeper
	cache         *sigcache.Cache
	msg           *types.MsgPayForFibre
	certKey       sigcache.Key
	promiseKey    sigcache.Key
	valPrivKeys   []ed25519.PrivKey
	signerPrivKey *secp256k1.PrivKey
}

// withFreshCache returns a second view of the same chain state and message with
// an empty signature cache, so a run with the pre-pass can be compared against
// one without it.
func (f *preverifyFixture) withFreshCache(t *testing.T) *preverifyFixture {
	t.Helper()

	fresh := *f
	fresh.cache = sigcache.New(1024)
	fresh.keeper = keeper.NewKeeper(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), f.storeKey,
		&MockBankKeeper{}, f.stakingKeeper, authtypes.NewModuleAddress("gov").String(), false, fresh.cache)
	return &fresh
}

// promiseSigCacheKey mirrors the keeper's unexported derivation of a promise's
// cache key, so a test can name the exact entries a pass is allowed to leave
// behind rather than only asking whether the certificate is there.
// TestPreverifySignaturesSkipsCertificatesWhenNotRequested pins it against the
// keeper's own derivation, so the two cannot drift apart unnoticed.
func promiseSigCacheKey(t *testing.T, msg *types.MsgPayForFibre) sigcache.Key {
	t.Helper()

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	return sigcache.NewKey(sigcache.PromiseSignature, pp.SignerKey.Bytes(), signBytes, pp.Signature)
}

// refreshCertKey re-derives the certificate key after the message was mutated.
func (f *preverifyFixture) refreshCertKey(t *testing.T) {
	t.Helper()

	certKey, err := f.msg.SigCacheKey()
	require.NoError(t, err)
	f.certKey = certKey
}

func newPreverifyFixture(t *testing.T) *preverifyFixture {
	t.Helper()

	return newPreverifyFixtureWithValidators(t, numPreverifyValidators)
}

// newPreverifyFixtureWithValidators builds the fixture with a chosen validator
// count, so a certificate can be made to span more than one verification batch.
func newPreverifyFixtureWithValidators(t *testing.T, validators int) *preverifyFixture {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{ChainID: "test-chain", Time: time.Now().UTC(), Height: 100}, false, nil)
	cache := sigcache.New(1024)
	stakingKeeper := &MockStakingKeeper{}
	k := keeper.NewKeeper(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), storeKey,
		&MockBankKeeper{}, stakingKeeper, authtypes.NewModuleAddress("gov").String(), false, cache)

	valPrivKeys := make([]ed25519.PrivKey, validators)
	valset := make([]stakingtypes.Validator, validators)
	for i := range valPrivKeys {
		valPrivKeys[i] = ed25519.GenPrivKey()
		pk, err := cryptocodec.FromCmtPubKeyInterface(valPrivKeys[i].PubKey())
		require.NoError(t, err)
		anyPubKey, err := codectypes.NewAnyWithValue(pk)
		require.NoError(t, err)
		valset[i] = stakingtypes.Validator{
			OperatorAddress: sdk.ValAddress(valPrivKeys[i].PubKey().Address()).String(),
			ConsensusPubkey: anyPubKey,
			Tokens:          math.NewInt(1_000_000),
		}
	}
	stakingKeeper.historicalInfo = map[int64]stakingtypes.HistoricalInfo{
		ctx.BlockHeight(): {Header: cmtproto.Header{Height: ctx.BlockHeight()}, Valset: valset},
	}

	signerPrivKey := secp256k1.GenPrivKey()
	promise := types.PaymentPromise{
		ChainId:           "test-chain",
		Height:            ctx.BlockHeight(),
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          1000,
		Commitment:        make([]byte, 32),
		CreationTimestamp: ctx.BlockTime(),
		SignerPublicKey:   *signerPrivKey.PubKey().(*secp256k1.PubKey),
		Signature:         make([]byte, 64),
	}
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, err = signerPrivKey.Sign(signBytes)
	require.NoError(t, err)

	signatures := make([][]byte, validators)
	for i, valPrivKey := range valPrivKeys {
		signatures[i], err = valPrivKey.Sign(signBytes)
		require.NoError(t, err)
	}

	msg := &types.MsgPayForFibre{PaymentPromise: promise, ValidatorSignatures: signatures}
	certKey, err := msg.SigCacheKey()
	require.NoError(t, err)

	return &preverifyFixture{
		ctx:           ctx,
		storeKey:      storeKey,
		stakingKeeper: stakingKeeper,
		keeper:        k,
		cache:         cache,
		msg:           msg,
		certKey:       certKey,
		promiseKey:    promiseSigCacheKey(t, msg),
		valPrivKeys:   valPrivKeys,
		signerPrivKey: signerPrivKey,
	}
}

// signedMessage returns a fully signed message distinguished by nonce, so a
// test can build as many distinct certificates as it needs.
func (f *preverifyFixture) signedMessage(t *testing.T, nonce int) *types.MsgPayForFibre {
	t.Helper()

	promise := f.msg.PaymentPromise
	promise.Commitment = make([]byte, len(f.msg.PaymentPromise.Commitment))
	binary.BigEndian.PutUint64(promise.Commitment, uint64(nonce))
	promise.Signature = make([]byte, 64)

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, err = f.signerPrivKey.Sign(signBytes)
	require.NoError(t, err)

	signatures := make([][]byte, len(f.valPrivKeys))
	for i, valPrivKey := range f.valPrivKeys {
		signatures[i], err = valPrivKey.Sign(signBytes)
		require.NoError(t, err)
	}
	return &types.MsgPayForFibre{PaymentPromise: promise, ValidatorSignatures: signatures}
}

// decodedMessage encodes msg into a transaction and decodes it the way every
// pre-verification caller does.
func (f *preverifyFixture) decodedMessage(t *testing.T, msg *types.MsgPayForFibre) *types.DecodedPayForFibre {
	t.Helper()
	d := keeper.DecodePayForFibre(f.txBytesFor(t, msg))
	require.NotNil(t, d)
	return d
}

// txBytes wraps the fixture's message in the minimal Cosmos tx encoding that
// ParsePayForFibreMsg reads.
func (f *preverifyFixture) txBytes(t *testing.T) []byte {
	t.Helper()

	return f.txBytesFor(t, f.msg)
}

// txBytesFor wraps any message in the same encoding, so one call can carry
// several transactions.
func (f *preverifyFixture) txBytesFor(t *testing.T, msg *types.MsgPayForFibre) []byte {
	t.Helper()

	value, err := msg.Marshal()
	require.NoError(t, err)
	body, err := (&cosmostx.TxBody{Messages: []*codectypes.Any{{
		TypeUrl: types.MsgPayForFibreTypeURL,
		Value:   value,
	}}}).Marshal()
	require.NoError(t, err)
	raw, err := (&cosmostx.TxRaw{BodyBytes: body}).Marshal()
	require.NoError(t, err)
	return raw
}

// TestPreverifySignaturesRecordsOnlyWhatTheSequentialVerifierAccepts pins the
// property the whole pre-pass rests on, over the whole cache rather than the
// certificate alone: a message records every key it is entitled to exactly
// when the sequential verifier would accept it, and records nothing at all
// otherwise, so the sequential verifier stays the one that decides.
func TestPreverifySignaturesRecordsOnlyWhatTheSequentialVerifierAccepts(t *testing.T) {
	tests := map[string]struct {
		corrupt    int // index of the validator signature to corrupt, -1 for none
		wantCached bool
	}{
		"all signatures valid": {
			corrupt:    -1,
			wantCached: true,
		},
		"signature inside the quorum prefix is invalid": {
			corrupt:    0,
			wantCached: false,
		},
		"signature past the quorum prefix is invalid": {
			// The sequential verifier short-circuits at the quorum and never
			// looks at this one, so the message is still valid.
			corrupt:    numPreverifyValidators - 1,
			wantCached: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newPreverifyFixture(t)
			if tc.corrupt >= 0 {
				f.msg.ValidatorSignatures[tc.corrupt] = make([]byte, 64)
				f.refreshCertKey(t)
			}

			// The sequential verifier is the reference: the pre-pass must
			// record the certificate exactly when that verifier accepts.
			reference := f.withFreshCache(t)
			require.Equal(t, tc.wantCached, reference.keeper.ValidatePayForFibreSignatures(reference.ctx, reference.msg) == nil)

			prepass := f.withFreshCache(t)
			prepass.keeper.PreverifySignatures(prepass.ctx, [][]byte{prepass.txBytes(t)}, keeper.PreverifyOptions{Certificates: true})

			if !tc.wantCached {
				require.Zero(t, prepass.cache.Len(),
					"a rejected message must buy no cache entry, its promise included")
				return
			}
			require.True(t, prepass.cache.Has(prepass.certKey), "the certificate must be recorded")
			require.True(t, prepass.cache.Has(prepass.promiseKey), "the promise must be recorded")
			require.Equal(t, 2, prepass.cache.Len(), "exactly the promise and the certificate")
		})
	}
}

// TestPreverifySignaturesSkipsCertificatesWhenNotRequested covers FinalizeBlock,
// which never verifies validator certificates and must not pay for them.
func TestPreverifySignaturesSkipsCertificatesWhenNotRequested(t *testing.T) {
	f := newPreverifyFixture(t)

	f.keeper.PreverifySignatures(f.ctx, [][]byte{f.txBytes(t)}, keeper.PreverifyOptions{})

	require.False(t, f.cache.Has(f.certKey), "certificate must not be recorded")
	// The promise signature is still warmed: the message server checks it in
	// every phase, FinalizeBlock included.
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&f.msg.PaymentPromise))
	require.NoError(t, f.keeper.ValidatePromiseStateless(&pp))
	require.Equal(t, 1, f.cache.Len())
	// The one entry is the promise's, under the key the keeper itself derives.
	require.True(t, f.cache.Has(f.promiseKey))
}

// TestPreverifySignaturesIsSafeToSkip pins that the pre-pass changes no
// outcome: the sequential verifier reaches the same verdict either way.
func TestPreverifySignaturesIsSafeToSkip(t *testing.T) {
	for _, corrupt := range []int{-1, 0, numPreverifyValidators - 1} {
		f := newPreverifyFixture(t)
		if corrupt >= 0 {
			f.msg.ValidatorSignatures[corrupt] = make([]byte, 64)
			f.refreshCertKey(t)
		}

		withPrepass := f.withFreshCache(t)
		withPrepass.keeper.PreverifySignatures(withPrepass.ctx, [][]byte{withPrepass.txBytes(t)},
			keeper.PreverifyOptions{Certificates: true, StopOnFirstFailure: true})
		gotWith := withPrepass.keeper.ValidatePayForFibreSignatures(withPrepass.ctx, withPrepass.msg)

		without := f.withFreshCache(t)
		gotWithout := without.keeper.ValidatePayForFibreSignatures(without.ctx, without.msg)

		require.Equal(t, gotWithout == nil, gotWith == nil, "corrupt index %d", corrupt)
	}
}

// batchBoundaryValidators makes the 2/3 quorum prefix 134 signatures, so a
// certificate is split across a full 128-signature batch and a short one.
const batchBoundaryValidators = 200

// newMessage returns a valid message for the same validator set, set apart
// from the fixture's by adding delta to its blob size, so one call can carry
// several distinct promises and certificates.
func (f *preverifyFixture) newMessage(t *testing.T, delta uint32) *types.MsgPayForFibre {
	t.Helper()

	promise := f.msg.PaymentPromise
	promise.BlobSize = f.msg.PaymentPromise.BlobSize + delta

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, err = f.signerPrivKey.Sign(signBytes)
	require.NoError(t, err)

	signatures := make([][]byte, len(f.valPrivKeys))
	for i, valPrivKey := range f.valPrivKeys {
		signatures[i], err = valPrivKey.Sign(signBytes)
		require.NoError(t, err)
	}
	return &types.MsgPayForFibre{PaymentPromise: promise, ValidatorSignatures: signatures}
}

// TestPreverifySignaturesAcrossBatchBoundary covers a certificate whose quorum
// prefix spans more than one batch, including a failure in the second batch.
func TestPreverifySignaturesAcrossBatchBoundary(t *testing.T) {
	tests := map[string]struct {
		corrupt    int // index of the validator signature to corrupt, -1 for none
		wantCached bool
	}{
		"all signatures valid":           {corrupt: -1, wantCached: true},
		"failure in the first batch":     {corrupt: 0, wantCached: false},
		"failure on the batch boundary":  {corrupt: 127, wantCached: false},
		"failure in the second batch":    {corrupt: 129, wantCached: false},
		"failure past the quorum prefix": {corrupt: batchBoundaryValidators - 1, wantCached: true},
	}

	for name, tc := range tests {
		for _, stopOnFirstFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stop=%t", name, stopOnFirstFailure), func(t *testing.T) {
				f := newPreverifyFixtureWithValidators(t, batchBoundaryValidators)
				if tc.corrupt >= 0 {
					f.msg.ValidatorSignatures[tc.corrupt] = make([]byte, 64)
					f.refreshCertKey(t)
				}

				reference := f.withFreshCache(t)
				require.Equal(t, tc.wantCached,
					reference.keeper.ValidatePayForFibreSignatures(reference.ctx, reference.msg) == nil)

				prepass := f.withFreshCache(t)
				prepass.keeper.PreverifySignatures(prepass.ctx, [][]byte{prepass.txBytes(t)},
					keeper.PreverifyOptions{Certificates: true, StopOnFirstFailure: stopOnFirstFailure})

				require.Equal(t, tc.wantCached, prepass.cache.Has(prepass.certKey))
			})
		}
	}
}

// TestPreverifySignaturesRecordsEachTransaction covers an accepted, a forged and a
// bailed-out message in one batch, on one worker so they share a chunk.
func TestPreverifySignaturesRecordsEachTransaction(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	f := newPreverifyFixtureWithValidators(t, batchBoundaryValidators)
	accepted := f.newMessage(t, 1)
	acceptedKey, err := accepted.SigCacheKey()
	require.NoError(t, err)

	// The pass queues this one's items and then fails one of them.
	forged := f.newMessage(t, 2)
	forged.ValidatorSignatures[129] = make([]byte, 64)
	forgedKey, err := forged.SigCacheKey()
	require.NoError(t, err)

	// The pass bails on this one before queueing any certificate item, so it
	// is the message whose promise item has to be rolled back.
	bailed := f.newMessage(t, 3)
	bailed.ValidatorSignatures = nil
	bailedKey, err := bailed.SigCacheKey()
	require.NoError(t, err)

	f.keeper.PreverifySignatures(f.ctx, [][]byte{
		f.txBytesFor(t, accepted), f.txBytesFor(t, forged), f.txBytesFor(t, bailed),
	}, keeper.PreverifyOptions{Certificates: true})

	require.False(t, f.cache.Has(acceptedKey),
		"a chunk is all or nothing, so the valid certificate in it is not recorded either")
	require.False(t, f.cache.Has(promiseSigCacheKey(t, accepted)), "nor the promise it carried")
	require.False(t, f.cache.Has(forgedKey), "the forged certificate must not be recorded")
	require.False(t, f.cache.Has(promiseSigCacheKey(t, forged)), "nor the promise it carried")
	require.False(t, f.cache.Has(bailedKey), "the bailed-out certificate must not be recorded")
	require.False(t, f.cache.Has(promiseSigCacheKey(t, bailed)), "nor the promise it carried")
	require.Zero(t, f.cache.Len(), "the whole chunk went unrecorded")
	// Losing a cache entry costs work, never correctness: the ordered verifier
	// still accepts the valid message.
	require.NoError(t, f.keeper.ValidatePayForFibreSignatures(f.ctx, accepted))
}

// TestPreverifySignaturesReusesVerifierAfterFailure checks a failing chunk does not
// break the shared batch verifier for the next; one worker keeps both on it.
func TestPreverifySignaturesReusesVerifierAfterFailure(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	f := newPreverifyFixtureWithValidators(t, batchBoundaryValidators)
	f.msg.ValidatorSignatures[129] = make([]byte, 64)
	f.refreshCertKey(t)

	// Each certificate makes two batch items at this validator count, so five
	// transactions produce more items than one chunk holds.
	messages := make([]*types.MsgPayForFibre, 0, 5)
	messages = append(messages, f.msg)
	for i := range 4 {
		messages = append(messages, f.newMessage(t, uint32(i)+1))
	}

	txs := make([][]byte, 0, len(messages))
	for _, msg := range messages {
		txs = append(txs, f.txBytesFor(t, msg))
	}
	f.keeper.PreverifySignatures(f.ctx, txs, keeper.PreverifyOptions{Certificates: true})

	require.False(t, f.cache.Has(f.certKey), "the invalid certificate must not be recorded")
	recorded := 0
	for _, msg := range messages[1:] {
		key, err := msg.SigCacheKey()
		require.NoError(t, err)
		if f.cache.Has(key) {
			recorded++
		}
	}
	require.Positive(t, recorded, "a chunk after the failed one must still record its certificates")
}

// TestPreverifySignaturesRecordsNothingForRejectedMessages covers the shapes in
// which a message the sequential verifier rejects can reach the pass. The
// promise is valid in every one, so a pass that gated the promise on nothing
// but its own check would warm it - and the node would be paying cache space
// for traffic it is about to throw away.
func TestPreverifySignaturesRecordsNothingForRejectedMessages(t *testing.T) {
	tests := map[string]func(f *preverifyFixture){
		"forged signature inside the quorum prefix": func(f *preverifyFixture) {
			f.msg.ValidatorSignatures[0] = make([]byte, 64)
		},
		"no validator signatures": func(f *preverifyFixture) {
			f.msg.ValidatorSignatures = nil
		},
		"height with no historical info": func(f *preverifyFixture) {
			f.stakingKeeper.GetHistoricalInfoFn = func(context.Context, int64) (stakingtypes.HistoricalInfo, error) {
				return stakingtypes.HistoricalInfo{}, errors.New("no historical info at height")
			}
		},
		"more signatures than validators": func(f *preverifyFixture) {
			f.msg.ValidatorSignatures = append(f.msg.ValidatorSignatures, make([]byte, 64))
		},
	}

	for name, reject := range tests {
		t.Run(name, func(t *testing.T) {
			f := newPreverifyFixture(t)
			reject(f)
			f.refreshCertKey(t)

			// Assert the rejection first, so the case cannot go vacuous: a
			// shape the sequential verifier happens to accept would make the
			// empty cache below prove nothing.
			reference := f.withFreshCache(t)
			require.Error(t, reference.keeper.ValidatePayForFibreSignatures(reference.ctx, reference.msg))

			prepass := f.withFreshCache(t)
			prepass.keeper.PreverifySignatures(prepass.ctx, [][]byte{prepass.txBytes(t)},
				keeper.PreverifyOptions{Certificates: true})

			require.Zero(t, prepass.cache.Len(), "a rejected message must buy no cache entry")
		})
	}
}

// TestPreverifySignaturesWarmsPromiseWhenCertificateCached pins the branch
// where the certificate is already settled and the promise is all that is left
// to warm. Gating the promise on a certificate this pass no longer checks
// would be an over-correction that stops the cache warming.
func TestPreverifySignaturesWarmsPromiseWhenCertificateCached(t *testing.T) {
	f := newPreverifyFixture(t)
	f.cache.Add(f.certKey)

	f.keeper.PreverifySignatures(f.ctx, [][]byte{f.txBytes(t)}, keeper.PreverifyOptions{Certificates: true})

	require.True(t, f.cache.Has(f.promiseKey), "the promise must still be recorded")
	require.Equal(t, 2, f.cache.Len(), "the seeded certificate and the promise")
}

// TestPreverifySignaturesRecordsCertificateWhenPromiseCached pins the opposite
// edge: with the promise already cached the message's span starts at its first
// certificate item, and the group must still cover exactly that span. A
// rollback that over-truncated, or a span computed from a promise item that
// was never queued, would show up here.
func TestPreverifySignaturesRecordsCertificateWhenPromiseCached(t *testing.T) {
	f := newPreverifyFixtureWithValidators(t, batchBoundaryValidators)
	f.cache.Add(f.promiseKey)

	f.keeper.PreverifySignatures(f.ctx, [][]byte{f.txBytes(t)}, keeper.PreverifyOptions{Certificates: true})

	require.True(t, f.cache.Has(f.certKey), "the certificate must still be recorded")
	require.Equal(t, 2, f.cache.Len(), "the seeded promise and the certificate")
}

// TestPreverifySignaturesLeavesCachedCertificateAlone covers a second pre-pass
// over a certificate already recorded by the first.
func TestPreverifySignaturesLeavesCachedCertificateAlone(t *testing.T) {
	f := newPreverifyFixtureWithValidators(t, batchBoundaryValidators)
	txs := [][]byte{f.txBytes(t)}

	f.keeper.PreverifySignatures(f.ctx, txs, keeper.PreverifyOptions{Certificates: true})
	require.True(t, f.cache.Has(f.certKey))
	before := f.cache.Len()

	f.keeper.PreverifySignatures(f.ctx, txs, keeper.PreverifyOptions{Certificates: true})
	require.True(t, f.cache.Has(f.certKey))
	require.Equal(t, before, f.cache.Len(), "a repeat pre-pass must not add entries")
	require.NoError(t, f.keeper.ValidatePayForFibreSignatures(f.ctx, f.msg))
}

// TestPreverifyDeduplicatesRepeatedCertificates checks a repeated certificate
// costs one verification and leaves the cache exactly as a single copy would.
func TestPreverifyDeduplicatesRepeatedCertificates(t *testing.T) {
	const repeats = 8

	collect := func(n int) *preverifyFixture {
		f := newPreverifyFixtureWithValidators(t, batchBoundaryValidators)
		decoded := make([]*types.DecodedPayForFibre, n)
		for i := range decoded {
			decoded[i] = f.decodedMessage(t, f.msg)
		}
		f.keeper.PreverifyDecoded(f.ctx, decoded, keeper.PreverifyOptions{Certificates: true})
		return f
	}

	single := collect(1)
	require.True(t, single.cache.Has(single.certKey))

	deduped := collect(repeats)
	require.True(t, deduped.cache.Has(deduped.certKey))
	require.Equal(t, single.cache.Len(), deduped.cache.Len(),
		"deduplication must not change what ends up cached")

	// Skipping a repeat drops its promise too, which is safe: the sequential loop still
	// decides every check the pass leaves uncached.
	require.NoError(t, deduped.keeper.ValidatePayForFibreSignatures(deduped.ctx, deduped.msg),
		"and the sequential path still accepts it")
}

// TestDecodePayForFibre checks the decoded view carries the same values the
// verification path derives itself, and is nil for anything else.
func TestDecodePayForFibre(t *testing.T) {
	f := newPreverifyFixture(t)
	// The system blob carries the signer, which the fixture leaves unset.
	f.msg.Signer = sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()
	raw := f.txBytes(t)

	d := keeper.DecodePayForFibre(raw)
	require.NotNil(t, d)
	require.Equal(t, raw, d.Raw)
	require.Equal(t, f.msg.PaymentPromise.String(), d.Msg.PaymentPromise.String())
	require.Equal(t, f.msg.ValidatorSignatures, d.Msg.ValidatorSignatures)
	require.True(t, d.CertKeyed)
	require.Equal(t, f.certKey, d.CertKey)

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&f.msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	require.True(t, d.PromiseKeyed())
	require.Equal(t, signBytes, d.PromiseSignBytes)

	fibreTx, err := d.FibreTx()
	require.NoError(t, err)
	require.Equal(t, raw, fibreTx.Tx)
	expected, isFibreTx, err := types.TryParseFibreTx(raw)
	require.NoError(t, err)
	require.True(t, isFibreTx)
	require.Equal(t, expected.SystemBlob, fibreTx.SystemBlob)

	require.Nil(t, keeper.DecodePayForFibre([]byte("not a transaction")))
	require.Nil(t, keeper.DecodePayForFibre(raw[:len(raw)/2]))
}

// TestPreverifyDecodedMatchesPreverifySignatures checks the decoded entry point
// records the same cache entries as the byte-level one.
func TestPreverifyDecodedMatchesPreverifySignatures(t *testing.T) {
	f := newPreverifyFixture(t)
	raw := f.txBytes(t)
	opts := keeper.PreverifyOptions{Certificates: true}

	f.keeper.PreverifySignatures(f.ctx, [][]byte{raw}, opts)
	require.True(t, f.cache.Has(f.certKey))

	fresh := f.withFreshCache(t)
	d := keeper.DecodePayForFibre(raw)
	fresh.keeper.PreverifyDecoded(fresh.ctx, []*types.DecodedPayForFibre{nil, d}, opts)
	require.True(t, fresh.cache.Has(f.certKey))
	require.True(t, fresh.cache.Has(d.PromiseKey))
	require.Equal(t, f.cache.Len(), fresh.cache.Len())
}

// TestPreverifyCapsAtCandidateBound pins the backstop: the pass covers at most
// MaxPayForFibreCandidates messages however many the caller hands it.
func TestPreverifyCapsAtCandidateBound(t *testing.T) {
	f := newPreverifyFixture(t)
	limit := appconsts.MaxPayForFibreCandidates

	decoded := make([]*types.DecodedPayForFibre, 0, limit+1)
	for i := range limit + 1 {
		decoded = append(decoded, f.decodedMessage(t, f.signedMessage(t, i)))
	}

	f.keeper.PreverifyDecoded(f.ctx, decoded, keeper.PreverifyOptions{Certificates: true})

	// The pass walks its input in order, so the bound falls in a fixed place.
	for i, d := range decoded[:limit] {
		require.True(t, f.cache.Has(d.CertKey), "message %d is within the bound and should be covered", i)
	}
	require.False(t, f.cache.Has(decoded[limit].CertKey), "the message past the bound must not be covered")

	// Being left out of the pass changes nothing: the sequential verifier still
	// accepts it, which is the only thing that decides.
	require.NoError(t, f.keeper.ValidatePayForFibreSignatures(f.ctx, decoded[limit].Msg))
}
