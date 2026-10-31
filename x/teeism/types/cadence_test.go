package types

import "testing"

func TestAttestationCadence(t *testing.T) {
	prev := &IsmState{Height: 10, Timestamp: 1_000}
	at := func(ts uint64) *IsmState { return &IsmState{Height: 11, Timestamp: ts} }

	if err := VerifyBatchCadence(prev, at(1_000), true); err != nil {
		t.Fatalf("an attestation with a new root is never held back: %v", err)
	}
	if err := VerifyBatchCadence(prev, at(1_000+EmptyBatchMinIntervalSecs-1), false); err == nil {
		t.Fatal("an unchanged root inside the interval must be refused")
	}
	if err := VerifyBatchCadence(prev, at(1_000+EmptyBatchMinIntervalSecs), false); err != nil {
		t.Fatalf("an unchanged root a full interval later advances the state: %v", err)
	}
}
