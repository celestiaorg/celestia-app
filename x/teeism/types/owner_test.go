package types_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestOnlyTheOwnerTransfersOwnership(t *testing.T) {
	ism := ismPinnedTo(identityFor(1))

	_, err := ism.TransferOwnership("celestia1someoneelse", "celestia1next")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	_, err = ism.TransferOwnership("celestia1owner", "")
	require.Error(t, err, "an ism cannot be left without an owner")
	require.Equal(t, "celestia1owner", ism.Owner, "a refused transfer changes nothing")

	previous, err := ism.TransferOwnership("celestia1owner", "celestia1next")
	require.NoError(t, err)
	require.Equal(t, "celestia1owner", previous)
	require.Equal(t, "celestia1next", ism.Owner)

	// Every owner control moves with the ism.
	_, err = ism.Repin("celestia1owner", identityFor(2))
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized, "the previous owner keeps no control")
	_, err = ism.Repin("celestia1next", identityFor(2))
	require.NoError(t, err)
	_, err = ism.SetVerifierOverrides("celestia1owner", &types.VerifierOverrides{TrustedRootCerts: [][]byte{selfSignedRoot(t)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	_, err = ism.SetVerifierOverrides("celestia1next", &types.VerifierOverrides{TrustedRootCerts: [][]byte{selfSignedRoot(t)}})
	require.NoError(t, err)
}

func TestOnlyTheOwnerSetsVerifierOverrides(t *testing.T) {
	ism := ismPinnedTo(identityFor(1))

	_, err := ism.SetVerifierOverrides("celestia1someoneelse", &types.VerifierOverrides{TrustedRootCerts: [][]byte{selfSignedRoot(t)}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Nil(t, ism.VerifierOverrides)

	set := &types.VerifierOverrides{TrustedRootCerts: [][]byte{selfSignedRoot(t)}}
	previous, err := ism.SetVerifierOverrides("celestia1owner", set)
	require.NoError(t, err)
	require.Nil(t, previous)
	require.Equal(t, set, ism.VerifierOverrides)

	// Empty overrides clear, and are not stored.
	previous, err = ism.SetVerifierOverrides("celestia1owner", &types.VerifierOverrides{})
	require.NoError(t, err)
	require.Equal(t, set, previous)
	require.Nil(t, ism.VerifierOverrides)

	_, err = ism.SetVerifierOverrides("celestia1owner", &types.VerifierOverrides{TrustedRootCerts: [][]byte{{0x30, 0x01}}})
	require.ErrorIs(t, err, types.ErrInvalidVerifierOverrides)
	require.Nil(t, ism.VerifierOverrides, "refused overrides change nothing")
}

func TestVerifierOverridesValidation(t *testing.T) {
	root := selfSignedRoot(t)

	for name, tc := range map[string]struct {
		overrides *types.VerifierOverrides
		valid     bool
	}{
		"none":                   {nil, true},
		"root":                   {&types.VerifierOverrides{TrustedRootCerts: [][]byte{root}}, true},
		"root that is not DER":   {&types.VerifierOverrides{TrustedRootCerts: [][]byte{{0x30, 0x01}}}, false},
		"too many roots":         {&types.VerifierOverrides{TrustedRootCerts: [][]byte{root, root, root, root, root}}, false},
		"root over the size cap": {&types.VerifierOverrides{TrustedRootCerts: [][]byte{make([]byte, types.MaxTrustedRootCertBytes+1)}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.overrides.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, types.ErrInvalidVerifierOverrides)
			}
		})
	}
}

// selfSignedRoot returns a DER root certificate that is not Intel's.
func selfSignedRoot(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Intel SGX Root CA"},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Unix(1<<33, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return der
}
