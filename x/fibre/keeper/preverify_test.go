package keeper_test

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/fibre"
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
	}
}

// txBytes wraps the fixture's message in the minimal Cosmos tx encoding that
// ParsePayForFibreMsg reads.
func (f *preverifyFixture) txBytes(t *testing.T) []byte {
	t.Helper()

	value, err := f.msg.Marshal()
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
