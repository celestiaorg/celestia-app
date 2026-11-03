package grpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/stretchr/testify/require"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// admissionOpWidth is the fixed width of one admission op record.
const admissionOpWidth = 4

// modelReservation mirrors a reservation's held bytes and per-IP slot.
type modelReservation struct {
	res      *reservation
	ip       string
	held     int64
	recvDone bool
}

// FuzzAdmissionSequence drives the admission limiter with sequences of admits,
// receives and releases, including repeated and out-of-order calls, from up to
// ten source IPs (one unkeyed). A model tracks each reservation's held bytes
// and per-IP slot. After every operation the limiter must agree with the model:
// admit succeeds exactly when the IP has a free slot and the budget fits one
// more max-size reservation, the reserved total matches the held bytes, and the
// per-IP counts match. After releasing everything the full budget is free.
func FuzzAdmissionSequence(f *testing.F) {
	prevWait := admissionWait
	admissionWait = 0 // reject at once instead of waiting for budget
	f.Cleanup(func() { admissionWait = prevWait })

	op := func(kind, ip, idx, size byte) []byte { return []byte{kind, ip, idx, size} }
	f.Add(uint8(4), uint8(3), []byte{})
	f.Add(uint8(4), uint8(3), fuzzutil.Records(admissionOpWidth,
		op(0, 1, 0, 0), op(0, 1, 0, 0), op(1, 0, 0, 2), op(0, 2, 0, 0), op(0, 2, 0, 0), op(2, 0, 1, 0), op(2, 0, 1, 0)))
	// Nine requests from one IP: the ninth hits the per-IP cap until one is received.
	var perIP [][]byte
	for range 9 {
		perIP = append(perIP, op(0, 5, 0, 0))
	}
	perIP = append(perIP, op(1, 0, 0, 0), op(0, 5, 0, 0))
	f.Add(uint8(1), uint8(63), fuzzutil.Records(admissionOpWidth, perIP...))
	// Unkeyed requests are never capped per IP; received with a size above the
	// reservation or negative must not grow it.
	f.Add(uint8(8), uint8(1), fuzzutil.Records(admissionOpWidth,
		op(0, 0, 0, 0), op(0, 0, 0, 0), op(1, 0, 0, 0xff), op(1, 0, 1, 0x80), op(2, 0, 0, 0), op(0, 0, 0, 0)))

	f.Fuzz(func(t *testing.T, maxMessage, extraSlots uint8, ops []byte) {
		reservationSize := int64(maxMessage%64) + 1
		budget := reservationSize * (int64(extraSlots%16) + 1)
		budget += int64(extraSlots>>4) % reservationSize // a partial slot that never fits a request
		var rejects int
		a, err := newAdmission(AdmissionConfig{
			Budget:     budget,
			MaxMessage: reservationSize,
			OnReject:   func(context.Context, string, string) { rejects++ },
		})
		require.NoError(t, err)

		ips := []string{""}
		for i := range 9 {
			ips = append(ips, fmt.Sprintf("10.0.0.%d", i))
		}
		var live []*modelReservation
		modelReserved := func() int64 {
			var n int64
			for _, m := range live {
				n += m.held
			}
			return n
		}
		modelSlots := func(ip string) int {
			n := 0
			for _, m := range live {
				if m.ip == ip && !m.recvDone {
					n++
				}
			}
			return n
		}

		c := fuzzutil.New(ops)
		for n := 0; n < 256 && !c.Exhausted(); n++ {
			r := c.Record(admissionOpWidth)
			kind, ipIdx, idx, size := r.Byte()%3, r.Byte(), int(r.Byte()), r.Byte()
			switch kind {
			case 0: // admit
				ip := ips[int(ipIdx)%len(ips)]
				wantOK := (ip == "" || modelSlots(ip) < admissionPerIP) && modelReserved()+reservationSize <= budget
				before := rejects
				res, err := a.admit(context.Background(), "Upload", ip)
				if !wantOK {
					require.Error(t, err, "admitted beyond the per-IP cap or budget")
					require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))
					require.Equal(t, before+1, rejects, "rejection not reported")
					continue
				}
				require.NoError(t, err)
				live = append(live, &modelReservation{res: res, ip: ip, held: reservationSize})
			case 1: // received, with a size that may be negative or exceed the reservation
				if len(live) == 0 {
					continue
				}
				m := live[idx%len(live)]
				got := int64(int8(size))
				if size&0x80 == 0 {
					got = int64(size) % (reservationSize + 2)
				}
				m.res.received(got)
				m.recvDone = true
				if got >= 0 && got < m.held {
					m.held = got
				}
			case 2: // release, possibly again
				if len(live) == 0 {
					continue
				}
				m := live[idx%len(live)]
				m.res.release()
				m.recvDone = true
				m.held = 0
			}

			require.Equal(t, modelReserved(), a.reserved.Load(), "reserved bytes diverged from held reservations")
			require.LessOrEqual(t, a.reserved.Load(), budget)
			a.perIP.mu.Lock()
			for _, ip := range ips[1:] {
				require.Equal(t, modelSlots(ip), a.perIP.m[ip], "per-IP slots for %s", ip)
			}
			require.NotContains(t, a.perIP.m, "", "unkeyed requests must not take per-IP slots")
			a.perIP.mu.Unlock()
		}

		for _, m := range live {
			m.res.release()
		}
		require.Zero(t, a.reserved.Load())
		require.Empty(t, a.perIP.m)
		require.True(t, a.sem.TryAcquire(budget), "budget leaked")
	})
}
