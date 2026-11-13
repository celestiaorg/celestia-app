package fibre_test

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

const maxFuzzPromiseBytes = 4096

var (
	fuzzPromiseKey      = secp256k1.GenPrivKeyFromSecret([]byte("fibre payment promise fuzz"))
	fuzzPromiseOtherKey = secp256k1.GenPrivKeyFromSecret([]byte("fibre payment promise fuzz other"))
)

// copyPromiseFields returns a promise with p's fields and empty sign bytes and
// hash caches.
func copyPromiseFields(p *fibre.PaymentPromise) *fibre.PaymentPromise {
	return &fibre.PaymentPromise{
		SignerKey:         p.SignerKey,
		ChainID:           p.ChainID,
		Namespace:         p.Namespace,
		UploadSize:        p.UploadSize,
		BlobVersion:       p.BlobVersion,
		Commitment:        p.Commitment,
		CreationTimestamp: p.CreationTimestamp,
		Signature:         p.Signature,
		Height:            p.Height,
	}
}

// requirePromiseRoundTrip requires p to encode, decode to the same signed
// content, and re-encode to the same bytes.
func requirePromiseRoundTrip(t *testing.T, p *fibre.PaymentPromise) {
	t.Helper()
	encoded, err := p.MarshalBinary()
	require.NoError(t, err)
	var decoded fibre.PaymentPromise
	require.NoError(t, decoded.UnmarshalBinary(encoded))
	reencoded, err := decoded.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, encoded, reencoded)

	want, wantErr := copyPromiseFields(p).SignBytes()
	got, gotErr := decoded.SignBytes()
	require.Equal(t, wantErr == nil, gotErr == nil)
	require.Equal(t, want, got)
	require.True(t, bytes.Equal(p.Signature, decoded.Signature))
}

// FuzzPaymentPromiseUnmarshal decodes payment promise bytes as a fibre server
// receives them from clients. Decoding, validation, sign bytes and hashing must
// not panic, and accepted promises must round trip.
func FuzzPaymentPromiseUnmarshal(f *testing.F) {
	promise := &fibre.PaymentPromise{
		SignerKey:         fuzzPromiseKey.PubKey().(*secp256k1.PubKey),
		ChainID:           "fuzz-chain",
		Namespace:         testNamespace,
		UploadSize:        1024,
		Commitment:        fibre.Commitment(bytes.Repeat([]byte{7}, 32)),
		CreationTimestamp: time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC),
		Height:            100,
	}
	signBytes, err := promise.SignBytes()
	require.NoError(f, err)
	promise.Signature, err = fuzzPromiseKey.Sign(signBytes)
	require.NoError(f, err)
	require.NoError(f, promise.Validate())
	valid, err := promise.MarshalBinary()
	require.NoError(f, err)
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	promise.Height = math.MaxUint64
	negativeHeight, err := copyPromiseFields(promise).MarshalBinary()
	require.NoError(f, err)
	f.Add(negativeHeight)
	promise.UploadSize, promise.BlobVersion = math.MaxUint32, math.MaxUint32
	promise.CreationTimestamp = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	extremes, err := copyPromiseFields(promise).MarshalBinary()
	require.NoError(f, err)
	f.Add(extremes)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzPromiseBytes {
			return
		}
		pb := &types.PaymentPromise{}
		pbErr := pb.Unmarshal(data)
		if pbErr == nil {
			_ = pb.ValidateBasic()
		}

		var p fibre.PaymentPromise
		if err := p.UnmarshalBinary(data); err != nil {
			return
		}
		require.NoError(t, pbErr)
		_ = p.Validate()
		_, _ = p.Hash()
		requirePromiseRoundTrip(t, &p)

		// The decoded promise must keep every field of the wire message.
		want, err := pb.Marshal()
		require.NoError(t, err)
		got, err := p.MarshalBinary()
		require.NoError(t, err)
		require.Equal(t, want, got, "decoding changed a payment promise field")
	})
}

