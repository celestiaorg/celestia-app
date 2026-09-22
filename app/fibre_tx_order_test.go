package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOrderFibreTxsByAge pins the two properties the ordering has to hold: the
// oldest promises come first, and one signer's transactions never get reordered
// against each other.
func TestOrderFibreTxsByAge(t *testing.T) {
	base := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }

	tests := map[string]struct {
		txs  []fibreTxByAge
		want []string
	}{
		"single signer keeps mempool order": {
			txs: []fibreTxByAge{
				{raw: []byte("a"), signer: "alice", created: at(0)},
				{raw: []byte("b"), signer: "alice", created: at(5)},
				{raw: []byte("c"), signer: "alice", created: at(10)},
			},
			want: []string{"a", "b", "c"},
		},
		"signers ordered by their oldest promise": {
			txs: []fibreTxByAge{
				{raw: []byte("newest"), signer: "carol", created: at(30)},
				{raw: []byte("oldest"), signer: "alice", created: at(1)},
				{raw: []byte("middle"), signer: "bob", created: at(10)},
			},
			want: []string{"oldest", "middle", "newest"},
		},
		"a signer's sequence order survives an out-of-order promise": {
			// alice's second tx carries an older promise than her first. She
			// must still be sent in sequence order, or the ante handler drops
			// everything after the first.
			txs: []fibreTxByAge{
				{raw: []byte("a1"), signer: "alice", created: at(20)},
				{raw: []byte("a2"), signer: "alice", created: at(5)},
				{raw: []byte("b1"), signer: "bob", created: at(10)},
			},
			want: []string{"a1", "a2", "b1"},
		},
		"equal timestamps keep first-appearance order": {
			txs: []fibreTxByAge{
				{raw: []byte("b"), signer: "bob", created: at(7)},
				{raw: []byte("a"), signer: "alice", created: at(7)},
			},
			want: []string{"b", "a"},
		},
		"empty input": {
			txs:  nil,
			want: []string{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := orderFibreTxsByAge(tc.txs)

			raw := make([]string, 0, len(got))
			for _, tx := range got {
				raw = append(raw, string(tx))
			}
			require.Equal(t, tc.want, raw)
		})
	}
}
