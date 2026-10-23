package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// signingSetPowers are CometBFT voting powers where a 2% slash of the third
// validator (100 -> 98) drops it below the fourth (99).
var signingSetPowers = []int64{400, 200, 100, 99, 60, 41}

// slashedIndex is the validator that misbehaves in the tests below.
const slashedIndex = 2

// TestCertificateVerifiesAfterEquivocationSlash is a regression test for
// historical info being recorded after evidence slashed a validator in the same
// BeginBlock. CometBFT still signs the next block with the powers from before
// the slash, so the recorded order must be the pre-slash order or a positional
// certificate fails at the reordered index.
func TestCertificateVerifiesAfterEquivocationSlash(t *testing.T) {
	s := newSigningSetApp(t, nil)
	s.finalize(t, nil, nil) // height 1
	s.finalize(t, s.votes(-1), nil)

	// Double-sign evidence for height 2 slashes and jails the validator in
	// BeginBlock of height 3.
	slashHeight := s.finalize(t, s.votes(-1), []abci.Misbehavior{{
		Type:             abci.MisbehaviorType_DUPLICATE_VOTE,
		Validator:        abci.Validator{Address: s.keys[slashedIndex].PubKey().Address(), Power: signingSetPowers[slashedIndex]},
		Height:           2,
		Time:             testutil.GenesisTime,
		TotalVotingPower: s.valSet.TotalVotingPower(),
	}})
	s.requireSlashed(t, math.LegacyNewDecWithPrec(2, 2))

	s.requireCertificateVerifies(t, slashHeight+1)
}

// TestCertificateVerifiesAfterDowntimeJail covers a validator that slashing jails
// for downtime in BeginBlock of H-1. It stays in CometBFT's set at H, so its
// position is unchanged. Mainnet does not slash for downtime; the 2% case shows
// that a downtime slash reorders the set the same way equivocation does.
func TestCertificateVerifiesAfterDowntimeJail(t *testing.T) {
	for _, tc := range []struct {
		name          string
		slashFraction math.LegacyDec
	}{
		{name: "no downtime slash", slashFraction: math.LegacyZeroDec()},
		{name: "2% downtime slash", slashFraction: math.LegacyNewDecWithPrec(2, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSigningSetApp(t, func(gs *slashingtypes.GenesisState) {
				gs.Params.SignedBlocksWindow = 2
				gs.Params.MinSignedPerWindow = math.LegacyNewDecWithPrec(5, 1)
				gs.Params.SlashFractionDowntime = tc.slashFraction
			})
			s.finalize(t, nil, nil) // height 1

			var jailHeight int64
			for range 10 {
				height := s.finalize(t, s.votes(slashedIndex), nil)
				if s.slashedValidator(t).Jailed {
					jailHeight = height
					break
				}
			}
			require.NotZero(t, jailHeight, "validator was never jailed for downtime")
			s.requireSlashed(t, tc.slashFraction)

			s.requireCertificateVerifies(t, jailHeight+1)
		})
	}
}

type signingSetApp struct {
	app    *app.App
	keys   []ed25519.PrivKey
	valSet *core.ValidatorSet
}

// newSigningSetApp starts a chain whose genesis set has signingSetPowers.
func newSigningSetApp(t *testing.T, modifySlashing func(*slashingtypes.GenesisState)) *signingSetApp {
	t.Helper()
	keys := make([]ed25519.PrivKey, len(signingSetPowers))
	vals := make([]*core.Validator, len(signingSetPowers))
	for i, power := range signingSetPowers {
		keys[i] = ed25519.GenPrivKey()
		vals[i] = core.NewValidator(keys[i].PubKey(), power)
	}
	valSet := core.NewValidatorSet(vals)

	testApp := testutil.NewTestApp()
	genesisState, _ := testutil.GenesisStateWithValidatorSet(testApp, valSet)
	var slashingGenesis slashingtypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(genesisState[slashingtypes.ModuleName], &slashingGenesis)
	// Validators imported as bonded skip the hook that creates signing info,
	// which evidence and downtime handling require.
	for _, key := range keys {
		consAddr := sdk.ConsAddress(key.PubKey().Address())
		slashingGenesis.SigningInfos = append(slashingGenesis.SigningInfos, slashingtypes.SigningInfo{
			Address:              consAddr.String(),
			ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(consAddr, 0, 0, time.Unix(0, 0).UTC(), false, 0),
		})
	}
	if modifySlashing != nil {
		modifySlashing(&slashingGenesis)
	}
	genesisState[slashingtypes.ModuleName] = json.RawMessage(testApp.AppCodec().MustMarshalJSON(&slashingGenesis))
	testutil.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	return &signingSetApp{app: testApp, keys: keys, valSet: valSet}
}

