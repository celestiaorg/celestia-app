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

func TestSkipDcap(t *testing.T) {
	quote, _, collateral, at := loadRealQuote(t)
	skip := &types.VerifierOverrides{SkipDcap: true}

	want, _, err := types.VerifyQuote(quote, collateral, at)
	require.NoError(t, err)

	t.Run("intel's verdict is skipped", func(t *testing.T) {
		badChain := append([]byte{}, quote...)
		badChain[len(badChain)-256] ^= 0x01
		_, _, err := types.VerifyQuote(badChain, collateral, at)
		require.Error(t, err)

		measured, _, err := types.VerifyQuoteWith(badChain, collateral, at, skip)
		require.NoError(t, err)
		require.Equal(t, want, measured, "the measurements are still read from the quote")

		_, _, err = types.VerifyQuoteWith(quote, collateral, at.Add(365*24*time.Hour), skip)
		require.NoError(t, err, "expired collateral no longer matters")
	})

	t.Run("the measurements are still the quote's", func(t *testing.T) {
		// Skipping Intel does not skip the identity check that follows: a quote
		// with other measurements yields them, and the pinned identity refuses it.
		// mr_td is bytes 184..232: a 48 byte header, then tee_tcb_svn, mr_seam,
		// mr_signer_seam, seam_attributes, td_attributes and xfam.
		otherTd := append([]byte{}, quote...)
		otherTd[184] ^= 0x01
		measured, _, err := types.VerifyQuoteWith(otherTd, collateral, at, skip)
		require.NoError(t, err)
		require.NotEqual(t, want.MrTd, measured.MrTd)
	})

	t.Run("the quote must still parse", func(t *testing.T) {
		_, _, err := types.VerifyQuoteWith(quote[:100], collateral, at, skip)
		require.ErrorIs(t, err, types.ErrInvalidQuote)
	})
}
