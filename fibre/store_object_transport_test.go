package fibre

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestObjectDialAggregateLimit(t *testing.T) {
	slots := make(chan struct{}, 2)
	var peers []net.Conn
	var mu sync.Mutex
	dial := func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		mu.Lock()
		peers = append(peers, b)
		mu.Unlock()
		return a, nil
	}
	defer func() {
		for _, p := range peers {
			_ = p.Close()
		}
	}()
	// Separate clients and destinations must use the same limit.
	first := limitObjectDial(slots, dial)
	second := limitObjectDial(slots, dial)
	a, err := first(context.Background(), "tcp", "bucket-a:443")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := second(context.Background(), "tcp", "bucket-b:443")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := second(ctx, "tcp", "bucket-c:443"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("full pool: %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("slots = %d", len(slots))
	}
	_ = a.Close()
	_ = a.Close()
	if len(slots) != 1 {
		t.Fatalf("double close released extra slot: %d", len(slots))
	}
	c, err := second(context.Background(), "tcp", "bucket-c:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	_ = b.Close()
	if len(slots) != 0 {
		t.Fatalf("leaked slots: %d", len(slots))
	}
}

func TestObjectDialFailureReleasesSlot(t *testing.T) {
	slots := make(chan struct{}, 1)
	want := errors.New("dial failed")
	dial := limitObjectDial(slots, func(context.Context, string, string) (net.Conn, error) { return nil, want })
	if _, err := dial(context.Background(), "tcp", "bucket:443"); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if len(slots) != 0 {
		t.Fatalf("failed dial leaked slot: %d", len(slots))
	}
}