// FuzzPaymentPromiseSignBytes builds and signs payment promises from fuzzed
// fields, including height and timestamp extremes. A correctly signed promise
// must pass Validate exactly when its fields are valid, must round trip, must
// pass ValidateBasic where Validate and the server's extra checks pass, and
// changing any signed field must change the sign bytes.
func FuzzPaymentPromiseSignBytes(f *testing.F) {
	ns := testNamespace.Bytes()
	commitment := bytes.Repeat([]byte{7}, 32)
	f.Add("fuzz-chain", ns, uint32(1024), uint32(0), commitment, int64(1767225600), uint32(5), uint64(100), uint8(0))
	f.Add("fuzz-chain", ns, uint32(1), uint32(1), commitment, int64(-62135596800), uint32(0), uint64(math.MaxInt64)+1, uint8(6))
	f.Add("", ns, uint32(0), uint32(0), commitment, int64(math.MaxInt64), uint32(999999999), uint64(0), uint8(7))
	f.Add("c", share.TxNamespace.Bytes(), uint32(math.MaxUint32), uint32(0), commitment, int64(math.MinInt64), uint32(1), uint64(math.MaxUint64), uint8(8))

	f.Fuzz(func(t *testing.T, chainID string, nsBytes []byte, uploadSize, blobVersion uint32, commitment []byte, sec int64, nsec uint32, height uint64, field uint8) {
		if len(chainID) > 2*types.MaxChainIDSize {
			return
		}
		namespace, err := share.NewNamespaceFromBytes(nsBytes)
		if err != nil {
			return
		}
		nsec %= uint32(time.Second)
		p := &fibre.PaymentPromise{
			SignerKey:         fuzzPromiseKey.PubKey().(*secp256k1.PubKey),
			ChainID:           chainID,
			Namespace:         namespace,
			UploadSize:        uploadSize,
			BlobVersion:       blobVersion,
			CreationTimestamp: time.Unix(sec, int64(nsec)).UTC(),
			Height:            height,
		}
		copy(p.Commitment[:], commitment)

		signBytes, err := p.SignBytes()
		if err != nil {
			require.Empty(t, chainID, "sign bytes may only fail for an empty chain ID: %v", err)
			return
		}
		p.Signature, err = fuzzPromiseKey.Sign(signBytes)
		require.NoError(t, err)

		valid := len(chainID) <= types.MaxChainIDSize && uploadSize > 0 && height > 0 && !p.CreationTimestamp.IsZero()
		err = p.Validate()
		require.Equal(t, valid, err == nil, "Validate: %v", err)
		_, err = p.Hash()
		require.NoError(t, err)

		// Protobuf timestamps only cover years 1 through 9999.
		year := p.CreationTimestamp.Year()
		if year >= 1 && year <= 9999 {
			requirePromiseRoundTrip(t, p)
		}

		// Validate leaves the namespace, blob version and the int64 range of the
		// height to the caller: the fibre server runs ValidateBasic and
		// BlobConfigForVersion first.
		if valid && namespace.ValidateForBlob() == nil && blobVersion == types.BlobVersionZero && height <= math.MaxInt64 {
			pb, err := p.ToProto()
			require.NoError(t, err)
			require.NoError(t, pb.ValidateBasic(), "a promise that passes Validate must pass ValidateBasic")
		}

		mutated := copyPromiseFields(p)
		switch field % 9 {
		case 0:
			mutated.SignerKey = fuzzPromiseOtherKey.PubKey().(*secp256k1.PubKey)
		case 1:
			mutated.ChainID += "x"
		case 2:
			nsMutated := bytes.Clone(namespace.Bytes())
			nsMutated[len(nsMutated)-1] ^= 1
			if mutated.Namespace, err = share.NewNamespaceFromBytes(nsMutated); err != nil {
				return
			}
		case 3:
			mutated.UploadSize ^= 1
		case 4:
			mutated.BlobVersion ^= 1
		case 5:
			mutated.Commitment[field%32] ^= 1
		case 6:
			mutated.Height ^= 1
		case 7:
			mutated.CreationTimestamp = time.Unix(sec, int64(nsec^1)).UTC()
		case 8:
			mutated.Signature = bytes.Clone(p.Signature)
			mutated.Signature[0] ^= 1
			mutatedSignBytes, err := mutated.SignBytes()
			require.NoError(t, err)
			require.Equal(t, signBytes, mutatedSignBytes, "the signature is not part of the sign bytes")
			hash, err := p.Hash()
			require.NoError(t, err)
			mutatedHash, err := mutated.Hash()
			require.NoError(t, err)
			require.NotEqual(t, hash, mutatedHash, "the hash must commit to the signature")
			require.Error(t, mutated.Validate())
			return
		}
		mutatedSignBytes, err := mutated.SignBytes()
		require.NoError(t, err)
		require.NotEqual(t, signBytes, mutatedSignBytes, "changing signed field %d did not change the sign bytes", field%9)
		require.Error(t, mutated.Validate(), "the signature must not verify for a changed field %d", field%9)
	})
}
