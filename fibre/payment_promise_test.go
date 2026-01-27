package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

// makePaymentPromise creates a test [fibre.PaymentPromise] with the given private key.
func makePaymentPromise(t *testing.T, privKey *secp256k1.PrivKey) *fibre.PaymentPromise {
	t.Helper()
	return &fibre.PaymentPromise{
		ChainID:     "test-chain-1",
		Height:      12345,
		Namespace:   share.MustNewV0Namespace([]byte("test")),
		UploadSize:  1024,
		BlobVersion: 0,
		Commitment: fibre.Commitment{
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
			17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
		},
		CreationTimestamp: time.Now().UTC().Truncate(time.Second),
		SignerKey:         privKey.PubKey().(*secp256k1.PubKey),
	}
}

func TestPaymentPromise(t *testing.T) {
	t.Run("MarshalUnmarshalBinary", func(t *testing.T) {
		privKey := secp256k1.GenPrivKey()
		original := makePaymentPromise(t, privKey)

		// Sign with a dummy hash for testing
		sig, err := privKey.Sign(make([]byte, 32))
		require.NoError(t, err)
		original.Signature = sig

		// Marshal
		data, err := original.MarshalBinary()
		require.NoError(t, err)

		// Unmarshal
		var decoded fibre.PaymentPromise
		require.NoError(t, decoded.UnmarshalBinary(data))

		// Verify all fields match
		require.Equal(t, original.SignerKey, decoded.SignerKey)
		require.Equal(t, original.ChainID, decoded.ChainID)
		require.Equal(t, original.Namespace, decoded.Namespace)
		require.Equal(t, original.UploadSize, decoded.UploadSize)
		require.Equal(t, original.Commitment, decoded.Commitment)
		require.Equal(t, original.BlobVersion, decoded.BlobVersion)
		require.True(t, decoded.CreationTimestamp.Equal(original.CreationTimestamp))
		require.Equal(t, original.Signature, decoded.Signature)
		require.Equal(t, original.Height, decoded.Height)

		// Verify roundtrip produces identical bytes
		data2, err := decoded.MarshalBinary()
		require.NoError(t, err)
		require.Equal(t, data, data2)
	})

	t.Run("SigningRoundtrip", func(t *testing.T) {
		privKey := secp256k1.GenPrivKey()
		pp := makePaymentPromise(t, privKey)

		// Verify key matches
		require.Equal(t, privKey.PubKey().(*secp256k1.PubKey), pp.SignerKey)

		// Sign the payment promise
		signBytes, err := pp.SignBytes()
		require.NoError(t, err)

		signature, err := privKey.Sign(signBytes)
		require.NoError(t, err)
		pp.Signature = signature

		// Marshal and unmarshal
		data, err := pp.MarshalBinary()
		require.NoError(t, err)

		var decoded fibre.PaymentPromise
		require.NoError(t, decoded.UnmarshalBinary(data))

		// Validate the decoded promise
		require.NoError(t, decoded.Validate())

		// Verify signature verification works on decoded promise
		decodedSignBytes, err := decoded.SignBytes()
		require.NoError(t, err)
		require.Equal(t, signBytes, decodedSignBytes)
		require.True(t, decoded.SignerKey.VerifySignature(decodedSignBytes, decoded.Signature))
	})

	t.Run("Validate", func(t *testing.T) {
		privKey := secp256k1.GenPrivKey()
		pp := makePaymentPromise(t, privKey)

		// Sign the payment promise
		signBytes, err := pp.SignBytes()
		require.NoError(t, err)
		signature, err := privKey.Sign(signBytes)
		require.NoError(t, err)
		pp.Signature = signature

		// Valid promise should pass validation
		require.NoError(t, pp.Validate())

		t.Run("InvalidSignature", func(t *testing.T) {
			invalidPP := makePaymentPromise(t, privKey)
			invalidPP.Signature = make([]byte, 64) // Invalid signature
			require.EqualError(t, invalidPP.Validate(), "signature verification failed")
		})

		t.Run("WrongKey", func(t *testing.T) {
			wrongPrivKey := secp256k1.GenPrivKey()
			wrongKeyPP := makePaymentPromise(t, wrongPrivKey)
			wrongKeyPP.Signature = signature // Using signature from different key
			require.EqualError(t, wrongKeyPP.Validate(), "signature verification failed")

			// Sign with correct key should work
			wrongSignBytes, err := wrongKeyPP.SignBytes()
			require.NoError(t, err)
			wrongSignature, err := wrongPrivKey.Sign(wrongSignBytes)
			require.NoError(t, err)
			wrongKeyPP.Signature = wrongSignature
			require.NoError(t, wrongKeyPP.Validate())
		})
	})
}

func TestValidatorSignPaymentPromise(t *testing.T) {
	signerPrivKey := secp256k1.GenPrivKey()
	pp := makePaymentPromise(t, signerPrivKey)

	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	signature, err := signerPrivKey.Sign(signBytes)
	require.NoError(t, err)
	pp.Signature = signature

	privVal := core.NewMockPV()
	valSignature, err := fibre.SignPaymentPromiseValidator(pp, privVal)
	require.NoError(t, err)
	require.Len(t, valSignature, ed25519.SignatureSize)

	validatorSignBytes, err := pp.SignBytesValidator()
	require.NoError(t, err)
	require.True(t, privVal.PrivKey.PubKey().VerifySignature(validatorSignBytes, valSignature))
}
