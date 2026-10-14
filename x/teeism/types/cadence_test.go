package types

import "testing"

func TestEmptyBatchCadence(t *testing.T) {
	prev := &IsmState{Height: 10, Timestamp: 1_000}
	at := func(ts uint64) *IsmState { return &IsmState{Height: 11, Timestamp: ts} }

	if err := VerifyBatchCadence(prev, at(1_000), 1); err != nil {
		t.Fatalf("a batch with messages is never held back: %v", err)
	}
	if err := VerifyBatchCadence(prev, at(1_000+EmptyBatchMinIntervalSecs-1), 0); err == nil {
		t.Fatal("an empty batch inside the interval must be refused")
	}
	if err := VerifyBatchCadence(prev, at(1_000+EmptyBatchMinIntervalSecs), 0); err != nil {
		t.Fatalf("an empty batch a full interval later advances the state: %v", err)
	}
}
