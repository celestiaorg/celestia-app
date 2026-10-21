package keeper_test

import (
	"bytes"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// countingSigCache records what the keeper asked of the cache, so a test can
// tell a hit from a re-verification.
type countingSigCache struct {
	entries map[sigcache.Key]struct{}
	lookups int
	adds    int
}

func newCountingSigCache() *countingSigCache {
	return &countingSigCache{entries: map[sigcache.Key]struct{}{}}
}

func (c *countingSigCache) Has(key sigcache.Key) bool {
	c.lookups++
	_, ok := c.entries[key]
	return ok
}

func (c *countingSigCache) Add(key sigcache.Key) {
	c.adds++
	c.entries[key] = struct{}{}
}

// newPromiseKeeper builds a keeper with the given cache. ValidatePromiseStateless
// reads no state, so the store is never mounted.
func newPromiseKeeper(sigCache keeper.SigCache) *keeper.Keeper {
	return keeper.NewKeeper(
		codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
		storetypes.NewKVStoreKey(types.StoreKey),
		&MockBankKeeper{},
		&MockStakingKeeper{},
		authtypes.NewModuleAddress("gov").String(),
		false,
		sigCache,
	)
}

// newSignedPromise returns a promise that passes Validate.
func newSignedPromise(t *testing.T) *fibre.PaymentPromise {
	t.Helper()

	signerPrivKey := secp256k1.GenPrivKey()
	promise := types.PaymentPromise{
		ChainId:           "test-chain",
		Height:            100,
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          1000,
		Commitment:        make([]byte, 32),
		CreationTimestamp: time.Now().UTC().Truncate(time.Second),
		SignerPublicKey:   *signerPrivKey.PubKey().(*secp256k1.PubKey),
		Signature:         make([]byte, 64),
	}

	pp := &fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	pp.Signature, err = signerPrivKey.Sign(signBytes)
	require.NoError(t, err)
	require.NoError(t, pp.Validate())

	return pp
}

// promiseKey re-derives the key the keeper uses, from the same three inputs:
// the signer key, the sign bytes and the signature.
func promiseKey(t *testing.T, pp *fibre.PaymentPromise) sigcache.Key {
	t.Helper()

	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	return sigcache.NewKey(sigcache.PromiseSignature, pp.SignerKey.Bytes(), signBytes, pp.Signature)
}

// protoPromiseKey is promiseKey for a promise still in its proto form.
func protoPromiseKey(t *testing.T, promise *types.PaymentPromise) sigcache.Key {
	t.Helper()

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(promise))
	return promiseKey(t, &pp)
}

// TestValidatePromiseStatelessRecordsNothing pins that a passing validation on
// its own leaves no entry: the promise signature is the submitter's own, so it
// alone says nothing about whether the message around it will be accepted.
// Recording is the caller's decision, taken behind the remaining checks (see
// TestValidatePayForFibreSignaturesRecordsPromiseBehindQuorum).
func TestValidatePromiseStatelessRecordsNothing(t *testing.T) {
	cache := newCountingSigCache()
	k := newPromiseKeeper(cache)
	pp := newSignedPromise(t)

	require.NoError(t, k.ValidatePromiseStateless(pp))
	require.NoError(t, k.ValidatePromiseStateless(pp))
	require.Equal(t, 2, cache.lookups, "every call consults the cache")
	require.Zero(t, cache.adds)
	require.Empty(t, cache.entries)
}

// TestValidatePromiseStatelessHitSkipsValidation pins that a hit really does
// skip the curve operation: a promise whose signature does not verify is
// accepted once its key is present. This is why only successful verifications
// may ever be admitted to the cache.
func TestValidatePromiseStatelessHitSkipsValidation(t *testing.T) {
	cache := newCountingSigCache()
	k := newPromiseKeeper(cache)
	pp := newSignedPromise(t)
	pp.Signature = bytes.Repeat([]byte{0x2}, len(pp.Signature))
	require.Error(t, pp.Validate(), "fixture must not verify on its own")

	cache.Add(promiseKey(t, pp))
	require.NoError(t, k.ValidatePromiseStateless(pp))
}

// TestValidatePromiseStatelessDoesNotRecordFailures pins the admission rule: a
// promise that fails validation leaves no entry, so the sequential path reaches
// the same error every time.
func TestValidatePromiseStatelessDoesNotRecordFailures(t *testing.T) {
	cache := newCountingSigCache()
	k := newPromiseKeeper(cache)
	pp := newSignedPromise(t)
	pp.Signature = bytes.Repeat([]byte{0x2}, len(pp.Signature))

	require.Error(t, k.ValidatePromiseStateless(pp))
	require.Zero(t, cache.adds)
	require.Empty(t, cache.entries)
}

// TestValidatePromiseStatelessKeyCoversSignature pins that the key covers the
// signature, so warming one promise cannot accept a different signature over
// the same bytes.
func TestValidatePromiseStatelessKeyCoversSignature(t *testing.T) {
	cache := newCountingSigCache()
	k := newPromiseKeeper(cache)
	pp := newSignedPromise(t)
	recorded := promiseKey(t, pp)
	cache.Add(recorded)

	pp.Signature = bytes.Repeat([]byte{0x3}, len(pp.Signature))
	forged := promiseKey(t, pp)
	require.NotEqual(t, recorded, forged, "the key must cover the signature")
	require.NotContains(t, cache.entries, forged)
	require.Error(t, k.ValidatePromiseStateless(pp))
}

// TestValidatePromiseStatelessUnkeyablePromise covers a promise too malformed
// to key: it is validated the ordinary way and the error stays Validate's to
// report.
func TestValidatePromiseStatelessUnkeyablePromise(t *testing.T) {
	cache := newCountingSigCache()
	k := newPromiseKeeper(cache)
	pp := newSignedPromise(t)
	pp.SignerKey = nil

	require.Error(t, k.ValidatePromiseStateless(pp))
	require.Zero(t, cache.lookups)
	require.Zero(t, cache.adds)
}

// TestValidatePromiseStatelessWithoutCache pins that the memoization is
// optional: a keeper built without a cache validates exactly as before.
func TestValidatePromiseStatelessWithoutCache(t *testing.T) {
	k := newPromiseKeeper(nil)
	pp := newSignedPromise(t)

	require.NoError(t, k.ValidatePromiseStateless(pp))
	require.NoError(t, k.ValidatePromiseStateless(pp))

	pp.Signature = bytes.Repeat([]byte{0x2}, len(pp.Signature))
	require.Error(t, k.ValidatePromiseStateless(pp))
}
