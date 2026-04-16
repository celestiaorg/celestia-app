package v3

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- Error classification tests ---

func TestClassifyBroadcastError_NonceMismatch(t *testing.T) {
	err := &user.BroadcastTxError{
		TxHash:   "abc",
		Code:     32, // sdkerrors.ErrWrongSequence
		ErrorLog: "account sequence mismatch, expected 5, got 3: incorrect account sequence",
	}
	kind, seq := ClassifyBroadcastError(err)
	assert.Equal(t, ErrSequenceMismatch, kind)
	assert.Equal(t, uint64(5), seq)
}

func TestClassifyBroadcastError_MempoolFull(t *testing.T) {
	err := &user.BroadcastTxError{
		TxHash:   "abc",
		Code:     1,
		ErrorLog: "mempool is full",
	}
	kind, seq := ClassifyBroadcastError(err)
	assert.Equal(t, ErrMempoolFull, kind)
	assert.Equal(t, uint64(0), seq)
}

func TestClassifyBroadcastError_TxInCache(t *testing.T) {
	err := &user.BroadcastTxError{
		TxHash:   "abc",
		Code:     1,
		ErrorLog: "tx already exists in cache",
	}
	kind, seq := ClassifyBroadcastError(err)
	assert.Equal(t, ErrTxInMempoolCache, kind)
	assert.Equal(t, uint64(0), seq)
}

func TestClassifyBroadcastError_NetworkError(t *testing.T) {
	err := status.Error(codes.Unavailable, "connection refused")
	kind, _ := ClassifyBroadcastError(err)
	assert.Equal(t, ErrNetworkError, kind)

	err = status.Error(codes.DeadlineExceeded, "timeout")
	kind, _ = ClassifyBroadcastError(err)
	assert.Equal(t, ErrNetworkError, kind)
}

func TestClassifyBroadcastError_Terminal(t *testing.T) {
	err := &user.BroadcastTxError{
		TxHash:   "abc",
		Code:     99,
		ErrorLog: "some unknown error",
	}
	kind, _ := ClassifyBroadcastError(err)
	assert.Equal(t, ErrTerminal, kind)
}

func TestClassifyBroadcastError_Nil(t *testing.T) {
	kind, _ := ClassifyBroadcastError(nil)
	assert.Equal(t, ErrTerminal, kind)
}

// --- TxBuffer tests ---

func TestTxBuffer_PendingQueue(t *testing.T) {
	buf := NewTxBuffer(1)

	// Empty buffer
	assert.Equal(t, 0, buf.PendingLen())
	assert.Nil(t, buf.PopPending())

	// Add and pop
	req1 := &TxRequest{}
	req2 := &TxRequest{}
	buf.AddPending(req1)
	buf.AddPending(req2)
	assert.Equal(t, 2, buf.PendingLen())

	popped := buf.PopPending()
	assert.Equal(t, req1, popped)
	assert.Equal(t, 1, buf.PendingLen())

	popped = buf.PopPending()
	assert.Equal(t, req2, popped)
	assert.Equal(t, 0, buf.PendingLen())
}

func TestTxBuffer_SequenceContinuity(t *testing.T) {
	buf := NewTxBuffer(10)

	// NextSequence should start at 10
	assert.Equal(t, uint64(10), buf.NextSequence())
	assert.Equal(t, uint64(11), buf.NextSequence())

	// Append signed with correct sequence
	err := buf.AppendSigned(txEntry{sequence: 10, txHash: "a", submittedTo: make(map[int]bool)})
	require.NoError(t, err)

	err = buf.AppendSigned(txEntry{sequence: 11, txHash: "b", submittedTo: make(map[int]bool)})
	require.NoError(t, err)

	// Gap should fail
	err = buf.AppendSigned(txEntry{sequence: 15, txHash: "c", submittedTo: make(map[int]bool)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sequence gap")
}

func TestTxBuffer_FrontAndConfirmFront(t *testing.T) {
	buf := NewTxBuffer(1)

	assert.Nil(t, buf.Front())
	assert.Nil(t, buf.ConfirmFront())

	require.NoError(t, buf.AppendSigned(txEntry{sequence: 1, txHash: "a", submittedTo: make(map[int]bool)}))
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 2, txHash: "b", submittedTo: make(map[int]bool)}))

	front := buf.Front()
	require.NotNil(t, front)
	assert.Equal(t, uint64(1), front.sequence)

	confirmed := buf.ConfirmFront()
	require.NotNil(t, confirmed)
	assert.Equal(t, uint64(1), confirmed.sequence)
	assert.Equal(t, uint64(1), buf.LastConfirmed())

	front = buf.Front()
	require.NotNil(t, front)
	assert.Equal(t, uint64(2), front.sequence)
}

func TestTxBuffer_GetByHash(t *testing.T) {
	buf := NewTxBuffer(1)
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 1, txHash: "abc", submittedTo: make(map[int]bool)}))

	entry := buf.GetByHash("abc")
	require.NotNil(t, entry)
	assert.Equal(t, uint64(1), entry.sequence)

	assert.Nil(t, buf.GetByHash("xyz"))
}

