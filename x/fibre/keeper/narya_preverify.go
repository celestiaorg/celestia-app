package keeper

import (
	"sync"

	errorsmod "cosmossdk.io/errors"
	naryaed25519 "github.com/Overclock-Validator/narya-ed25519/ed25519"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	naryaInit  sync.Once
	naryaReady bool
)

// naryaPositionalSignatures uses independent AVX-512 verification equations.
// The StdlibCompat profile agrees with the existing cofactorless verifier;
// other machines keep the established expanded-key path.
func naryaPositionalSignatures(converted *convertedValset, signBytes []byte, signatures [][]byte) (error, bool) {
	naryaInit.Do(func() {
		naryaed25519.SetDefaultProfile(naryaed25519.StdlibCompat)
		naryaReady = naryaed25519.SetBackend("r51") == nil
	})
	if !naryaReady {
		return nil, false
	}

	required := converted.set.TotalVotingPower() * 2 / 3
	var power int64
	end := len(signatures)
	for i, sig := range signatures {
		if len(sig) == 0 {
			continue
		}
		power += converted.validators[i].VotingPower
		if power >= required {
			end = i + 1
			break
		}
	}

	pubs := make([]*[32]byte, 0, end)
	msgs := make([][]byte, 0, end)
	sigs := make([][]byte, 0, end)
	for i, sig := range signatures[:end] {
		if len(sig) == 0 {
			continue
		}
		pubs = append(pubs, &converted.pubs[i])
		msgs = append(msgs, signBytes)
		sigs = append(sigs, sig)
	}
	valid := make([]bool, len(sigs))
	converted.naryaCache.VerifyBatch(pubs, msgs, sigs, valid)

	power = 0
	j := 0
	for i, sig := range signatures[:end] {
		if len(sig) == 0 {
			continue
		}
		if !valid[j] {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid signature at index %d: invalid signature from validator %s", i, converted.validators[i].Address.String()), true
		}
		j++
		power += converted.validators[i].VotingPower
		if power >= required {
			return nil, true
		}
	}
	return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature validation failed: %s", (&validator.NotEnoughSignaturesError{CollectedPower: power, RequiredPower: required}).Error()), true
}
