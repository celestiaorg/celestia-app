package queued

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---

// fakeSigner produces deterministic tx bytes/hash and assigns sequence
// numbers from a counter. It records every call so tests can assert.
type fakeSigner struct {
	mu   sync.Mutex
	next uint64
	err  error // if non-nil, Sign returns this for the next call
	logs []string
}

func newFakeSigner(start uint64) *fakeSigner {
	return &fakeSigner{next: start}
}

func (s *fakeSigner) Sign(_ context.Context, req *TxRequest, seq uint64) ([]byte, string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		e := s.err
		s.err = nil
		return nil, "", 0, e
	}
	// Honor the sequence queued dictates (fix A: queued is authoritative). It tracks
	// the buffer's expected sequence in lockstep with next.
	s.next = seq + 1
	hash := fmt.Sprintf("hash-%d", seq)
	bytes := []byte(hash)
	s.logs = append(s.logs, fmt.Sprintf("sign:%d", seq))
	_ = req
	return bytes, hash, seq, nil
}

// fakeBroadcaster records every submit and serves canned Status responses.
type fakeBroadcaster struct {
	mu sync.Mutex

	// submitErr returns the given error for the next submit at the matching seq.
	submitErr map[uint64]error
	// submitHold lets a test block a specific seq's Submit until it closes the chan.
	submitHold map[uint64]chan struct{}

	// status maps txHash -> sequence of responses (consumed in order).
	status   map[string][]*tx.TxStatusResponse
	statusFn func(hash string) (*tx.TxStatusResponse, error)

	submitted []uint64
	submits   atomic.Int64
}

func newFakeBroadcaster() *fakeBroadcaster {
	return &fakeBroadcaster{
		submitErr:  make(map[uint64]error),
		submitHold: make(map[uint64]chan struct{}),
		status:     make(map[string][]*tx.TxStatusResponse),
	}
}

func (b *fakeBroadcaster) Submit(_ context.Context, txBytes []byte) error {
	b.mu.Lock()
	b.submits.Add(1)
	// derive sequence from canned bytes "hash-N"
	hash := string(txBytes)
	var seq uint64
	if _, err := fmt.Sscanf(hash, "hash-%d", &seq); err != nil {
		b.mu.Unlock()
		return fmt.Errorf("unexpected tx bytes: %q", hash)
	}
	hold := b.submitHold[seq]
	errOnReturn, hasErr := b.submitErr[seq]
	if hasErr {
		delete(b.submitErr, seq)
	}
	if !hasErr {
		b.submitted = append(b.submitted, seq)
	}
	b.mu.Unlock()

	if hold != nil {
		<-hold
	}
	if hasErr {
		return errOnReturn
	}
	return nil
}

func (b *fakeBroadcaster) Status(_ context.Context, hash string) (*tx.TxStatusResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.statusFn != nil {
		return b.statusFn(hash)
	}
	queue, ok := b.status[hash]
	if !ok || len(queue) == 0 {
		return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
	}
	resp := queue[0]
	if len(queue) > 1 {
		b.status[hash] = queue[1:]
	} else {
		b.status[hash] = nil
	}
	return resp, nil
}

func (b *fakeBroadcaster) setStatus(hash string, resps ...*tx.TxStatusResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status[hash] = append(b.status[hash], resps...)
}

func committedResp() *tx.TxStatusResponse {
	return &tx.TxStatusResponse{
		Status:        core.TxStatusCommitted,
		Height:        100,
		ExecutionCode: abci.CodeTypeOK,
	}
}

// --- helpers ---

