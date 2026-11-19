package types_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// The real fixture's collateral was fetched moments before the quote was
// verified, so its issue time sits just before the pinned clock.
func TestCollateralIssuedAt(t *testing.T) {
	quote, _, collateral, at := loadRealQuote(t)
	issued, err := collateral.IssuedAt()
	require.NoError(t, err)
	require.False(t, issued.IsZero())
	require.True(t, issued.Before(at) || issued.Equal(at), "issued %s, verified %s", issued, at)
	require.Less(t, at.Sub(issued), 14*24*time.Hour)

	result, err := types.VerifyAttestation(quote, nil, collateral, nil, nil, at, nil)
	require.Error(t, err) // no payload: but the issue time is read before that
	_ = result

	t.Run("unreadable dates", func(t *testing.T) {
		bad := *collateral
		bad.TcbInfo = []byte(`{"tcbInfo":{"issueDate":"yesterday"},"signature":""}`)
		_, err := bad.IssuedAt()
		require.ErrorIs(t, err, types.ErrInvalidCollateral)
		bad = *collateral
		bad.PckCrl = []byte{1, 2, 3}
		_, err = bad.IssuedAt()
		require.ErrorIs(t, err, types.ErrInvalidCollateral)
	})
}

// Collateral inside Intel's own validity window is still refused once it is
// older than the age bound, so a revoked platform cannot ride out the window.
func TestStaleCollateralIsRefusedBeforeIntelsWindowEnds(t *testing.T) {
	quote, _, collateral, _ := loadRealQuote(t)
	issued, err := collateral.IssuedAt()
	require.NoError(t, err)

	fresh := issued.Add(types.MaxCollateralAgeSecs*time.Second - time.Hour)
	_, _, err = types.VerifyQuote(quote, collateral, fresh)
	require.NoError(t, err, "one hour inside the age bound verifies")

	stale := issued.Add(types.MaxCollateralAgeSecs*time.Second + time.Hour)
	_, _, err = types.VerifyQuote(quote, collateral, stale)
	require.ErrorIs(t, err, types.ErrInvalidCollateral)
	require.Contains(t, err.Error(), "old at block time")
}

func TestCheckCollateralFreshness(t *testing.T) {
	block := time.Unix(1_800_000_000, 0)
	issued := block.Add(-time.Hour)
	day := uint64(24 * 60 * 60)

	require.NoError(t, types.CheckCollateralFreshness(issued, block, 0), "first attestation")
	require.NoError(t, types.CheckCollateralFreshness(issued, block, uint64(issued.Unix())), "same collateral again")
	require.NoError(t, types.CheckCollateralFreshness(issued, block, uint64(issued.Unix())+day/2), "within the regression slack")

	err := types.CheckCollateralFreshness(issued, block, uint64(issued.Unix())+2*day)
	require.ErrorIs(t, err, types.ErrInvalidCollateral)
	require.Contains(t, err.Error(), "older than what this ism last accepted")

	old := block.Add(-time.Duration(types.MaxCollateralAgeSecs)*time.Second - time.Second)
	err = types.CheckCollateralFreshness(old, block, 0)
	require.ErrorIs(t, err, types.ErrInvalidCollateral)
}

// The real fixture's root CA CRL is Intel's current one, issued 2026-02-26 and
// valid for a year; IssuedAt leaves it out, and the ism holds it to its own
// no-going-back rule.
func TestRootCaCrlIssuedAt(t *testing.T) {
	_, _, collateral, _ := loadRealQuote(t)
	issued, err := collateral.RootCaCrlIssuedAt()
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 2, 26, 13, 4, 0, 0, time.UTC), issued.UTC())

	bad := *collateral
	bad.RootCaCrl = []byte{1, 2, 3}
	_, err = bad.RootCaCrlIssuedAt()
	require.ErrorIs(t, err, types.ErrInvalidCollateral)
}

func TestCheckRootCaCrlRegression(t *testing.T) {
	issued := time.Date(2026, 2, 26, 13, 4, 0, 0, time.UTC)
	day := uint64(24 * 60 * 60)

	require.NoError(t, types.CheckRootCaCrlRegression(issued, 0), "first attestation")
	require.NoError(t, types.CheckRootCaCrlRegression(issued, uint64(issued.Unix())), "the same crl again")
	require.NoError(t, types.CheckRootCaCrlRegression(issued, uint64(issued.Unix())+day/2), "within the regression slack")

	// Intel revokes an intermediate CA in June with a new CRL; once an ism has
	// accepted it, February's is refused, though Intel signed it and it is valid
	// until 2027.
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	err := types.CheckRootCaCrlRegression(issued, uint64(june.Unix()))
	require.ErrorIs(t, err, types.ErrInvalidCollateral)
	require.Contains(t, err.Error(), "older than the one this ism last accepted")

	require.Error(t, types.CheckRootCaCrlRegression(time.Unix(-1, 0), 0))
}
