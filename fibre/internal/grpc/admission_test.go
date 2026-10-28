package grpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testMsg = int64(100)

// testAdmission is a small admission config for tests that do not exercise it.
var testAdmission = AdmissionConfig{Budget: 64 << 20, MaxMessage: 4 << 20}

// withShortWait shortens the admission wait so a budget timeout fires fast.
func withShortWait(t *testing.T) {
	t.Helper()
	orig := admissionWait
	admissionWait = 100 * time.Millisecond
	t.Cleanup(func() { admissionWait = orig })
}

// rejectLog records OnReject calls as "method/reason".
type rejectLog struct {
	mu  sync.Mutex
	got []string
}

func (l *rejectLog) onReject(_ context.Context, method, reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, method+"/"+reason)
}

func (l *rejectLog) reasons() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

func newTestAdmission(t *testing.T, budget int64, log *rejectLog) *admission {
	t.Helper()
	cfg := AdmissionConfig{Budget: budget, MaxMessage: testMsg}
	if log != nil {
		cfg.OnReject = log.onReject
	}
	a, err := newAdmission(cfg)
	require.NoError(t, err)
	return a
}

// requireRetryable asserts ResourceExhausted with a positive RetryInfo hint.
func requireRetryable(t *testing.T, err error) {
	t.Helper()
	require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			require.Positive(t, ri.GetRetryDelay().AsDuration())
			return
		}
	}
	t.Fatal("expected RetryInfo")
}

func TestNewAdmissionRejectsBudgetBelowOneMessage(t *testing.T) {
	_, err := newAdmission(AdmissionConfig{Budget: testMsg - 1, MaxMessage: testMsg})
	require.Error(t, err)
	_, err = newAdmission(AdmissionConfig{Budget: testMsg})
	require.Error(t, err)
}

// TestAdmissionRejections covers each rejection reason: ResourceExhausted with a
// RetryInfo hint, reported to OnReject with the method.
func TestAdmissionRejections(t *testing.T) {
	cases := map[string]func(t *testing.T, a *admission) error{
		"per_ip": func(t *testing.T, a *admission) error {
			for range admissionPerIP {
				_, err := a.admit(t.Context(), "m", "1.1.1.1")
				require.NoError(t, err)
			}
			_, err := a.admit(t.Context(), "m", "1.1.1.1")
			return err
		},
		"memory_timeout": func(t *testing.T, a *admission) error {
			withShortWait(t)
			_, err := a.admit(t.Context(), "m", "")
			require.NoError(t, err)
			_, err = a.admit(t.Context(), "m", "")
			return err
		},
		"memory_overflow": func(t *testing.T, a *admission) error {
			_, err := a.admit(t.Context(), "m", "")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			var wg sync.WaitGroup
			for range admissionWaiters {
				wg.Go(func() { _, _ = a.admit(ctx, "m", "") })
			}
			t.Cleanup(func() { cancel(); wg.Wait() })
			require.Eventually(t, func() bool { return a.waiting.Load() == admissionWaiters }, 2*time.Second, 5*time.Millisecond)
			_, err = a.admit(t.Context(), "m", "")
			return err
		},
	}
	for reason, run := range cases {
		t.Run(reason, func(t *testing.T) {
			log := &rejectLog{}
			a := newTestAdmission(t, 100*testMsg, log)
			if reason != "per_ip" {
				a = newTestAdmission(t, testMsg, log) // budget fits one
			}
			requireRetryable(t, run(t, a))
			require.Equal(t, []string{"m/" + reason}, log.reasons())
		})
	}
}

// TestAdmissionClientCancel: a cancel while waiting returns the context's code
// and is not counted as a rejection.
func TestAdmissionClientCancel(t *testing.T) {
	log := &rejectLog{}
	a := newTestAdmission(t, testMsg, log)
	_, err := a.admit(t.Context(), "m", "")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := a.admit(ctx, "m", ""); done <- err }()
	require.Eventually(t, func() bool { return a.waiting.Load() == 1 }, time.Second, 5*time.Millisecond)

	cancel()
	require.Equal(t, grpccodes.Canceled, status.Code(<-done))
	require.Empty(t, log.reasons())
}

// TestAdmissionReceivedAndRelease: received shrinks to the decoded size, freeing
// budget for another request, and release wakes a waiter.
func TestAdmissionReceivedAndRelease(t *testing.T) {
	a := newTestAdmission(t, testMsg+testMsg/2, nil) // fits one max, not two
	rA, err := a.admit(t.Context(), "m", "")
	require.NoError(t, err)
	rA.received(10)
	require.EqualValues(t, 10, a.reserved.Load())

	rB, err := a.admit(t.Context(), "m", "")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		r, err := a.admit(t.Context(), "m", "")
		if err == nil {
			r.release()
		}
		done <- err
	}()
	require.Eventually(t, func() bool { return a.waiting.Load() == 1 }, time.Second, 5*time.Millisecond)
	rB.release()
	require.NoError(t, <-done)
	rA.release()
	rA.release() // idempotent
	require.Zero(t, a.reserved.Load())
}

func TestKeyedCounter(t *testing.T) {
	c := newKeyedCounter(2)
	require.True(t, c.tryAcquire("a"))
	require.True(t, c.tryAcquire("a"))
	require.False(t, c.tryAcquire("a"))
	require.True(t, c.tryAcquire("b"))
	c.release("a")
	require.True(t, c.tryAcquire("a"))

	require.True(t, newKeyedCounter(0).tryAcquire("x")) // disabled
	require.True(t, newKeyedCounter(2).tryAcquire(""))  // empty key unlimited
}

func TestConnKeyGrouping(t *testing.T) {
	addr := func(ip string, port int) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: port} }
	require.Equal(t, connKey(addr("1.2.3.4", 1)), connKey(addr("1.2.3.4", 2)))
	require.NotEqual(t, connKey(addr("1.2.3.4", 1)), connKey(addr("1.2.3.5", 1)))
	require.Equal(t, connKey(addr("2001:db8:abcd:1::1", 1)), connKey(addr("2001:db8:abcd:1::2", 2)))
	require.NotEqual(t, connKey(addr("2001:db8:abcd:1::1", 1)), connKey(addr("2001:db8:abcd:2::1", 1)))
	require.Equal(t, connKey(addr("1.2.3.4", 1)), connKey(addr("::ffff:1.2.3.4", 2)))
}
