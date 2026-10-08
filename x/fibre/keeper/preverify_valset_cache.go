package keeper

import (
	"sync"

	errorsmod "cosmossdk.io/errors"
	naryaed25519 "github.com/Overclock-Validator/narya-ed25519/ed25519"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	voied25519 "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
)

type preverifyValsetCacheKey struct{}

// preverifyValsetCache is scoped to one proposal's read-only preverification.
// A historical validator set cannot change while the workers read the same
// proposal state, so each distinct height needs one conversion and decode.
type preverifyValsetCache struct {
	entries sync.Map // int64 -> *cachedValset
}

type cachedValset struct {
	once sync.Once
	set  *convertedValset
	err  error
}

type convertedValset struct {
	validators []*core.Validator // historical info order, which binds signature positions
	set        *core.ValidatorSet
	pubs       [][32]byte
	naryaCache naryaed25519.Cache
	expandOnce sync.Once
	expanded   []*voied25519.ExpandedPublicKey
}

// expandedKeys are immutable after first construction and are shared by the
// concurrent preverification workers for this proposal only.
func (v *convertedValset) expandedKeys() []*voied25519.ExpandedPublicKey {
	v.expandOnce.Do(func() {
		v.expanded = make([]*voied25519.ExpandedPublicKey, len(v.validators))
		for i, validator := range v.validators {
			v.expanded[i], _ = voied25519.NewExpandedPublicKey(validator.PubKey.Bytes())
		}
	})
	return v.expanded
}

// WithPreverifyValsetCache lets workers share immutable validator sets for one
// ProcessProposal preverification pass. The sequential ante path does not use it.
func WithPreverifyValsetCache(ctx sdk.Context) sdk.Context {
	return ctx.WithValue(preverifyValsetCacheKey{}, &preverifyValsetCache{})
}

func (k Keeper) validatorSetForSignatures(ctx sdk.Context, height int64) (*convertedValset, error) {
	cache, ok := ctx.Value(preverifyValsetCacheKey{}).(*preverifyValsetCache)
	if !ok {
		return k.loadValidatorSetForSignatures(ctx, height)
	}
	value, _ := cache.entries.LoadOrStore(height, &cachedValset{})
	entry := value.(*cachedValset)
	entry.once.Do(func() {
		entry.set, entry.err = k.loadValidatorSetForSignatures(ctx, height)
	})
	return entry.set, entry.err
}

func (k Keeper) loadValidatorSetForSignatures(ctx sdk.Context, height int64) (*convertedValset, error) {
	historicalInfo, err := k.stakingKeeper.GetHistoricalInfo(ctx, height)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to get historical validator set at height %d", height)
	}
	validators := make([]*core.Validator, len(historicalInfo.Valset))
	for i, val := range historicalInfo.Valset {
		consPubKey, err := val.ConsPubKey()
		if err != nil {
			return nil, errorsmod.Wrapf(err, "failed to get consensus public key for validator %s", val.GetOperator())
		}
		pubKeyBytes := consPubKey.Bytes()
		if len(pubKeyBytes) != ed25519.PubKeySize {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid ed25519 public key size for validator %s", val.GetOperator())
		}
		validators[i] = core.NewValidator(ed25519.PubKey(pubKeyBytes), val.Tokens.Int64())
	}
	set := core.NewValidatorSet(validators)
	set.TotalVotingPower() // Force lazy memoization before sharing across workers.
	pubs := make([][32]byte, len(validators))
	for i, val := range validators {
		copy(pubs[i][:], val.PubKey.Bytes())
	}
	return &convertedValset{
		validators: validators,
		set:        set,
		pubs:       pubs,
		naryaCache: naryaed25519.Cache{MaxTableBytes: 8 << 20},
	}, nil
}
