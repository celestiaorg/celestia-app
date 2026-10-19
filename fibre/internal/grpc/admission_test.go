package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testMsg = int64(100)

func newTestAdmission(budget, msg int64) *admission {
	return newAdmission(AdmissionConfig{Budget: budget, MaxMessage: msg})
}

// withShortWait shortens the admission wait so a server-side timeout fires fast;
// returns a restore func.
func withShortWait(t *testing.T) func() {
	t.Helper()
	orig := admissionWait
	admissionWait = 100 * time.Millisecond
	return func() { admissionWait = orig }
}

// TestAdmissionRejections table-tests the rejection paths: each returns
// ResourceExhausted with a RetryInfo hint.
func TestAdmissionRejections(t *testing.T) {
	cases := map[string]func(t *testing.T) error{
		"per_ip": func(t *testing.T) error {
			a := newAdmission(AdmissionConfig{Budget: 100 * testMsg, MaxMessage: testMsg})
			for range admissionPerIP {
				_, err := a.admit(context.Background(), "1.1.1.1")
				require.NoError(t, err)
			}
			_, err := a.admit(context.Background(), "1.1.1.1")
			return err
		},
		"memory_timeout": func(t *testing.T) error {
			defer withShortWait(t)()
			a := newTestAdmission(testMsg, testMsg) // budget fits one
			_, err := a.admit(context.Background(), "")
			require.NoError(t, err)
			_, err = a.admit(context.Background(), "") // waits out admissionWait
			return err
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			err := run(t)
			require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))
			st, _ := status.FromError(err)
			var found bool
			for _, d := range st.Details() {
				if ri, ok := d.(*errdetails.RetryInfo); ok {
					require.Positive(t, ri.GetRetryDelay().AsDuration())
					found = true
				}
			}
			require.True(t, found, "expected RetryInfo")
		})
	}
}

// TestAdmissionShrinkFreesBudget: shrinking a reservation returns budget so
// another max-size request fits.
func TestAdmissionShrinkFreesBudget(t *testing.T) {
	a := newTestAdmission(testMsg+testMsg/2, testMsg) // fits one, not two
	rA, err := a.admit(context.Background(), "")
	require.NoError(t, err)
	defer rA.release()

	_, err = a.admit(context.Background(), "")
	require.Error(t, err)

	rA.Shrink(10)
	rB, err := a.admit(context.Background(), "")
	require.NoError(t, err)
	rB.release()
}

// TestAdmissionReleaseThenAdmit: releasing frees budget for a waiter.
func TestAdmissionReleaseThenAdmit(t *testing.T) {
	a := newTestAdmission(testMsg, testMsg)
	rA, err := a.admit(context.Background(), "")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		r, err := a.admit(context.Background(), "")
		if r != nil {
			r.release()
		}
		done <- err
	}()
	require.Eventually(t, func() bool { return a.waiting.Load() == 1 }, time.Second, 5*time.Millisecond)

	rA.release()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("waiter not admitted after release")
	}
}

// TestAdmissionClientCancel: a cancel while waiting returns the context's code.
func TestAdmissionClientCancel(t *testing.T) {
	a := newTestAdmission(testMsg, testMsg)
	rA, err := a.admit(context.Background(), "")
	require.NoError(t, err)
	defer rA.release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := a.admit(ctx, ""); done <- err }()
	require.Eventually(t, func() bool { return a.waiting.Load() == 1 }, time.Second, 5*time.Millisecond)

	cancel()
	require.Equal(t, grpccodes.Canceled, status.Code(<-done))
}

// TestAdmissionDisabled: a nil admission admits everything without a reservation.
func TestAdmissionDisabled(t *testing.T) {
	var a *admission // Budget 0 -> newAdmission returns nil
	require.Nil(t, newAdmission(AdmissionConfig{}))
	r, err := a.admit(context.Background(), "")
	require.NoError(t, err)
	require.Nil(t, r)
	r.Shrink(1) // no-op
	r.release() // no-op
}

// TestStatsAccessors: the metric accessors report live counts and are nil-safe.
func TestStatsAccessors(t *testing.T) {
	var nilAdm *admission
	require.Zero(t, nilAdm.Reserved())
	require.Zero(t, nilAdm.Waiting())

	a := newTestAdmission(10*testMsg, testMsg)
	r, err := a.admit(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, testMsg, a.Reserved())
	r.release()
	require.Zero(t, a.Reserved())

	cl := newConnLimiter(1, 0)
	require.True(t, cl.acquire("a"))
	require.False(t, cl.acquire("b"))
	require.Equal(t, int64(1), cl.OpenConns())
	require.Equal(t, int64(1), cl.RejectedGlobal())
	require.Zero(t, cl.RejectedPerIP())
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
