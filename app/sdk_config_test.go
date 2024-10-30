package app

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

func Test_maybeSetCosmosSDKConfig(t *testing.T) {
	config := sdk.GetConfig()
	assert.Equal(t, Bech32PrefixAccAddr, config.GetBech32AccountAddrPrefix())
	assert.Equal(t, Bech32PrefixAccPub, config.GetBech32AccountPubPrefix())
	assert.Equal(t, Bech32PrefixValAddr, config.GetBech32ValidatorAddrPrefix())
	assert.Equal(t, Bech32PrefixValPub, config.GetBech32ValidatorPubPrefix())
	assert.Equal(t, Bech32PrefixConsAddr, config.GetBech32ConsensusAddrPrefix())
	assert.Equal(t, Bech32PrefixConsPub, config.GetBech32ConsensusPubPrefix())
}
