package keeper

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/cometbft/cometbft/crypto/ed25519"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// newStakingValidator builds the staking record validatorSetAtHeight converts,
// with the consensus key packed the way historical info carries it.
func newStakingValidator(t *testing.T, tokens int64) stakingtypes.Validator {
	t.Helper()

	privKey := ed25519.GenPrivKey()
	pubKey, err := cryptocodec.FromCmtPubKeyInterface(privKey.PubKey())
	require.NoError(t, err)
	anyPubKey, err := codectypes.NewAnyWithValue(pubKey)
	require.NoError(t, err)

	return stakingtypes.Validator{
		OperatorAddress: sdk.ValAddress(privKey.PubKey().Address()).String(),
		ConsensusPubkey: anyPubKey,
		Tokens:          math.NewInt(tokens),
	}
}

// TestValidatorSetAtHeightReusesConversion pins the memoization: a second read
// of the same height returns the entry the first one published rather than
// rebuilding it.
func TestValidatorSetAtHeightReusesConversion(t *testing.T) {
	k := Keeper{valSetCache: newValidatorSetCache()}
	valset := []stakingtypes.Validator{
		newStakingValidator(t, 1_000_000),
		newStakingValidator(t, 2_000_000),
	}

	first, err := k.validatorSetAtHeight(7, valset)
	require.NoError(t, err)
	second, err := k.validatorSetAtHeight(7, valset)
	require.NoError(t, err)

	require.Same(t, first, second)
}

// TestValidatorSetAtHeightKeysOnHeight pins that two heights do not share an
// entry: validator signatures are positional over the set, so serving one
// height's set for another would verify against the wrong keys.
func TestValidatorSetAtHeightKeysOnHeight(t *testing.T) {
	k := Keeper{valSetCache: newValidatorSetCache()}
	atSeven := []stakingtypes.Validator{newStakingValidator(t, 1_000_000)}
	atEight := []stakingtypes.Validator{newStakingValidator(t, 5_000_000)}

	seven, err := k.validatorSetAtHeight(7, atSeven)
	require.NoError(t, err)
	eight, err := k.validatorSetAtHeight(8, atEight)
	require.NoError(t, err)

	require.NotSame(t, seven, eight)
	require.Equal(t, int64(1_000_000), seven.set.TotalVotingPower())
	require.Equal(t, int64(5_000_000), eight.set.TotalVotingPower())
}

// TestValidatorSetAtHeightPreservesOrderAndPower pins what the conversion owes
// its callers: historical info order, because signatures are positional over
// it, and a total voting power already computed so a shared entry is never
// written to again.
func TestValidatorSetAtHeightPreservesOrderAndPower(t *testing.T) {
	k := Keeper{valSetCache: newValidatorSetCache()}
	valset := []stakingtypes.Validator{
		newStakingValidator(t, 1_000_000),
		newStakingValidator(t, 2_000_000),
		newStakingValidator(t, 3_000_000),
	}

	converted, err := k.validatorSetAtHeight(7, valset)
	require.NoError(t, err)

	require.Len(t, converted.validators, len(valset))
	for i, val := range valset {
		consPubKey, err := val.ConsPubKey()
		require.NoError(t, err)
		require.Equal(t, consPubKey.Bytes(), converted.validators[i].PubKey.Bytes())
		require.Equal(t, val.Tokens.Int64(), converted.validators[i].VotingPower)
	}
	require.Equal(t, int64(6_000_000), converted.set.TotalVotingPower())
}

// TestValidatorSetAtHeightRejectsNonEd25519 covers the size guard: consensus
// keys are ed25519, and a set carrying anything else must fail rather than be
// converted into validators whose signatures can never verify.
func TestValidatorSetAtHeightRejectsNonEd25519(t *testing.T) {
	pubKey, err := codectypes.NewAnyWithValue(secp256k1.GenPrivKey().PubKey())
	require.NoError(t, err)
	valset := []stakingtypes.Validator{{
		OperatorAddress: sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		ConsensusPubkey: pubKey,
		Tokens:          math.NewInt(1_000_000),
	}}

	k := Keeper{valSetCache: newValidatorSetCache()}
	_, err = k.validatorSetAtHeight(7, valset)
	require.ErrorContains(t, err, "invalid ed25519 public key size")

	// A rejected set is not cached, so a later correct read is not poisoned.
	require.Zero(t, k.valSetCache.Len())
}

// TestValidatorSetAtHeightWithoutCache pins that the cache is an optimization:
// a keeper built without one still converts, it just converts every time.
func TestValidatorSetAtHeightWithoutCache(t *testing.T) {
	k := Keeper{}
	valset := []stakingtypes.Validator{newStakingValidator(t, 1_000_000)}

	first, err := k.validatorSetAtHeight(7, valset)
	require.NoError(t, err)
	second, err := k.validatorSetAtHeight(7, valset)
	require.NoError(t, err)

	require.NotSame(t, first, second)
	require.Equal(t, first.set.TotalVotingPower(), second.set.TotalVotingPower())
}

// TestValidatorSetAtHeightEvicts pins that the cache is bounded: past its size
// the oldest height is dropped and rebuilt on the next read.
func TestValidatorSetAtHeightEvicts(t *testing.T) {
	k := Keeper{valSetCache: newValidatorSetCache()}
	valset := []stakingtypes.Validator{newStakingValidator(t, 1_000_000)}

	first, err := k.validatorSetAtHeight(1, valset)
	require.NoError(t, err)
	for height := int64(2); height <= validatorSetCacheSize+1; height++ {
		_, err = k.validatorSetAtHeight(height, valset)
		require.NoError(t, err)
	}
	require.Equal(t, validatorSetCacheSize, k.valSetCache.Len())

	rebuilt, err := k.validatorSetAtHeight(1, valset)
	require.NoError(t, err)
	require.NotSame(t, first, rebuilt)
}
