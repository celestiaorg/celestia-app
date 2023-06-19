package keeper_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/stretchr/testify/assert"
)

func TestCheckLatestAttestationNonce(t *testing.T) {
	input := testutil.CreateTestEnvWithoutQGBKeysInit(t)
	k := input.QgbKeeper

	// check if the latest attestation nonce is init
	exists := k.CheckLatestAttestationNonce(input.Context)
	assert.False(t, exists)

	// init the latest attestation nonce
	input.QgbKeeper.SetLatestAttestationNonce(input.Context, qgb.InitialLatestAttestationNonce)

	// check if the latest attestation nonce value was initialized correctly
	input.QgbKeeper.CheckLatestAttestationNonce(input.Context)
	assert.Equal(t, qgb.InitialLatestAttestationNonce, input.QgbKeeper.GetLatestAttestationNonce(input.Context))
}

func TestCheckEarliestAvailableAttestationNonce(t *testing.T) {
	input := testutil.CreateTestEnvWithoutQGBKeysInit(t)
	k := input.QgbKeeper

	// check if the earliest available attestation nonce is init
	exists := k.CheckEarliestAvailableAttestationNonce(input.Context)
	assert.False(t, exists)

	// init the earliest available attestation nonce
	input.QgbKeeper.SetEarliestAvailableAttestationNonce(input.Context, qgb.InitialEarliestAvailableAttestationNonce)

	// check if the earliest attestation nonce value was initialized correctly
	input.QgbKeeper.CheckEarliestAvailableAttestationNonce(input.Context)
	assert.Equal(t, qgb.InitialEarliestAvailableAttestationNonce, input.QgbKeeper.GetEarliestAvailableAttestationNonce(input.Context))
}
