package keeper_test

import (
	"bytes"
	"encoding/binary"
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

// refreshCertKey re-derives the certificate key after the message was mutated.
func (f *preverifyFixture) refreshCertKey(t *testing.T) {
	t.Helper()

	certKey, err := f.msg.SigCacheKey()
	require.NoError(t, err)
	f.certKey = certKey
}

func newPreverifyFixture(t *testing.T) *preverifyFixture {
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

	valPrivKeys := make([]ed25519.PrivKey, numPreverifyValidators)
	valset := make([]stakingtypes.Validator, numPreverifyValidators)
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

	signatures := make([][]byte, numPreverifyValidators)
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

// decodedMessage encodes msg into a transaction declaring gasLimit and decodes
// it the way every pre-verification caller does.
func (f *preverifyFixture) decodedMessage(t *testing.T, msg *types.MsgPayForFibre, gasLimit uint64) *types.DecodedPayForFibre {
	t.Helper()
	d := keeper.DecodePayForFibre(txBytesWithGas(t, msg, gasLimit))
	require.NotNil(t, d)
	return d
}

// txBytes wraps the fixture's message in the minimal Cosmos tx encoding that
// ParsePayForFibreMsg reads.
func (f *preverifyFixture) txBytes(t *testing.T) []byte {
	t.Helper()
	return txBytesWithGas(t, f.msg, types.EstimateGasForPayForFibreSignatureVerification(uint64(len(f.msg.ValidatorSignatures))))
}

// txBytesWithGas encodes msg in a tx declaring gasLimit. The pre-pass skips a
// transaction that cannot pay for its own signature checks, so the declared
// limit is part of what a fixture has to get right.
func txBytesWithGas(t *testing.T, msg *types.MsgPayForFibre, gasLimit uint64) []byte {
	t.Helper()

	value, err := msg.Marshal()
	require.NoError(t, err)
	body, err := (&cosmostx.TxBody{Messages: []*codectypes.Any{{
		TypeUrl: types.MsgPayForFibreTypeURL,
		Value:   value,
	}}}).Marshal()
	require.NoError(t, err)
	authInfo, err := (&cosmostx.AuthInfo{Fee: &cosmostx.Fee{GasLimit: gasLimit}}).Marshal()
	require.NoError(t, err)
	raw, err := (&cosmostx.TxRaw{BodyBytes: body, AuthInfoBytes: authInfo}).Marshal()
	require.NoError(t, err)
	return raw
}

// TestPreverifySignaturesRecordsOnlyValidCertificates pins the property the
// whole pre-pass rests on: it records a certificate exactly when the sequential
// verifier would accept it, and records nothing otherwise, so the sequential
// verifier stays the one that decides.
func TestPreverifySignaturesRecordsOnlyValidCertificates(t *testing.T) {
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

			require.Equal(t, tc.wantCached, prepass.cache.Has(prepass.certKey))
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

// TestDecodePayForFibre checks the decoded view carries exactly the values the
// verification path derives for itself, and is nil for anything else.
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
	require.True(t, d.PromiseKeyed)
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

// TestValidatePayForFibreSignaturesWithDecodedContext checks that a context
// carrying the decoded view reaches the same verdict and cache state as one
// without it.
func TestValidatePayForFibreSignaturesWithDecodedContext(t *testing.T) {
	f := newPreverifyFixture(t)
	raw := f.txBytes(t)
	d := keeper.DecodePayForFibre(raw)

	plain := f.withFreshCache(t)
	require.NoError(t, plain.keeper.ValidatePayForFibreSignatures(plain.ctx, f.msg))

	decoded := f.withFreshCache(t)
	ctx := types.WithDecodedPayForFibre(decoded.ctx.WithTxBytes(raw), d)
	require.NoError(t, decoded.keeper.ValidatePayForFibreSignatures(ctx, f.msg))

	// Both paths reach the same verdict and leave the promise verified. The
	// CheckTx path additionally warms the certificate, because it runs the
	// parallel pre-pass over the transaction once admission has let it through.
	require.True(t, plain.cache.Has(d.PromiseKey))
	require.True(t, decoded.cache.Has(d.PromiseKey))
	require.True(t, decoded.cache.Has(d.CertKey))
	require.False(t, plain.cache.Has(d.CertKey))

	// A tampered promise signature fails on both paths.
	bad := *f.msg
	bad.PaymentPromise.Signature = append([]byte{}, bad.PaymentPromise.Signature...)
	bad.PaymentPromise.Signature[0] ^= 0xff
	badRaw := (&preverifyFixture{msg: &bad}).txBytes(t)
	badDecoded := keeper.DecodePayForFibre(badRaw)
	badCtx := types.WithDecodedPayForFibre(f.withFreshCache(t).ctx.WithTxBytes(badRaw), badDecoded)
	require.Error(t, f.withFreshCache(t).keeper.ValidatePayForFibreSignatures(badCtx, &bad))
	require.Error(t, f.withFreshCache(t).keeper.ValidatePayForFibreSignatures(f.withFreshCache(t).ctx, &bad))
}

// TestPreverifyCapsAtVersionedLimit pins the bound the pass runs under: it
// covers at most as many messages as a block may carry, however many the caller
// hands it. PrepareProposal is handed everything the mempool holds within block
// max bytes, which is far more than any block can include.
func TestPreverifyCapsAtVersionedLimit(t *testing.T) {
	f := newPreverifyFixture(t)
	limit := appconsts.GetMaxPayForFibreMessages(f.ctx.ConsensusParams().Version.GetApp())
	gas := types.EstimateGasForPayForFibreSignatureVerification(numPreverifyValidators)

	decoded := make([]*types.DecodedPayForFibre, 0, limit+1)
	for i := range limit + 1 {
		decoded = append(decoded, f.decodedMessage(t, f.signedMessage(t, i), gas))
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

// TestPreverifySkipsUnderfundedGas checks the pass does no signature work for a
// transaction that did not declare enough gas to pay for it. The ante handler
// is still the authority: skipping only leaves the check uncached.
func TestPreverifySkipsUnderfundedGas(t *testing.T) {
	enough := types.EstimateGasForPayForFibreSignatureVerification(numPreverifyValidators)

	f := newPreverifyFixture(t)

	funded := f.withFreshCache(t)
	d := funded.decodedMessage(t, funded.msg, enough)
	funded.keeper.PreverifyDecoded(funded.ctx, []*types.DecodedPayForFibre{d}, keeper.PreverifyOptions{Certificates: true})
	require.True(t, funded.cache.Has(d.CertKey))
	require.True(t, funded.cache.Has(d.PromiseKey))

	underfunded := f.withFreshCache(t)
	short := underfunded.decodedMessage(t, underfunded.msg, enough-1)
	underfunded.keeper.PreverifyDecoded(underfunded.ctx, []*types.DecodedPayForFibre{short}, keeper.PreverifyOptions{Certificates: true})
	require.Equal(t, 0, underfunded.cache.Len(), "an underfunded tx must cost the pass no signature work")

	// The verdict is unchanged: the sequential verifier still accepts it.
	require.NoError(t, underfunded.keeper.ValidatePayForFibreSignatures(underfunded.ctx, underfunded.msg))
}
