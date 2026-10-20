package ante

import (
	"context"
	"testing"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	txsigning "cosmossdk.io/x/tx/signing"
	"cosmossdk.io/x/tx/signing/direct"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"
)

// keyInputs is everything txSigCacheKey is meant to cover. A test mutates one
// field at a time and asserts the key moves.
type keyInputs struct {
	pubKey     cryptotypes.PubKey
	signerData txsigning.SignerData
	sigData    *signing.SingleSignatureData
	txData     txsigning.TxData
}

func baselineKeyInputs() keyInputs {
	pubKey := secp256k1.GenPrivKey().PubKey()
	return keyInputs{
		pubKey: pubKey,
		signerData: txsigning.SignerData{
			Address:       "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7",
			ChainID:       "test-chain",
			AccountNumber: 7,
			Sequence:      3,
		},
		sigData: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: []byte("signature"),
		},
		txData: txsigning.TxData{
			BodyBytes:     []byte("body"),
			AuthInfoBytes: []byte("auth info"),
		},
	}
}

func (in keyInputs) key(t *testing.T) sigcache.Key {
	t.Helper()

	key, keyed := txSigCacheKey(in.pubKey, in.signerData, in.sigData, in.txData)
	require.True(t, keyed)
	return key
}

// TestTxSigCacheKeyCoversEveryInput pins the property the whole cache rests on:
// change anything the sign bytes or the verification depend on, and the key
// moves. A key that missed one of these would let a signature verified over one
// transaction stand in for another.
func TestTxSigCacheKeyCoversEveryInput(t *testing.T) {
	baseline := baselineKeyInputs()

	mutations := map[string]func(*keyInputs){
		"public key bytes": func(in *keyInputs) { in.pubKey = secp256k1.GenPrivKey().PubKey() },
		// Same bytes, different curve: the key must separate them, or an
		// ed25519 entry could satisfy a secp256k1 lookup.
		"public key type":   func(in *keyInputs) { in.pubKey = &ed25519.PubKey{Key: in.pubKey.Bytes()} },
		"signer address":    func(in *keyInputs) { in.signerData.Address = "celestia1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx0qph2w" },
		"chain id":          func(in *keyInputs) { in.signerData.ChainID = "other-chain" },
		"account number":    func(in *keyInputs) { in.signerData.AccountNumber = 8 },
		"sequence":          func(in *keyInputs) { in.signerData.Sequence = 4 },
		"sign mode":         func(in *keyInputs) { in.sigData.SignMode = signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON },
		"signature":         func(in *keyInputs) { in.sigData.Signature = []byte("other signature") },
		"body bytes":        func(in *keyInputs) { in.txData.BodyBytes = []byte("other body") },
		"auth info bytes":   func(in *keyInputs) { in.txData.AuthInfoBytes = []byte("other auth info") },
		"unknown criticals": func(in *keyInputs) { in.txData.BodyHasUnknownNonCriticals = true },
	}

	seen := map[sigcache.Key]string{baseline.key(t): "baseline"}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated := baselineKeyInputs()
			mutated.pubKey = baseline.pubKey
			mutate(&mutated)

			key := mutated.key(t)
			if previous, ok := seen[key]; ok {
				t.Fatalf("changing the %s collides with %s", name, previous)
			}
			seen[key] = name
		})
	}
}

// TestTxSigCacheKeyIsDeterministic pins the other half: the same inputs key the
// same entry, or nothing would ever hit.
func TestTxSigCacheKeyIsDeterministic(t *testing.T) {
	inputs := baselineKeyInputs()
	repeat := baselineKeyInputs()
	repeat.pubKey = inputs.pubKey

	require.Equal(t, inputs.key(t), repeat.key(t))
}

// TestTxSigCacheKeyRefusesTextual covers the one sign mode whose handler reads
// state. Its sign bytes are not a function of the inputs above and its reads are
// metered, so it must never be keyed.
func TestTxSigCacheKeyRefusesTextual(t *testing.T) {
	inputs := baselineKeyInputs()
	inputs.sigData.SignMode = signing.SignMode_SIGN_MODE_TEXTUAL

	_, keyed := txSigCacheKey(inputs.pubKey, inputs.signerData, inputs.sigData, inputs.txData)
	require.False(t, keyed)
}

// TestTxSigCacheKeyRefusesMultiAndMissingKey covers the two shapes the cache
// does not handle: each inner signature of a multisig would need its own entry,
// and a nil public key has nothing to key on.
func TestTxSigCacheKeyRefusesMultiAndMissingKey(t *testing.T) {
	inputs := baselineKeyInputs()

	_, keyed := txSigCacheKey(inputs.pubKey, inputs.signerData, &signing.MultiSignatureData{}, inputs.txData)
	require.False(t, keyed)

	_, keyed = txSigCacheKey(nil, inputs.signerData, inputs.sigData, inputs.txData)
	require.False(t, keyed)
}

