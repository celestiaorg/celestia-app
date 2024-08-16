package keeper_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/x/blobstream"
	"github.com/stretchr/testify/assert"
)

func TestCheckLatestAttestationNonce(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper

	// check if the latest attestation nonce is init
	exists := k.CheckLatestAttestationNonce(input.Context)
	assert.False(t, exists)

	// init the latest attestation nonce
	input.BlobstreamKeeper.SetLatestAttestationNonce(input.Context, blobstream.InitialLatestAttestationNonce)

	// check if the latest attestation nonce value was initialized correctly
	input.BlobstreamKeeper.CheckLatestAttestationNonce(input.Context)
	assert.Equal(t, blobstream.InitialLatestAttestationNonce, input.BlobstreamKeeper.GetLatestAttestationNonce(input.Context))
}

func TestCheckEarliestAvailableAttestationNonce(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper

	// check if the earliest available attestation nonce is init
	exists := k.CheckEarliestAvailableAttestationNonce(input.Context)
	assert.False(t, exists)

	// init the earliest available attestation nonce
	input.BlobstreamKeeper.SetEarliestAvailableAttestationNonce(input.Context, blobstream.InitialEarliestAvailableAttestationNonce)

	// check if the earliest attestation nonce value was initialized correctly
	input.BlobstreamKeeper.CheckEarliestAvailableAttestationNonce(input.Context)
	assert.Equal(t, blobstream.InitialEarliestAvailableAttestationNonce, input.BlobstreamKeeper.GetEarliestAvailableAttestationNonce(input.Context))
}
