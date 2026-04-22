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
	assert.Equal(t, ErrTerminal, kind)
}

func TestClassifyBroadcastError_Nil(t *testing.T) {
	kind, _ := ClassifyBroadcastError(nil)
	assert.Equal(t, ErrTerminal, kind)
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
	assert.Equal(t, uint64(1), buf.lastConfirmed())

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
	assert.Equal(t, uint64(0), buf.lastSubmittedSeq())
	next := buf.next()
	require.NotNil(t, next)
	assert.Equal(t, uint64(1), next.sequence)

	// Submit first two.
	buf.signed[0].submitted = true
	buf.signed[1].submitted = true
	assert.Equal(t, uint64(2), buf.lastSubmittedSeq())

	next = buf.next()
	require.NotNil(t, next)
	assert.Equal(t, uint64(3), next.sequence)

	// Submit all.
	buf.signed[2].submitted = true
	assert.Nil(t, buf.next())
}

func TestTxBuffer_Reset(t *testing.T) {
	buf := newTxBuffer(1)
	for i := uint64(1); i <= 5; i++ {
		require.NoError(t, buf.appendSigned(txEntry{sequence: i, txHash: fmt.Sprintf("tx%d", i), submitted: true}))
	}

	buf.reset(3)

	// Seqs 1,2 still submitted; 3,4,5 reset.
	assert.True(t, buf.signed[0].submitted)
	assert.True(t, buf.signed[1].submitted)
	assert.False(t, buf.signed[2].submitted)
	assert.False(t, buf.signed[3].submitted)
	assert.False(t, buf.signed[4].submitted)
	assert.Equal(t, uint64(2), buf.lastSubmittedSeq())
}

// --- TxHandle tests ---

func TestNewTxHandle(t *testing.T) {
	req, handle := newTxHandle(context.Background(), nil, nil, nil)

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

	hash2 := computeTxHash(txBytes)
	assert.Equal(t, hash, hash2)

	hash3 := computeTxHash([]byte("different bytes"))
	assert.NotEqual(t, hash, hash3)
}