func newTestWorker(t *testing.T, startSeq uint64) (*worker, *fakeSigner, *fakeBroadcaster, chan<- *TxRequest, func()) {
	t.Helper()
	sig := newFakeSigner(startSeq)
	bro := newFakeBroadcaster()
	requestCh := make(chan *TxRequest, 32)
	w := &worker{
		signer:      sig,
		broadcaster: bro,
		buffer:      newTxBuffer(startSeq),
		requestCh:   requestCh,
		events:      make(chan event, 8),
		pollTime:    10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()
	stop := func() {
		cancel()
		<-done
	}
	return w, sig, bro, requestCh, stop
}

func enqueueRequest(ctx context.Context, ch chan<- *TxRequest) *TxHandle {
	req, handle := newTxHandle(ctx, nil, nil, nil)
	ch <- req
	return handle
}

func awaitWithTimeout(t *testing.T, h *TxHandle, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	_, err := h.Await(ctx)
	return err
}

// --- handle() / state machine tests (pure, no goroutines) ---

func TestHandle_NewRequest_QueuesAndPlansSign(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(1),
		mode:   modeSubmitting,
	}
	req, _ := newTxHandle(context.Background(), nil, nil, nil)
	cmds := w.handle(evNewRequest{req: req})

	require.Len(t, cmds, 1)
	_, ok := cmds[0].(cmdSign)
	assert.True(t, ok, "first command should be cmdSign")
	assert.True(t, w.signing)
}

func TestHandle_StoppedMode_RejectsNewRequest(t *testing.T) {
	stopErr := errors.New("stopped")
	w := &worker{
		buffer:  newTxBuffer(1),
		mode:    modeStopped,
		stopErr: stopErr,
	}
	req, _ := newTxHandle(context.Background(), nil, nil, nil)
	cmds := w.handle(evNewRequest{req: req})

	require.Len(t, cmds, 1)
	fin, ok := cmds[0].(cmdFinalize)
	require.True(t, ok)
	assert.ErrorIs(t, fin.err, stopErr)
	assert.Equal(t, 0, w.buffer.pendingLen())
}

func TestHandle_OneInFlightSign(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(1),
		mode:   modeSubmitting,
	}
	req1, _ := newTxHandle(context.Background(), nil, nil, nil)
	req2, _ := newTxHandle(context.Background(), nil, nil, nil)

	cmds := w.handle(evNewRequest{req: req1})
	assert.Len(t, cmds, 1, "first request → one sign")

	cmds = w.handle(evNewRequest{req: req2})
	assert.Empty(t, cmds, "second request must NOT start another sign while one is in flight")
	assert.Equal(t, 1, w.buffer.pendingLen(), "req2 is queued")
}

func TestHandle_SignResult_AppendsAndSubmits(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(5),
		mode:   modeSubmitting,
	}
	req, _ := newTxHandle(context.Background(), nil, nil, nil)
	w.signing = true
	w.buffer.addPending(req)
	_ = w.buffer.popPending() // mimic plan() having taken it

	cmds := w.handle(evSignResult{
		req:   req,
		bytes: []byte("hash-5"),
		hash:  "hash-5",
		seq:   5,
	})

	require.NotEmpty(t, cmds)
	_, hasSubmit := cmds[0].(cmdSubmit)
	assert.True(t, hasSubmit, "should immediately submit the freshly signed entry")
	assert.False(t, w.signing)
	assert.True(t, w.submitting)
	assert.Equal(t, 1, w.buffer.signedLen())
}

func TestHandle_SignResult_Failure_Finalizes(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(5),
		mode:   modeSubmitting,
	}
	req, _ := newTxHandle(context.Background(), nil, nil, nil)
	w.signing = true

	cmds := w.handle(evSignResult{req: req, err: errors.New("boom")})
	require.NotEmpty(t, cmds)
	fin, ok := cmds[0].(cmdFinalize)
	require.True(t, ok)
	assert.ErrorContains(t, fin.err, "signing tx")
	assert.False(t, w.signing)
	assert.Equal(t, 0, w.buffer.signedLen())
}

func TestHandle_SequenceMismatch_EntersRecovery(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(1),
		mode:   modeSubmitting,
	}
	// Three signed entries; submit the first two.
	for i := uint64(1); i <= 3; i++ {
		require.NoError(t, w.buffer.appendSigned(txEntry{sequence: i, txHash: fmt.Sprintf("h%d", i)}))
		if i <= 2 {
			w.buffer.markSubmitted(i)
		}
	}
	w.submitting = true

	// Node says "expected 3" → recovery target = 3.
	mismatchErr := &user.BroadcastTxError{
		Code:     32, // sdkerrors.ErrWrongSequence
		ErrorLog: "account sequence mismatch, expected 3, got 1: incorrect account sequence",
	}
	_ = w.handle(evSubmitResult{seq: 1, err: mismatchErr})
	assert.Equal(t, modeRecovering, w.mode)
	assert.Equal(t, uint64(3), w.recoverTarget)
}