// finalize executes and commits the next block and returns its height.
func (s *signingSetApp) finalize(t *testing.T, votes []abci.VoteInfo, misbehavior []abci.Misbehavior) int64 {
	t.Helper()
	height := s.app.LastBlockHeight() + 1
	_, err := s.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:               testutil.GenesisTime.Add(time.Duration(height) * time.Second),
		Height:             height,
		Hash:               s.app.LastCommitID().Hash,
		NextValidatorsHash: s.valSet.Hash(),
		DecidedLastCommit:  abci.CommitInfo{Votes: votes},
		Misbehavior:        misbehavior,
	})
	require.NoError(t, err)
	_, err = s.app.Commit()
	require.NoError(t, err)
	return height
}

// votes returns a last commit signed by every genesis validator except absent.
func (s *signingSetApp) votes(absent int) []abci.VoteInfo {
	votes := make([]abci.VoteInfo, len(s.keys))
	for i, key := range s.keys {
		flag := cmtproto.BlockIDFlagCommit
		if i == absent {
			flag = cmtproto.BlockIDFlagAbsent
		}
		votes[i] = abci.VoteInfo{
			Validator:   abci.Validator{Address: key.PubKey().Address(), Power: signingSetPowers[i]},
			BlockIdFlag: flag,
		}
	}
	return votes
}

func (s *signingSetApp) slashedValidator(t *testing.T) stakingtypes.Validator {
	t.Helper()
	val, err := s.app.StakingKeeper.GetValidator(s.app.NewContext(true), sdk.ValAddress(s.keys[slashedIndex].PubKey().Address()))
	require.NoError(t, err)
	return val
}

// requireSlashed checks the misbehaving validator is jailed and lost fraction of
// its bond, so a test cannot pass without the infraction having been applied.
func (s *signingSetApp) requireSlashed(t *testing.T, fraction math.LegacyDec) {
	t.Helper()
	bonded := sdk.TokensFromConsensusPower(signingSetPowers[slashedIndex], sdk.DefaultPowerReduction)
	val := s.slashedValidator(t)
	require.True(t, val.Jailed)
	require.Equal(t, bonded.Sub(fraction.MulInt(bonded).TruncateInt()), val.Tokens)
}

// requireCertificateVerifies builds a certificate for promiseHeight over
// CometBFT's set at that height, which no block has changed since genesis, and
// checks the chain accepts it. The second-ranked validator does not sign, so
// verification cannot reach quorum before the slashed validator and the one
// ranked below it, the positions a reordered set swaps.
func (s *signingSetApp) requireCertificateVerifies(t *testing.T, promiseHeight int64) {
	t.Helper()
	ctx := s.app.NewContext(true).WithBlockHeight(promiseHeight)

	ownerPriv := secp256k1.GenPrivKey()
	ownerPub := ownerPriv.PubKey().(*secp256k1.PubKey)
	promise := buildSignedPromise(t, promiseHeight, testutil.GenesisTime.Add(time.Duration(promiseHeight)*time.Second), *ownerPub, ownerPriv)
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)

	signingSet := validator.Set{ValidatorSet: s.valSet, Height: uint64(promiseHeight)}
	sigSet := signingSet.NewSignatureSet(fibre.DefaultProtocolParams.SafetyThreshold, signBytes)
	done := false
	for _, i := range []int{0, slashedIndex, slashedIndex + 1, slashedIndex + 2} {
		val, found := signingSet.GetByAddress(s.keys[i].PubKey().Address())
		require.True(t, found)
		signature, err := s.keys[i].Sign(signBytes)
		require.NoError(t, err)
		done, err = sigSet.Add(val, signature)
		require.NoError(t, err)
	}
	require.True(t, done, "signers must reach quorum")
	signatures, err := sigSet.Signatures()
	require.NoError(t, err)

	msg := &fibretypes.MsgPayForFibre{PaymentPromise: promise, ValidatorSignatures: signatures}
	require.NoError(t, s.app.FibreKeeper.ValidatePayForFibreSignatures(ctx, msg))
}
