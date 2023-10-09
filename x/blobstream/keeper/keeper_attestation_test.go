package keeper_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/blobstream"
	"github.com/stretchr/testify/assert"
)

func TestCheckLatestAttestationNonce(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobStreamKeysInit(t)
	k := input.BStreamKeeper

	// check if the latest attestation nonce is init
	exists := k.CheckLatestAttestationNonce(input.Context)
	assert.False(t, exists)

	// init the latest attestation nonce
	input.BStreamKeeper.SetLatestAttestationNonce(input.Context, blobstream.InitialLatestAttestationNonce)

	// check if the latest attestation nonce value was initialized correctly
	input.BStreamKeeper.CheckLatestAttestationNonce(input.Context)
	assert.Equal(t, blobstream.InitialLatestAttestationNonce, input.BStreamKeeper.GetLatestAttestationNonce(input.Context))
}

func TestCheckEarliestAvailableAttestationNonce(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobStreamKeysInit(t)
	k := input.BStreamKeeper

	// check if the earliest available attestation nonce is init
	exists := k.CheckEarliestAvailableAttestationNonce(input.Context)
	assert.False(t, exists)

	// init the earliest available attestation nonce
	input.BStreamKeeper.SetEarliestAvailableAttestationNonce(input.Context, blobstream.InitialEarliestAvailableAttestationNonce)

	// check if the earliest attestation nonce value was initialized correctly
	input.BStreamKeeper.CheckEarliestAvailableAttestationNonce(input.Context)
	assert.Equal(t, blobstream.InitialEarliestAvailableAttestationNonce, input.BStreamKeeper.GetEarliestAvailableAttestationNonce(input.Context))
}
