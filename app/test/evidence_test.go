package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// TestFinalizeBlockIgnoresEvidenceForUnknownValidator is a regression test for
// CELESTIA-267. Equivocation evidence naming a consensus address that is not in
// staking state (e.g. a validator that fully unbonded and was removed) must be
// ignored rather than propagating an error out of FinalizeBlock, which would
// halt the chain. See evidenceStakingKeeper.
func TestFinalizeBlockIgnoresEvidenceForUnknownValidator(t *testing.T) {
	accounts := testfactory.GenerateAccounts(1)
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)

	// A consensus address that was never (or is no longer) a validator.
	unknownConsAddr := make([]byte, 20)
	for i := range unknownConsAddr {
		unknownConsAddr[i] = byte(i + 1)
	}

	misbehavior := abci.Misbehavior{
		Type:             abci.MisbehaviorType_DUPLICATE_VOTE,
		Validator:        abci.Validator{Address: unknownConsAddr, Power: 100},
		Height:           1,
		Time:             time.Now(),
		TotalVotingPower: 100,
	}

	resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:        time.Now(),
		Height:      testApp.LastBlockHeight() + 1,
		Hash:        testApp.LastCommitID().Hash,
		Misbehavior: []abci.Misbehavior{misbehavior},
	})
	require.NoError(t, err, "evidence for an unknown validator must not halt FinalizeBlock")
	require.NotNil(t, resp)
}
