//go:build pffcampaign

package util

import (
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

func CampaignGenesis(a *app.App, vals *core.ValidatorSet, names ...string) (app.GenesisState, keyring.Keyring) {
	return genesisStateWithValidatorSet(a, vals, testnode.FundDeterministicKeyringAccounts, names...)
}
