package types_test

import (
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// The real quote's collateral (TCB info) expires 30 days after it was captured.
const fixtureCollateralValidity = 30 * 24 * time.Hour

// intelRootFrom returns the Intel root CA the fixture's collateral chains to: the
// last certificate of the TCB info issuer chain, which Intel serves URL-encoded.
func intelRootFrom(t *testing.T, collateral *types.Collateral) []byte {
	t.Helper()
	chain, err := url.QueryUnescape(string(collateral.TcbInfoIssuerChain))
	require.NoError(t, err)
	var last []byte
	for rest := []byte(chain); ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		last = block.Bytes
	}
	require.NotNil(t, last)
	cert, err := x509.ParseCertificate(last)
	require.NoError(t, err)
	require.Equal(t, "Intel SGX Root CA", cert.Subject.CommonName)
	return last
}

func TestNoOverridesIsTheDefaultVerifier(t *testing.T) {
	quote, _, collateral, at := loadRealQuote(t)

	_, _, err := types.VerifyQuoteWith(quote, collateral, at, nil)
	require.NoError(t, err)
	_, _, err = types.VerifyQuoteWith(quote, collateral, at, &types.VerifierOverrides{})
	require.NoError(t, err)
}

func TestTrustedRootOverride(t *testing.T) {
	quote, _, collateral, at := loadRealQuote(t)

	t.Run("intel's own root still verifies", func(t *testing.T) {
		_, _, err := types.VerifyQuoteWith(quote, collateral, at, &types.VerifierOverrides{
			TrustedRootCerts: [][]byte{intelRootFrom(t, collateral)},
		})
		require.NoError(t, err)
	})

	t.Run("a different root replaces intel's", func(t *testing.T) {
		// The override replaces the embedded root rather than adding to it, so a
		// quote chaining to Intel's root no longer verifies.
		_, _, err := types.VerifyQuoteWith(quote, collateral, at, &types.VerifierOverrides{
			TrustedRootCerts: [][]byte{selfSignedRoot(t)},
		})
		require.ErrorIs(t, err, types.ErrInvalidQuote)
	})
}

func TestCollateralGrace(t *testing.T) {
	quote, _, collateral, at := loadRealQuote(t)
	lapsed := at.Add(fixtureCollateralValidity + 5*24*time.Hour)

	_, _, err := types.VerifyQuoteWith(quote, collateral, lapsed, nil)
	require.ErrorIs(t, err, types.ErrInvalidQuote, "collateral lapsed 5 days ago")

	t.Run("a window covering the lapse verifies", func(t *testing.T) {
		_, _, err := types.VerifyQuoteWith(quote, collateral, lapsed, &types.VerifierOverrides{CollateralGraceSecs: 7 * 24 * 60 * 60})
		require.NoError(t, err)
	})

	t.Run("a window short of the lapse does not", func(t *testing.T) {
		_, _, err := types.VerifyQuoteWith(quote, collateral, lapsed, &types.VerifierOverrides{CollateralGraceSecs: 3 * 24 * 60 * 60})
		require.ErrorIs(t, err, types.ErrInvalidQuote)
	})

	t.Run("fresh collateral still verifies with a window set", func(t *testing.T) {
		// Block time is tried first, so collateral issued inside the window,
		// whose validity starts after the earlier instant, keeps working.
		_, _, err := types.VerifyQuoteWith(quote, collateral, at, &types.VerifierOverrides{CollateralGraceSecs: types.MaxCollateralGraceSecs})
		require.NoError(t, err)
	})

	t.Run("the window does not make up for a bad quote", func(t *testing.T) {
		bad := append([]byte{}, quote...)
		bad[len(bad)-256] ^= 0x01
		_, _, err := types.VerifyQuoteWith(bad, collateral, at, &types.VerifierOverrides{CollateralGraceSecs: types.MaxCollateralGraceSecs})
		require.ErrorIs(t, err, types.ErrInvalidQuote)
	})
}

// No override, alone or combined, lets a quote through that Intel's verification
// rejects: a broken signature fails however the owner has configured the ism.
func TestNoOverrideSkipsIntel(t *testing.T) {
	quote, _, collateral, at := loadRealQuote(t)
	badChain := append([]byte{}, quote...)
	badChain[len(badChain)-256] ^= 0x01

	for name, overrides := range map[string]*types.VerifierOverrides{
		"none":       nil,
		"empty":      {},
		"max grace":  {CollateralGraceSecs: types.MaxCollateralGraceSecs},
		"intel root": {TrustedRootCerts: [][]byte{intelRootFrom(t, collateral)}},
		"all": {
			TrustedRootCerts:    [][]byte{intelRootFrom(t, collateral)},
			CollateralGraceSecs: types.MaxCollateralGraceSecs,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := types.VerifyQuoteWith(badChain, collateral, at, overrides)
			require.ErrorIs(t, err, types.ErrInvalidQuote)
		})
	}
}
