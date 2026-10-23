package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	lru "github.com/hashicorp/golang-lru/v2"
)

// quorumFraction is the share of voting power a certificate must carry.
var quorumFraction = cmtmath.Fraction{Numerator: 2, Denominator: 3}

// certificateValidatorSet returns the converted validator set a certificate at height is
// checked against. A cached conversion still needs the height stored, but not decoded again.
func (k Keeper) certificateValidatorSet(ctx sdk.Context, height int64) (*convertedValidatorSet, error) {
	if k.valSetCache != nil {
		if cached, ok := k.valSetCache.Get(height); ok {
			if err := k.checkHistoricalInfo(ctx, height); err != nil {
				return nil, errorsmod.Wrapf(err, "failed to get historical validator set at height %d", height)
			}
			return cached, nil
		}
	}
	historicalInfo, err := k.stakingKeeper.GetHistoricalInfo(ctx, height)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to get historical validator set at height %d", height)
	}
	return k.validatorSetAtHeight(height, historicalInfo.Valset)
}

// checkSignatureCount rejects a positional list longer than the validator set, which also
// keeps every index in range so a quorum short-circuit cannot skip a trailing entry.
func checkSignatureCount(converted *convertedValidatorSet, signatures [][]byte) error {
	if len(signatures) > len(converted.validators) {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature count %d exceeds validator count %d", len(signatures), len(converted.validators))
	}
	return nil
}

// validatorSetCacheSize is the number of heights whose converted validator set
// is kept. Payment promises reference recent heights, so a few hundred entries
// cover the settleable window.
const validatorSetCacheSize = 512

// convertedValidatorSet is a validator set built from staking historical info.
// It is immutable once cached, so concurrent readers are safe.
type convertedValidatorSet struct {
	// validators is in historical info order. Validator signatures are
	// positional over this slice.
	validators []*core.Validator
	set        *core.ValidatorSet
}

// validatorSetAtHeight converts valset for use in signature verification,
// reusing an earlier conversion of the same height. Staking writes historical
// info once per height and only ever deletes it, so a height identifies its
// validator set. The cache lives in memory, and rolling state back requires an
// offline command and therefore a restart, so it cannot serve a stale set.
func (k Keeper) validatorSetAtHeight(height int64, valset []stakingtypes.Validator) (*convertedValidatorSet, error) {
	if k.valSetCache != nil {
		if cached, ok := k.valSetCache.Get(height); ok {
			return cached, nil
		}
	}

	validators := make([]*core.Validator, len(valset))
	for i, val := range valset {
		consPubKey, err := val.ConsPubKey()
		if err != nil {
			return nil, errorsmod.Wrapf(err, "failed to get consensus public key for validator %s", val.GetOperator())
		}

		// Create CometBFT ed25519 public key from bytes
		pubKeyBytes := consPubKey.Bytes()
		if len(pubKeyBytes) != ed25519.PubKeySize {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid ed25519 public key size for validator %s", val.GetOperator())
		}

		validators[i] = core.NewValidator(ed25519.PubKey(pubKeyBytes), val.Tokens.Int64())
	}

	converted := &convertedValidatorSet{
		validators: validators,
		set:        core.NewValidatorSet(validators),
	}
	// TotalVotingPower is computed lazily and memoized on the set. Force it
	// before publishing so a shared entry is never written to again.
	converted.set.TotalVotingPower()

	if k.valSetCache != nil {
		k.valSetCache.Add(height, converted)
	}
	return converted, nil
}

// newValidatorSetCache returns the per-height validator set cache.
func newValidatorSetCache() *lru.Cache[int64, *convertedValidatorSet] {
	cache, err := lru.New[int64, *convertedValidatorSet](validatorSetCacheSize)
	if err != nil {
		panic(err)
	}
	return cache
}
