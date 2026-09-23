package main

import (
	"context"
	"testing"
	"time"
)

func TestSubmissionBackoff(t *testing.T) {
	for _, upper := range []bool{false, true} {
		var b submissionBackoff
		jitter := func(n int64) int64 {
			if upper {
				return n - 1
			}
			return 0
		}
		for i := 0; i < 100; i++ {
			ceiling := maxSubmissionBackoff
			if i < 5 {
				ceiling = 500 * time.Millisecond << i
			}
			want := ceiling / 2
			if upper {
				want = ceiling
			}
			if got := b.nextWithJitter(false, jitter); got != want {
				t.Fatalf("failure %d: got %s, want %s", i, got, want)
			}
		}
		if got := b.nextWithJitter(true, jitter); got != 0 || b.ceiling != 0 {
			t.Fatal("success did not reset backoff")
		}
		if got := b.nextWithJitter(false, func(int64) int64 { return 0 }); got != 250*time.Millisecond {
			t.Fatalf("first failure after success: %s", got)
		}
	}
}

func TestWaitSubmission(t *testing.T) {
	if !waitSubmission(context.Background(), 0) {
		t.Fatal("zero delay should proceed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- waitSubmission(ctx, time.Hour) }()
	cancel()
	select {
	case proceeded := <-done:
		if proceeded {
			t.Fatal("cancelled wait proceeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt wait")
	}
	if waitSubmission(ctx, 0) {
		t.Fatal("cancelled zero-delay wait proceeded")
	}
	if !waitSubmission(context.Background(), time.Millisecond) {
		t.Fatal("elapsed delay should proceed")
	}
}
