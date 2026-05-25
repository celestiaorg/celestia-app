package v3

import (
	"context"
	"fmt"
	"testing"

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
	assert.Equal(t, ErrUnrecoverable, kind)
}

func TestClassifyBroadcastError_Nil(t *testing.T) {
	kind, _ := ClassifyBroadcastError(nil)
	assert.Equal(t, ErrUnrecoverable, kind)
}

// --- TxBuffer tests ---

func TestTxBuffer_PendingQueue(t *testing.T) {
	buf := newTxBuffer(1)

	assert.Equal(t, 0, buf.pendingLen())
	assert.Nil(t, buf.popPending())

	req1 := &TxRequest{}
	req2 := &TxRequest{}
	buf.addPending(req1)
	buf.addPending(req2)
	assert.Equal(t, 2, buf.pendingLen())

	assert.Equal(t, req1, buf.popPending())
	assert.Equal(t, 1, buf.pendingLen())
	assert.Equal(t, req2, buf.popPending())
	assert.Equal(t, 0, buf.pendingLen())
}

func TestTxBuffer_SequenceContinuity(t *testing.T) {
	buf := newTxBuffer(10)

	require.NoError(t, buf.appendSigned(txEntry{sequence: 10, txHash: "a"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 11, txHash: "b"}))

	// Gap should fail
	err := buf.appendSigned(txEntry{sequence: 15, txHash: "c"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sequence gap")
}

func TestTxBuffer_FrontAndConfirmFront(t *testing.T) {
	buf := newTxBuffer(1)

	assert.Nil(t, buf.front())
	assert.Nil(t, buf.confirmFront())

	require.NoError(t, buf.appendSigned(txEntry{sequence: 1, txHash: "a"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 2, txHash: "b"}))

	front := buf.front()
	require.NotNil(t, front)
	assert.Equal(t, uint64(1), front.sequence)

	confirmed := buf.confirmFront()
	require.NotNil(t, confirmed)
	assert.Equal(t, uint64(1), confirmed.sequence)
	assert.Equal(t, uint64(2), buf.nextSeq)

	front = buf.front()
	require.NotNil(t, front)
	assert.Equal(t, uint64(2), front.sequence)
}

func TestTxBuffer_GetBySequence(t *testing.T) {
	buf := newTxBuffer(5)
	require.NoError(t, buf.appendSigned(txEntry{sequence: 5, txHash: "a"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 6, txHash: "b"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 7, txHash: "c"}))

	entry := buf.getBySequence(6)
	require.NotNil(t, entry)
	assert.Equal(t, "b", entry.txHash)

	assert.Nil(t, buf.getBySequence(4))
	assert.Nil(t, buf.getBySequence(8))
}

func TestTxBuffer_SubmissionTracking(t *testing.T) {
	buf := newTxBuffer(1)
	require.NoError(t, buf.appendSigned(txEntry{sequence: 1, txHash: "a"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 2, txHash: "b"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 3, txHash: "c"}))

	// Nothing submitted yet.
	assert.False(t, buf.hasSubmissions())
	next := buf.next()
	require.NotNil(t, next)
	assert.Equal(t, uint64(1), next.sequence)

	// Submit first two.
	buf.markSubmitted(1)
	buf.markSubmitted(2)
	assert.True(t, buf.hasSubmissions())
	assert.Equal(t, uint64(2), buf.lastSubmittedSeq())

	next = buf.next()
	require.NotNil(t, next)
	assert.Equal(t, uint64(3), next.sequence)

	// Submit all.
	buf.markSubmitted(3)
	assert.Nil(t, buf.next())
}

func TestTxBuffer_Reset(t *testing.T) {
	buf := newTxBuffer(1)
	for i := uint64(1); i <= 5; i++ {
		require.NoError(t, buf.appendSigned(txEntry{sequence: i, txHash: fmt.Sprintf("tx%d", i)}))
		buf.markSubmitted(i)
	}

	buf.reset(3)

	// Seqs 1,2 still submitted; 3,4,5 reset.
	assert.Equal(t, uint64(2), buf.lastSubmittedSeq())
	next := buf.next()
	require.NotNil(t, next)
	assert.Equal(t, uint64(3), next.sequence, "next unsubmitted should be seq 3")
}

func TestTxBuffer_PopPending_ClearsBackingSlot(t *testing.T) {
	// Regression guard: popPending must zero the vacated slot so the backing
	// array doesn't pin the TxRequest (memory leak in long-running clients).
	buf := newTxBuffer(1)
	req := &TxRequest{}
	buf.addPending(req)
	// Alias the backing array so we can inspect the slot after the pop.
	underlying := buf.pending[:1:1]

	popped := buf.popPending()
	assert.Same(t, req, popped)
	assert.Nil(t, underlying[0], "popped slot must be nil-ed to release reference")
}

func TestTxBuffer_ConfirmFront_ClearsBackingSlot(t *testing.T) {
	// Regression guard: confirmFront must zero the vacated slot so the backing
	// array doesn't pin txBytes / request.
	buf := newTxBuffer(5)
	require.NoError(t, buf.appendSigned(txEntry{
		sequence: 5,
		txHash:   "x",
		txBytes:  []byte("bytes"),
		request:  &TxRequest{},
	}))
	underlying := buf.signed[:1:1]

	_ = buf.confirmFront()
	assert.Equal(t, txEntry{}, underlying[0], "confirmed slot must be zeroed")
}

func TestTxBuffer_StartSeqZero(t *testing.T) {
	// Brand-new account: startSeq=0 used to underflow nextSeq via "confirmed-1".
	buf := newTxBuffer(0)

	require.NoError(t, buf.appendSigned(txEntry{sequence: 0, txHash: "a"}))
	require.NoError(t, buf.appendSigned(txEntry{sequence: 1, txHash: "b"}))

	front := buf.front()
	require.NotNil(t, front)
	assert.Equal(t, uint64(0), front.sequence)

	buf.markSubmitted(0)
	assert.True(t, buf.hasSubmissions())
	assert.Equal(t, uint64(0), buf.lastSubmittedSeq())

	confirmed := buf.confirmFront()
	require.NotNil(t, confirmed)
	assert.Equal(t, uint64(1), buf.nextSeq)
}

// --- TxHandle tests ---

func TestTxHandle_Await(t *testing.T) {
	req, handle := newTxHandle(context.Background(), nil, nil, nil)

	go req.resolve(nil, fmt.Errorf("test error"))

	resp, err := handle.Await(context.Background())
	assert.Nil(t, resp)
	assert.EqualError(t, err, "test error")

	// Subsequent calls return the same result without blocking.
	resp2, err2 := handle.Await(context.Background())
	assert.Nil(t, resp2)
	assert.EqualError(t, err2, "test error")
}

func TestTxHandle_AwaitContextCancel(t *testing.T) {
	_, handle := newTxHandle(context.Background(), nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := handle.Await(ctx)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- computeTxHash tests ---

func TestComputeTxHash(t *testing.T) {
	txBytes := []byte("test transaction bytes")
	hash := computeTxHash(txBytes)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA256 hex = 64 chars

	hash2 := computeTxHash(txBytes)
	assert.Equal(t, hash, hash2)

	hash3 := computeTxHash([]byte("different bytes"))
	assert.NotEqual(t, hash, hash3)
}