func TestTxBuffer_GetBySequence(t *testing.T) {
	buf := NewTxBuffer(5)
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 5, txHash: "a", submittedTo: make(map[int]bool)}))
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 6, txHash: "b", submittedTo: make(map[int]bool)}))
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 7, txHash: "c", submittedTo: make(map[int]bool)}))

	entry := buf.GetBySequence(6)
	require.NotNil(t, entry)
	assert.Equal(t, "b", entry.txHash)

	assert.Nil(t, buf.GetBySequence(4))
	assert.Nil(t, buf.GetBySequence(8))
}

func TestTxBuffer_EntriesInRange(t *testing.T) {
	buf := NewTxBuffer(10)
	for i := uint64(10); i <= 15; i++ {
		require.NoError(t, buf.AppendSigned(txEntry{
			sequence:    i,
			txHash:      fmt.Sprintf("tx%d", i),
			submittedTo: make(map[int]bool),
		}))
	}

	entries := buf.EntriesInRange(11, 13)
	require.Len(t, entries, 3)
	assert.Equal(t, uint64(11), entries[0].sequence)
	assert.Equal(t, uint64(13), entries[2].sequence)

	// Out of range
	assert.Nil(t, buf.EntriesInRange(20, 25))
	assert.Nil(t, buf.EntriesInRange(1, 5))
}

func TestTxBuffer_Rollback(t *testing.T) {
	buf := NewTxBuffer(1)
	for i := uint64(1); i <= 5; i++ {
		require.NoError(t, buf.AppendSigned(txEntry{
			sequence:    i,
			txHash:      fmt.Sprintf("tx%d", i),
			request:     &TxRequest{},
			submittedTo: make(map[int]bool),
		}))
	}

	removed := buf.RollbackTo(3)
	assert.Len(t, removed, 3) // seq 3, 4, 5
	assert.Equal(t, uint64(3), removed[0].sequence)
	assert.Equal(t, 2, buf.SignedLen())

	// Next sequence should be 3 now
	assert.Equal(t, uint64(3), buf.NextSequence())
}

func TestTxBuffer_SubmittedHashes(t *testing.T) {
	buf := NewTxBuffer(1)
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 1, txHash: "a", submittedTo: map[int]bool{0: true}}))
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 2, txHash: "b", submittedTo: make(map[int]bool)}))
	require.NoError(t, buf.AppendSigned(txEntry{sequence: 3, txHash: "c", submittedTo: map[int]bool{0: true}}))

	hashes := buf.SubmittedHashes(10)
	assert.Equal(t, []string{"a", "c"}, hashes)

	// With limit
	hashes = buf.SubmittedHashes(1)
	assert.Equal(t, []string{"a"}, hashes)
}

// --- NodeConnection tests ---

func TestNodeConnection_StateTransitions(t *testing.T) {
	node := NewNodeConnection(0, nil)

	// Initially active
	assert.True(t, node.IsAvailable())

	// Mark recovering
	node.MarkRecovering()
	assert.False(t, node.IsAvailable())
	assert.Equal(t, NodeRecovering, node.status)

	// Simulate time passing past retry
	node.retryAfter = time.Now().Add(-1 * time.Second)
	assert.True(t, node.IsAvailable())
	assert.Equal(t, NodeActive, node.status)
}

func TestNodeConnection_MaxFailures(t *testing.T) {
	node := NewNodeConnection(0, nil)

	// Hit max failures
	for i := range maxFailures {
		node.MarkRecovering()
		if i < maxFailures-1 {
			// Reset to active for next iteration
			node.retryAfter = time.Now().Add(-1 * time.Second)
			node.IsAvailable() // triggers Active transition
		}
	}

	assert.Equal(t, NodeStopped, node.status)
	assert.False(t, node.IsAvailable())
}

func TestNodeConnection_SubmissionTracking(t *testing.T) {
	node := NewNodeConnection(0, nil)

	assert.True(t, node.NeedsSubmission(1))
	node.RecordSubmission(1)
	assert.False(t, node.NeedsSubmission(1))
	assert.True(t, node.NeedsSubmission(2))
}

func TestNodeConnection_MarkStopped(t *testing.T) {
	node := NewNodeConnection(0, nil)
	node.MarkStopped(fmt.Errorf("terminal error"))
	assert.Equal(t, NodeStopped, node.status)
	assert.False(t, node.IsAvailable())
}

// --- TxHandle tests ---

func TestNewTxHandle(t *testing.T) {
	req, handle := newTxHandle(context.Background(), nil, nil, nil)

	// Verify channels are connected
	req.signedCh <- SignedResult{TxHash: "abc", Sequence: 1}
	result := <-handle.Signed
	assert.Equal(t, "abc", result.TxHash)
	assert.Equal(t, uint64(1), result.Sequence)

	req.submittedCh <- SubmittedResult{TxHash: "abc"}
	subResult := <-handle.Submitted
	assert.Equal(t, "abc", subResult.TxHash)

	req.confirmedCh <- ConfirmedResult{Response: nil, Err: fmt.Errorf("test error")}
	confResult := <-handle.Confirmed
	assert.Error(t, confResult.Err)
}

// --- computeTxHash tests ---

func TestComputeTxHash(t *testing.T) {
	txBytes := []byte("test transaction bytes")
	hash := computeTxHash(txBytes)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA256 hex = 64 chars

	// Same input should produce same hash
	hash2 := computeTxHash(txBytes)
	assert.Equal(t, hash, hash2)

	// Different input should produce different hash
	hash3 := computeTxHash([]byte("different bytes"))
	assert.NotEqual(t, hash, hash3)
}
