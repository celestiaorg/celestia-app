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
type SignatureSet struct {
	requiredBytesSigned    []byte
	minRequiredVotingPower int64
	minRequiredSignatures  int

	mu          sync.Mutex
	votingPower int64
	signatures  [][]byte
	done        chan struct{}
}

// NewSignatureSet creates a new [SignatureSet] for collecting and validating signatures.
func (s Set) NewSignatureSet(targetVotingPower, targetValidatorsCount cmtmath.Fraction, requiredBytesSigned []byte) *SignatureSet {
	minRequiredVotingPower := s.TotalVotingPower() * int64(targetVotingPower.Numerator) / int64(targetVotingPower.Denominator)
	minRequiredSignatures := s.Size() * int(targetValidatorsCount.Numerator) / int(targetValidatorsCount.Denominator)

	return &SignatureSet{
		requiredBytesSigned:    requiredBytesSigned,
		minRequiredVotingPower: minRequiredVotingPower,
		minRequiredSignatures:  minRequiredSignatures,
		signatures:             make([][]byte, 0, s.Size()),
		done:                   make(chan struct{}),
	}
}

// Add validates and adds a signature from the given validator.
// Returns an error if the signature is invalid.
func (ss *SignatureSet) Add(val *core.Validator, signature []byte) error {
	// verify signature
	pubKey := val.PubKey.Bytes()
	if !ed25519.Verify(ed25519.PublicKey(pubKey), ss.requiredBytesSigned, signature) {
		return fmt.Errorf("invalid signature from validator %s", val.Address.String())
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// add to collection
	ss.votingPower += val.VotingPower
	ss.signatures = append(ss.signatures, signature)

	// check if thresholds are met and close done channel
	if len(ss.signatures) >= ss.minRequiredSignatures && ss.votingPower >= ss.minRequiredVotingPower {
		select {
		case <-ss.done:
			// already closed
		default:
			close(ss.done)
		}
	}

	return nil
}

// Done returns a channel that is closed when enough signatures are collected to meet both thresholds.
func (ss *SignatureSet) Done() <-chan struct{} {
	return ss.done
}

// Signatures returns all collected signatures if thresholds are met.
// Returns [NotEnoughSignaturesError] if either count or voting power threshold is not met.
// The error contains the partially collected signatures and threshold information.
func (ss *SignatureSet) Signatures() ([][]byte, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	countNotMet := len(ss.signatures) < ss.minRequiredSignatures
	powerNotMet := ss.votingPower < ss.minRequiredVotingPower
	if countNotMet || powerNotMet {
		return nil, &NotEnoughSignaturesError{
			Collected:      ss.signatures,
			RequiredCount:  ss.minRequiredSignatures,
			CollectedPower: ss.votingPower,
			RequiredPower:  ss.minRequiredVotingPower,
		}
	}

	return ss.signatures, nil
}

// NotEnoughSignaturesError indicates that signature collection did not meet the required thresholds.
// It contains the partially collected signatures and threshold information.
type NotEnoughSignaturesError struct {
	Collected      [][]byte
	RequiredCount  int
	CollectedPower int64
	RequiredPower  int64
}

func (e *NotEnoughSignaturesError) Error() string {
	switch {
	case len(e.Collected) < e.RequiredCount:
		return fmt.Sprintf("not enough signatures: collected %d, required %d", len(e.Collected), e.RequiredCount)
	case e.CollectedPower < e.RequiredPower:
		return fmt.Sprintf("not enough voting power: collected %d, required %d", e.CollectedPower, e.RequiredPower)
	default:
		panic("unreachable")
	}
}
