package testnet_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/test/e2e/testnet"
)

func TestVersionParsing(t *testing.T) {
	versionStr := "v1.3.0 v1.1.0 v1.2.0-rc0"
	versions := testnet.ParseVersions(versionStr)
	require.Len(t, versions, 3)
	require.Len(t, versions.FilterOutReleaseCandidates(), 2)
	require.Equal(t, versions.GetLatest(), testnet.Version{1, 3, 0, false, 0})
}

// Test case with multiple major versions and filtering out a single major version
func TestFilterMajorVersions(t *testing.T) {
	versionStr := "v2.0.0 v1.1.0 v2.1.0-rc0 v1.2.0 v2.2.0 v1.3.0"
	versions := testnet.ParseVersions(versionStr)
	require.Len(t, versions, 6)
	require.Len(t, versions.FilterMajor(1), 3)
}

// Test case to check the Order function
func TestOrder(t *testing.T) {
	versionStr := "v1.3.0 v1.1.0 v1.2.0-rc0 v1.4.0 v1.2.1 v2.0.0"
	versions := testnet.ParseVersions(versionStr)
	versions.Order()
	require.Equal(t, versions[0], testnet.Version{1, 1, 0, false, 0})
	require.Equal(t, versions[1], testnet.Version{1, 2, 0, true, 0})
	require.Equal(t, versions[2], testnet.Version{1, 2, 1, false, 0})
	require.Equal(t, versions[3], testnet.Version{1, 3, 0, false, 0})
	require.Equal(t, versions[4], testnet.Version{1, 4, 0, false, 0})
	require.Equal(t, versions[5], testnet.Version{2, 0, 0, false, 0})
	for i := len(versions) - 1; i > 0; i-- {
		require.True(t, versions[i].IsGreater(versions[i-1]))
	}
}

func TestOrderOfReleaseCandidates(t *testing.T) {
	versionsStr := "v1.0.0 v1.0.0-rc0 v1.0.0-rc1"
	versions := testnet.ParseVersions(versionsStr)
	versions.Order()
	require.Equal(t, versions[0], testnet.Version{1, 0, 0, true, 0})
	require.Equal(t, versions[1], testnet.Version{1, 0, 0, true, 1})
	require.Equal(t, versions[2], testnet.Version{1, 0, 0, false, 0})
}
