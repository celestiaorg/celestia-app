package validator

import (
	"crypto/ed25519"
	"fmt"
	"sync"

	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
)

// SignatureSet collects and validates signatures from validators.
// It is safe for concurrent use.
// Signatures are returned in validator set order by [Signatures],
// with nil entries for validators that did not sign.
type SignatureSet struct {
	// requiredBytesSigned is the message that each validator's signature is
	// verified against. A signature is only accepted if it is a valid Ed25519
	// signature of these bytes by the validator's public key.
	requiredBytesSigned    []byte
	minRequiredVotingPower int64
	validators             []*core.Validator

	mu          sync.Mutex
	votingPower int64
	signatures  map[string][]byte
}

// NewSignatureSet creates a new [SignatureSet] for collecting and validating signatures.
func (s Set) NewSignatureSet(targetVotingPower cmtmath.Fraction, requiredBytesSigned []byte) *SignatureSet {
	minRequiredVotingPower := s.TotalVotingPower() * int64(targetVotingPower.Numerator) / int64(targetVotingPower.Denominator)

	return &SignatureSet{
		requiredBytesSigned:    requiredBytesSigned,
		minRequiredVotingPower: minRequiredVotingPower,
		validators:             s.Validators,
		signatures:             make(map[string][]byte, s.Size()),
	}
}

// Add validates and adds a signature from the given validator.
// Returns an error if the signature is invalid.
// Returns true if enough signatures have been collected to meet both thresholds.
func (ss *SignatureSet) Add(val *core.Validator, signature []byte) (bool, error) {
	// verify signature
	pubKey := val.PubKey.Bytes()
	if !ed25519.Verify(ed25519.PublicKey(pubKey), ss.requiredBytesSigned, signature) {
		return false, fmt.Errorf("invalid signature from validator %s", val.Address.String())
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// add to collection
	ss.votingPower += val.VotingPower
	ss.signatures[val.Address.String()] = signature

	// check if thresholds are met
	return ss.votingPower >= ss.minRequiredVotingPower, nil
}

// Signatures returns collected signatures ordered by validator set position if thresholds are met.
// Validators that did not sign have nil entries.
// Returns [NotEnoughSignaturesError] if voting power threshold is not met.
// The error contains the partially collected signatures and threshold information.
func (ss *SignatureSet) Signatures() ([][]byte, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ordered := make([][]byte, len(ss.validators))
	for i, val := range ss.validators {
		if sig, ok := ss.signatures[val.Address.String()]; ok {
			ordered[i] = sig
		}
	}

	if ss.votingPower < ss.minRequiredVotingPower {
		return nil, &NotEnoughSignaturesError{
			Collected:      ordered,
			CollectedPower: ss.votingPower,
			RequiredPower:  ss.minRequiredVotingPower,
		}
	}

	return ordered, nil
}

// NotEnoughSignaturesError indicates that signature collection did not meet the required thresholds.
// It contains the partially collected signatures and threshold information.
type NotEnoughSignaturesError struct {
	Collected      [][]byte
	CollectedPower int64
	RequiredPower  int64
}

func (e *NotEnoughSignaturesError) Error() string {
	return fmt.Sprintf("not enough voting power: collected %d, required %d", e.CollectedPower, e.RequiredPower)
}