func TestHandle_FatalSubmit_EntersStop(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(1),
		mode:   modeSubmitting,
	}
	require.NoError(t, w.buffer.appendSigned(txEntry{sequence: 1, txHash: "h1"}))
	w.buffer.markSubmitted(1)
	w.submitting = true

	terminalErr := &user.BroadcastTxError{
		Code:     99,
		ErrorLog: "some terminal error",
	}
	_ = w.handle(evSubmitResult{seq: 1, err: terminalErr})
	assert.Equal(t, modeStopped, w.mode)
	assert.Equal(t, uint64(1), w.stopSeq)
}

func TestAddTx_NilHandleOnClosed(t *testing.T) {
	c := &Client{requestCh: make(chan *TxRequest, 1)}
	c.closed.Store(true)

	h, err := c.AddTx(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, h, "no handle should be returned when the client is closed")
}

func TestAddTx_NilHandleOnFull(t *testing.T) {
	// Unbuffered channel with no reader → enqueue hits the default branch.
	c := &Client{requestCh: make(chan *TxRequest)}

	h, err := c.AddTx(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue is full")
	assert.Nil(t, h, "no handle should be returned when the queue is full")
}

func TestHandle_MempoolFull_RetriesWithBackoff(t *testing.T) {
	const delay = 500 * time.Millisecond
	w := &worker{
		buffer:           newTxBuffer(1),
		mode:             modeSubmitting,
		submitRetryDelay: delay,
	}
	// One signed entry, currently in flight (submitting=true, not yet marked
	// submitted — markSubmitted only happens on success).
	require.NoError(t, w.buffer.appendSigned(txEntry{sequence: 1, txHash: "h1", txBytes: []byte("hash-1")}))
	w.submitting = true

	mempoolErr := &user.BroadcastTxError{Code: 20, ErrorLog: "mempool is full"}
	cmds := w.handle(evSubmitResult{seq: 1, err: mempoolErr})

	// Transient: not fatal, stays submitting.
	assert.Equal(t, modeSubmitting, w.mode)

	// The retry cmdSubmit must carry the backoff delay, then it's consumed.
	var sub *cmdSubmit
	for i := range cmds {
		if c, ok := cmds[i].(cmdSubmit); ok {
			c := c
			sub = &c
		}
	}
	require.NotNil(t, sub, "expected a cmdSubmit retry")
	assert.Equal(t, uint64(1), sub.seq)
	assert.Equal(t, delay, sub.delay, "retry must carry the backoff delay")
	assert.Equal(t, time.Duration(0), w.nextSubmitDelay, "delay should be consumed")
}

func TestHandle_FirstSubmit_NoBackoff(t *testing.T) {
	w := &worker{
		buffer:           newTxBuffer(1),
		mode:             modeSubmitting,
		submitRetryDelay: 500 * time.Millisecond,
	}
	require.NoError(t, w.buffer.appendSigned(txEntry{sequence: 1, txHash: "h1", txBytes: []byte("hash-1")}))

	cmds := w.plan() // fresh submit, no prior error
	var sub *cmdSubmit
	for i := range cmds {
		if c, ok := cmds[i].(cmdSubmit); ok {
			c := c
			sub = &c
		}
	}
	require.NotNil(t, sub)
	assert.Equal(t, time.Duration(0), sub.delay, "first submit must have no backoff")
}

func TestHandle_Committed_FinalizesAndPlans(t *testing.T) {
	w := &worker{
		buffer: newTxBuffer(1),
		mode:   modeSubmitting,
	}
	req, _ := newTxHandle(context.Background(), nil, nil, nil)
	require.NoError(t, w.buffer.appendSigned(txEntry{sequence: 1, txHash: "h1", request: req}))
	w.buffer.markSubmitted(1)
	w.confirming = true

	cmds := w.handle(evConfirmResult{seq: 1, status: committedResp()})

	require.NotEmpty(t, cmds)
	fin, ok := cmds[0].(cmdFinalize)
	require.True(t, ok)
	assert.Equal(t, req, fin.req)
	assert.NoError(t, fin.err)
	assert.Equal(t, "h1", fin.resp.TxHash)
	assert.Equal(t, int64(100), fin.resp.Height)
	assert.False(t, w.confirming)
}

func TestHandle_RecoveryConfirms_ResumesSubmitting(t *testing.T) {
	w := &worker{
		buffer:        newTxBuffer(1),
		mode:          modeRecovering,
		recoverTarget: 3,
	}
	for i := uint64(1); i <= 3; i++ {
		req, _ := newTxHandle(context.Background(), nil, nil, nil)
		require.NoError(t, w.buffer.appendSigned(txEntry{sequence: i, txHash: fmt.Sprintf("h%d", i), request: req}))
		w.buffer.markSubmitted(i)
	}
	w.confirming = true
	_ = w.handle(evConfirmResult{seq: 1, status: committedResp()})
	assert.Equal(t, modeRecovering, w.mode, "still recovering after seq 1")

	w.confirming = true
	_ = w.handle(evConfirmResult{seq: 2, status: committedResp()})
	assert.Equal(t, modeSubmitting, w.mode, "should resume submitting after confirming target-1")
}

// --- integration: full worker driven by fakes ---

func TestWorker_SubmitAndConfirm(t *testing.T) {
	_, sig, bro, ch, stop := newTestWorker(t, 1)
	defer stop()

	for i := range 5 {
		bro.setStatus(fmt.Sprintf("hash-%d", uint64(i)+1), committedResp())
	}

	handles := make([]*TxHandle, 0, 5)
	for range 5 {
		handles = append(handles, enqueueRequest(context.Background(), ch))
	}

	for i, h := range handles {
		err := awaitWithTimeout(t, h, 2*time.Second)
		assert.NoError(t, err, "handle %d should confirm", i)
	}
	assert.Equal(t, uint64(6), sig.next, "5 sequential signs starting at 1")
}

func TestWorker_RecoveryAfterMismatch(t *testing.T) {
	_, sig, bro, ch, stop := newTestWorker(t, 1)
	defer stop()

	// Block the failing submit for seq=1 until all 3 are signed, so the
	// mismatch fires when the buffer already contains seq=3 → recovery (not stop).
	release := make(chan struct{})
	bro.mu.Lock()
	bro.submitHold[1] = release
	bro.submitErr[1] = &user.BroadcastTxError{
		Code:     32, // sdkerrors.ErrWrongSequence
		ErrorLog: "account sequence mismatch, expected 3, got 1: incorrect account sequence",
	}
	bro.mu.Unlock()

	for i := uint64(1); i <= 3; i++ {
		bro.setStatus(fmt.Sprintf("hash-%d", i), committedResp())
	}

	handles := make([]*TxHandle, 0, 3)
	for range 3 {
		handles = append(handles, enqueueRequest(context.Background(), ch))
	}

	// Wait for all 3 to be signed before letting the failing submit proceed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sig.mu.Lock()
		signed := sig.next
		sig.mu.Unlock()
		if signed >= 4 { // started at 1, signed 1/2/3 → next is 4
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)

	for i, h := range handles {
		err := awaitWithTimeout(t, h, 3*time.Second)
		assert.NoError(t, err, "handle %d should still confirm via recovery", i)
	}
}

func TestWorker_FatalRejection_StopsAndDrains(t *testing.T) {
	_, _, bro, ch, stop := newTestWorker(t, 1)
	defer stop()

	// Seq 1 commits, but seq 2 is rejected → stop mode → seq 3 also errors.
	bro.setStatus("hash-1", committedResp())
	bro.setStatus("hash-2", &tx.TxStatusResponse{
		Status: core.TxStatusRejected,
		Error:  "bad signature",
	})

	h1 := enqueueRequest(context.Background(), ch)
	h2 := enqueueRequest(context.Background(), ch)
	h3 := enqueueRequest(context.Background(), ch)

	assert.NoError(t, awaitWithTimeout(t, h1, 2*time.Second))
	assert.Error(t, awaitWithTimeout(t, h2, 2*time.Second))
	assert.Error(t, awaitWithTimeout(t, h3, 2*time.Second))
}

func TestWorker_ContextCancelDrains(t *testing.T) {
	_, _, _, ch, stop := newTestWorker(t, 1)

	// Enqueue but don't set status responses — they'll stay pending.
	h1 := enqueueRequest(context.Background(), ch)
	h2 := enqueueRequest(context.Background(), ch)

	time.Sleep(50 * time.Millisecond) // let them get signed + submitted
	stop()

	assert.Error(t, awaitWithTimeout(t, h1, 1*time.Second))
	assert.Error(t, awaitWithTimeout(t, h2, 1*time.Second))
}

// --- tx_client / Close / enqueue tests ---

func TestClose_DrainsQueuedRequests(t *testing.T) {
	// Stand up a Client manually (no v1 wiring needed for this test) with
	// a worker that consumes nothing — every queued request stays in requestCh.
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		requestCh: make(chan *TxRequest, 8),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go func() {
		defer close(c.done)
		<-ctx.Done() // worker stub: just block until cancel.
	}()

	handles := make([]*TxHandle, 0, 3)
	for range 3 {
		req, h := newTxHandle(context.Background(), nil, nil, nil)
		require.NoError(t, c.enqueue(req))
		handles = append(handles, h)
	}

	c.Close()

	for i, h := range handles {
		err := awaitWithTimeout(t, h, 1*time.Second)
		assert.ErrorIs(t, err, errClientClosed, "handle %d should resolve with client-closed", i)
	}

	// Second Close is a no-op.
	c.Close()
}

func TestEnqueue_RejectsAfterClose(t *testing.T) {
	c := &Client{
		requestCh: make(chan *TxRequest, 1),
	}
	c.closed.Store(true)

	req, _ := newTxHandle(context.Background(), nil, nil, nil)
	err := c.enqueue(req)
	assert.ErrorIs(t, err, errClientClosed)
}

func TestEnqueue_QueueFull(t *testing.T) {
	c := &Client{
		requestCh: make(chan *TxRequest, 1),
	}
	req1, _ := newTxHandle(context.Background(), nil, nil, nil)
	require.NoError(t, c.enqueue(req1))

	req2, _ := newTxHandle(context.Background(), nil, nil, nil)
	err := c.enqueue(req2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue is full")
}

// blockingSigner blocks Sign until ctx is cancelled. Used to park a request
// in the "popped from pending, not yet appended to signed" window so the
// drain path can be exercised.
type blockingSigner struct {
	started chan struct{}
	once    sync.Once
}

func (s *blockingSigner) Sign(ctx context.Context, _ *TxRequest, _ uint64) ([]byte, string, uint64, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return nil, "", 0, ctx.Err()
}

func TestWorker_DrainAllResolvesInflightSign(t *testing.T) {
	// Regression: when ctx is cancelled while a request is in the signer
	// (popped from pending, not yet appended to signed), drainAll must
	// resolve it. Without inflightSign tracking, Await would hang forever.
	sigStarted := make(chan struct{})
	sig := &blockingSigner{started: sigStarted}
	requestCh := make(chan *TxRequest, 1)
	w := &worker{
		signer:      sig,
		broadcaster: newFakeBroadcaster(),
		buffer:      newTxBuffer(1),
		requestCh:   requestCh,
		events:      make(chan event, 8),
		pollTime:    10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()

	h := enqueueRequest(context.Background(), requestCh)
	<-sigStarted // signer is now in flight; req is owned only by inflightSign.

	cancel()
	<-done

	err := awaitWithTimeout(t, h, 1*time.Second)
	require.Error(t, err, "in-flight sign req must resolve on shutdown")
}

func TestClose_DrainsWhenWorkerExitsFirst(t *testing.T) {
	// Regression: if the parent ctx is cancelled externally (and the user
	// never calls Close), the requestCh must still drain — NewClient's
	// worker goroutine invokes Close on its way out for exactly this reason.
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		requestCh: make(chan *TxRequest, 8),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	// Mirror NewClient: worker calls Close on exit as a safety net.
	go func() {
		<-ctx.Done()
		close(c.done)
		c.Close()
	}()

	handles := make([]*TxHandle, 0, 3)
	for range 3 {
		req, h := newTxHandle(context.Background(), nil, nil, nil)
		require.NoError(t, c.enqueue(req))
		handles = append(handles, h)
	}

	// External cancel — user never invokes Close themselves.
	cancel()

	for i, h := range handles {
		err := awaitWithTimeout(t, h, 1*time.Second)
		assert.ErrorIs(t, err, errClientClosed, "handle %d must resolve via worker-triggered Close", i)
	}
}