// signedInputs returns inputs whose signature verifies under handler.
func signedInputs(t *testing.T, handler *txsigning.HandlerMap) keyInputs {
	t.Helper()

	privKey := secp256k1.GenPrivKey()
	inputs := baselineKeyInputs()
	inputs.pubKey = privKey.PubKey()

	signBytes, err := handler.GetSignBytes(context.Background(),
		signingv1beta1.SignMode_SIGN_MODE_DIRECT, inputs.signerData, inputs.txData)
	require.NoError(t, err)
	inputs.sigData.Signature, err = privKey.Sign(signBytes)
	require.NoError(t, err)

	return inputs
}

func (in keyInputs) verify(verifier ante.SignatureVerifier, handler *txsigning.HandlerMap) error {
	return verifier(context.Background(), in.pubKey, in.signerData, in.sigData, handler, in.txData)
}

// TestCachedVerifierRecordsOnlySuccesses pins the admission rule on the
// transaction domain: a signature that verifies is recorded, one that does not
// leaves the cache empty so the next pass reaches the same error.
func TestCachedVerifierRecordsOnlySuccesses(t *testing.T) {
	handler := txsigning.NewHandlerMap(direct.SignModeHandler{})

	t.Run("success", func(t *testing.T) {
		cache := sigcache.New(8)
		inputs := signedInputs(t, handler)

		require.NoError(t, inputs.verify(cachedVerifier(cache), handler))
		require.True(t, cache.Has(inputs.key(t)))

		// A second pass over the same signature hits and records nothing new.
		require.NoError(t, inputs.verify(cachedVerifier(cache), handler))
		require.Equal(t, 1, cache.Len())
	})

	t.Run("failure", func(t *testing.T) {
		cache := sigcache.New(8)
		inputs := signedInputs(t, handler)
		inputs.sigData.Signature = []byte("not a signature")

		require.Error(t, inputs.verify(cachedVerifier(cache), handler))
		require.Zero(t, cache.Len())
	})
}

// TestCachedVerifierHitSkipsVerification pins that a hit really does skip the
// curve operation: a signature that cannot verify is accepted once its key is
// present. This is why only successes may ever be admitted.
func TestCachedVerifierHitSkipsVerification(t *testing.T) {
	handler := txsigning.NewHandlerMap(direct.SignModeHandler{})
	cache := sigcache.New(8)
	inputs := signedInputs(t, handler)
	inputs.sigData.Signature = []byte("not a signature")
	require.Error(t, inputs.verify(cachedVerifier(cache), handler))

	cache.Add(inputs.key(t))
	require.NoError(t, inputs.verify(cachedVerifier(cache), handler))
}

// TestCachedVerifierWithoutCache pins that the cache is an optimization: a
// verifier built without one defers to the SDK on every call.
func TestCachedVerifierWithoutCache(t *testing.T) {
	handler := txsigning.NewHandlerMap(direct.SignModeHandler{})
	inputs := signedInputs(t, handler)

	require.NoError(t, inputs.verify(cachedVerifier(nil), handler))

	inputs.sigData.Signature = []byte("not a signature")
	require.Error(t, inputs.verify(cachedVerifier(nil), handler))
}

// TestCachedVerifierExecModeTolerateNonSDKContext checks reading the exec mode
// never assumes an SDK context, since unwrapping one that is absent panics.
func TestCachedVerifierExecModeTolerateNonSDKContext(t *testing.T) {
	handler := txsigning.NewHandlerMap(direct.SignModeHandler{})
	inputs := signedInputs(t, handler)

	require.NotPanics(t, func() {
		require.NoError(t, inputs.verify(cachedVerifier(sigcache.New(8)), handler))
	})
	require.False(t, inProcessProposal(context.Background()))
}

// TestCachedVerifierExecModeReadsSDKContext checks ProcessProposal is recognised
// from an SDK context, as the context or its value, and no other exec mode is.
func TestCachedVerifierExecModeReadsSDKContext(t *testing.T) {
	handler := txsigning.NewHandlerMap(direct.SignModeHandler{})
	inputs := signedInputs(t, handler)
	sdkCtx := sdk.Context{}.WithExecMode(sdk.ExecModeProcessProposal)

	require.True(t, inProcessProposal(sdkCtx))
	require.True(t, inProcessProposal(context.WithValue(context.Background(), sdk.SdkContextKey, sdkCtx)))
	require.False(t, inProcessProposal(sdk.Context{}.WithExecMode(sdk.ExecModeFinalize)))

	// The swapped key must reach the same verdict as the ordinary one.
	cache := sigcache.New(8)
	require.NoError(t, cachedVerifier(cache)(sdkCtx, inputs.pubKey, inputs.signerData, inputs.sigData, handler, inputs.txData))
}

// TestCachedVerifierDoesNotCacheUnkeyable covers an unkeyable signature
// reaching the verifier: it is verified the ordinary way and nothing is stored.
func TestCachedVerifierDoesNotCacheUnkeyable(t *testing.T) {
	handler := txsigning.NewHandlerMap(direct.SignModeHandler{})
	cache := sigcache.New(8)
	inputs := signedInputs(t, handler)
	inputs.sigData.SignMode = signing.SignMode_SIGN_MODE_TEXTUAL

	// No TEXTUAL handler is registered, so the ordinary path reports it.
	require.Error(t, inputs.verify(cachedVerifier(cache), handler))
	require.Zero(t, cache.Len())
}
