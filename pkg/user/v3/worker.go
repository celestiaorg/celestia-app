package v3

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// --- events: results posted back to the worker loop. ---

type event interface{ isEvent() }

type evNewRequest struct{ req *TxRequest }

type evSignResult struct {
	req   *TxRequest
	bytes []byte
	hash  string
	seq   uint64
	err   error
}

type evSubmitResult struct {
	seq uint64
	err error
}

type evConfirmResult struct {
	seq    uint64
	status *tx.TxStatusResponse
	err    error
}

type evConfirmTick struct{}

func (evNewRequest) isEvent()    {}
func (evSignResult) isEvent()    {}
func (evSubmitResult) isEvent()  {}
func (evConfirmResult) isEvent() {}
func (evConfirmTick) isEvent()   {}

// --- commands: what the loop tells the dispatcher to do next. ---

type command interface{ isCommand() }

type cmdSign struct{ req *TxRequest }

type cmdSubmit struct {
	seq   uint64
	bytes []byte
}

type cmdConfirm struct {
	seq  uint64
	hash string
}

type cmdFinalize struct {
	req  *TxRequest
	resp *sdktypes.TxResponse
	err  error
}

func (cmdSign) isCommand()     {}
func (cmdSubmit) isCommand()   {}
func (cmdConfirm) isCommand()  {}
func (cmdFinalize) isCommand() {}

// --- mode: which of the three high-level states the worker is in. ---

type workerMode int

const (
	modeSubmitting workerMode = iota
	modeRecovering
	modeStopped
)

// worker orchestrates the async pipeline as a single-goroutine event loop.
//
// All mutable state (buffer, mode, in-flight gates) is owned by the run()
// goroutine. Sign / Submit / Status calls run in their own goroutines and
// post results back as events. handle() is a pure (state, event) → command
// function with no I/O, which keeps the state machine trivially testable.
//
// Invariants:
//   - at most one outstanding sign at a time (signing gate)
//   - at most one outstanding submit at a time (submitting gate)
//   - at most one outstanding confirm at a time (confirming gate)
//   - signed entries are contiguous in sequence; never re-signed
type worker struct {
	signer      txSigner
	broadcaster txBroadcaster
	buffer      *txBuffer
	requestCh   <-chan *TxRequest
	events      chan event
	pollTime    time.Duration

	mode          workerMode
	stopSeq       uint64
	stopErr       error
	recoverTarget uint64

	signing    bool
	submitting bool
	confirming bool

	// inflightSign holds the request currently being signed. It is set
	// when plan() emits cmdSign and cleared on evSignResult. Tracking it
	// outside the buffer lets drainAll resolve it on shutdown — otherwise
	// a caller would block on Await forever because the req was popped
	// from pending but not yet appended to signed when ctx was cancelled.
	inflightSign *TxRequest
}

// run is the main event loop. It exits when the parent context is
// cancelled, the request channel is closed, or stop mode has finished
// draining.
func (w *worker) run(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	confirmTick := time.NewTicker(w.pollTime)
	defer confirmTick.Stop()

	for {
		select {
		case <-ctx.Done():
			w.drainAll(ctx.Err())
			return

		case req, ok := <-w.requestCh:
			if !ok {
				w.drainAll(fmt.Errorf("request channel closed"))
				return
			}
			w.execute(ctx, w.handle(evNewRequest{req: req}))

		case <-confirmTick.C:
			w.execute(ctx, w.handle(evConfirmTick{}))

		case e := <-w.events:
			w.execute(ctx, w.handle(e))
			if w.mode == modeStopped && w.allConfirmedUpToStop() && !w.anyInFlight() {
				w.drainRemaining()
				return
			}
		}
	}
}

func (w *worker) anyInFlight() bool {
	return w.signing || w.submitting || w.confirming
}

// handle: pure state transition. No goroutines, no I/O. Returns commands
// for the dispatcher to execute.
func (w *worker) handle(e event) []command {
	switch e := e.(type) {
	case evNewRequest:
		if w.mode == modeStopped {
			return []command{cmdFinalize{req: e.req, err: w.stopErr}}
		}
		w.buffer.addPending(e.req)
		return w.plan()

	case evSignResult:
		w.signing = false
		w.inflightSign = nil
		if e.err != nil {
			return append(
				[]command{cmdFinalize{req: e.req, err: fmt.Errorf("signing tx: %w", e.err)}},
				w.plan()...,
			)
		}
		if w.mode == modeStopped {
			return append(
				[]command{cmdFinalize{req: e.req, err: w.stopErr}},
				w.plan()...,
			)
		}
		entry := txEntry{sequence: e.seq, txHash: e.hash, txBytes: e.bytes, request: e.req}
		if err := w.buffer.appendSigned(entry); err != nil {
			return append(
				[]command{cmdFinalize{req: e.req, err: fmt.Errorf("buffer append: %w", err)}},
				w.plan()...,
			)
		}
		return w.plan()

	case evSubmitResult:
		w.submitting = false
		return w.onSubmitResult(e)

	case evConfirmResult:
		w.confirming = false
		return w.onConfirmResult(e)

	case evConfirmTick:
		return w.plan()
	}
	return nil
}

// plan decides what to do next given current state. Enforces the
// "one outstanding" invariants via the in-flight gates.
func (w *worker) plan() []command {
	var cmds []command

	// Drop any ctx-cancelled pending requests, then start at most one sign.
	for w.mode == modeSubmitting && w.buffer.pendingLen() > 0 {
		if w.signing {
			break
		}
		req := w.buffer.popPending()
		if err := req.Ctx.Err(); err != nil {
			cmds = append(cmds, cmdFinalize{req: req, err: err})
			continue
		}
		w.signing = true
		w.inflightSign = req
		cmds = append(cmds, cmdSign{req: req})
		break
	}

	// Submit next unsubmitted signed entry.
	if !w.submitting && w.mode == modeSubmitting {
		if entry := w.buffer.next(); entry != nil {
			w.submitting = true
			cmds = append(cmds, cmdSubmit{seq: entry.sequence, bytes: entry.txBytes})
		}
	}

	// Confirm front entry if eligible.
	if !w.confirming {
		if entry := w.confirmCandidate(); entry != nil {
			w.confirming = true
			cmds = append(cmds, cmdConfirm{seq: entry.sequence, hash: entry.txHash})
		}
	}

	return cmds
}

func (w *worker) confirmCandidate() *txEntry {
	front := w.buffer.front()
	if front == nil {
		return nil
	}
	if w.mode == modeStopped && front.sequence >= w.stopSeq {
		return nil
	}
	if w.mode == modeSubmitting && !w.buffer.hasSubmissions() {
		return nil
	}
	return front
}

// --- submit-result handling ---

func (w *worker) onSubmitResult(e evSubmitResult) []command {
	if e.err == nil {
		w.buffer.markSubmitted(e.seq)
		return w.plan()
	}
	return w.handleSubmitError(e.seq, e.err)
}

func (w *worker) handleSubmitError(seq uint64, err error) []command {
	kind, expectedSeq := ClassifyBroadcastError(err)
	switch kind {
	case ErrSequenceMismatch:
		w.handleSubmitSequenceMismatch(expectedSeq)
	case ErrMempoolFull, ErrNetworkError:
		// non-fatal: entry is still marked unsubmitted; plan() will retry.
	case ErrTxInMempoolCache:
		w.buffer.markSubmitted(seq)
	case ErrUnrecoverable, ErrInsufficientFee:
		w.enterStop(seq, fmt.Errorf("fatal broadcast error: %w", err))
	}
	return w.plan()
}

func (w *worker) handleSubmitSequenceMismatch(expectedSeq uint64) {
	lastSubmitted := w.buffer.lastSubmittedSeq()
	if expectedSeq <= lastSubmitted {
		front := w.buffer.front()
		if front != nil && expectedSeq < front.sequence {
			w.enterStop(expectedSeq, fmt.Errorf("sequence mismatch: node expects %d, before our buffer (first=%d)", expectedSeq, front.sequence))
			return
		}
		w.buffer.reset(expectedSeq)
		return
	}
	if w.buffer.getBySequence(expectedSeq) != nil {
		w.enterRecovery(expectedSeq)
	} else {
		w.enterStop(expectedSeq, fmt.Errorf("sequence mismatch: node expects %d, beyond our signed range", expectedSeq))
	}
}

// --- confirm-result handling ---

func (w *worker) onConfirmResult(e evConfirmResult) []command {
	if e.err != nil {
		return w.plan() // transient network error; next tick will retry.
	}

	front := w.buffer.front()
	if front == nil || front.sequence != e.seq {
		// Buffer moved on (e.g., due to a drain). Ignore stale result.
		return w.plan()
	}

	switch e.status.Status {
	case core.TxStatusPending:
		return w.plan()

	case core.TxStatusCommitted:
		return w.onCommitted(front, e.status)

	case core.TxStatusEvicted:
		return w.onEvicted(front)

	case core.TxStatusRejected:
		return w.onFatalConfirm(front, fmt.Errorf("tx rejected: %s", e.status.Error))

	default:
		if w.mode == modeRecovering {
			return w.drainAllCmds(fmt.Errorf("recovery contradiction: unknown status for %s at seq %d", front.txHash, front.sequence))
		}
		return w.onFatalConfirm(front, fmt.Errorf("unknown tx status for %s: %s", front.txHash, e.status.Status))
	}
}

func (w *worker) onCommitted(front *txEntry, resp *tx.TxStatusResponse) []command {
	txResp := &sdktypes.TxResponse{
		TxHash:    front.txHash,
		Height:    resp.Height,
		Code:      resp.ExecutionCode,
		GasWanted: resp.GasWanted,
		GasUsed:   resp.GasUsed,
		Codespace: resp.Codespace,
	}
	var cerr error
	if resp.ExecutionCode != 0 {
		cerr = fmt.Errorf("tx execution failed with code %d: %s", resp.ExecutionCode, resp.Error)
	}
	confirmed := w.buffer.confirmFront()
	cmds := []command{cmdFinalize{req: confirmed.request, resp: txResp, err: cerr}}

	if w.mode == modeRecovering && w.buffer.nextSeq >= w.recoverTarget {
		w.mode = modeSubmitting
	}

	return append(cmds, w.plan()...)
}

func (w *worker) onEvicted(front *txEntry) []command {
	switch w.mode {
	case modeRecovering:
		return w.drainAllCmds(fmt.Errorf("recovery contradiction: tx evicted at seq %d", front.sequence))

	case modeStopped:
		err := fmt.Errorf("tx evicted at seq %d during stop drain", front.sequence)
		if front.sequence < w.stopSeq {
			w.stopSeq = front.sequence
			w.stopErr = err
		}
		confirmed := w.buffer.confirmFront()
		return []command{cmdFinalize{req: confirmed.request, err: err}}

	default: // modeSubmitting
		w.buffer.reset(front.sequence)
		return w.plan()
	}
}

func (w *worker) onFatalConfirm(front *txEntry, err error) []command {
	switch w.mode {
	case modeRecovering:
		return w.drainAllCmds(fmt.Errorf("recovery error at seq %d: %w", front.sequence, err))

	case modeStopped:
		if front.sequence < w.stopSeq {
			w.stopSeq = front.sequence
			w.stopErr = err
		}
		confirmed := w.buffer.confirmFront()
		return []command{cmdFinalize{req: confirmed.request, err: err}}

	default: // modeSubmitting
		w.enterStop(front.sequence, err)
		return w.plan()
	}
}

// --- mode transitions ---

func (w *worker) enterRecovery(targetSeq uint64) {
	w.mode = modeRecovering
	w.recoverTarget = targetSeq
}

func (w *worker) enterStop(seq uint64, err error) {
	if w.mode == modeStopped {
		if seq < w.stopSeq {
			w.stopSeq = seq
			w.stopErr = err
		}
		return
	}
	w.mode = modeStopped
	w.stopSeq = seq
	w.stopErr = err
}

func (w *worker) allConfirmedUpToStop() bool {
	front := w.buffer.front()
	if front == nil {
		return true
	}
	return front.sequence >= w.stopSeq
}

// drainAllCmds finalizes every pending/signed entry with err and forces
// the worker into a permanently-drained stop state. Used when a recovery
// contradiction makes the pipeline unrecoverable.
func (w *worker) drainAllCmds(err error) []command {
	var cmds []command
	for w.buffer.pendingLen() > 0 {
		cmds = append(cmds, cmdFinalize{req: w.buffer.popPending(), err: err})
	}
	for w.buffer.signedLen() > 0 {
		entry := w.buffer.confirmFront()
		cmds = append(cmds, cmdFinalize{req: entry.request, err: err})
	}
	w.mode = modeStopped
	w.stopSeq = 0
	w.stopErr = err
	return cmds
}

// drainRemaining resolves all entries from stopSeq onwards with stopErr.
// Called from the run loop once stop mode has caught up.
func (w *worker) drainRemaining() {
	for w.buffer.signedLen() > 0 {
		entry := w.buffer.confirmFront()
		entry.request.resolve(nil, w.stopErr)
	}
	for w.buffer.pendingLen() > 0 {
		w.buffer.popPending().resolve(nil, w.stopErr)
	}
}

// drainAll resolves everything still owned by the worker with err. Called
// on shutdown. Order: in-flight sign first (it was popped from pending
// before the signer started, so it lives only in inflightSign), then the
// buffer.
func (w *worker) drainAll(err error) {
	if w.inflightSign != nil {
		w.inflightSign.resolve(nil, err)
		w.inflightSign = nil
	}
	for w.buffer.pendingLen() > 0 {
		w.buffer.popPending().resolve(nil, err)
	}
	for w.buffer.signedLen() > 0 {
		entry := w.buffer.confirmFront()
		entry.request.resolve(nil, err)
	}
}

// --- dispatcher: turns commands into goroutines / synchronous resolves. ---

func (w *worker) execute(ctx context.Context, cmds []command) {
	for _, c := range cmds {
		switch c := c.(type) {
		case cmdSign:
			req := c.req
			go func() {
				b, h, s, err := w.signer.Sign(ctx, req)
				w.send(ctx, evSignResult{req: req, bytes: b, hash: h, seq: s, err: err})
			}()
		case cmdSubmit:
			seq, bytes := c.seq, c.bytes
			go func() {
				err := w.broadcaster.Submit(ctx, bytes)
				w.send(ctx, evSubmitResult{seq: seq, err: err})
			}()
		case cmdConfirm:
			seq, hash := c.seq, c.hash
			go func() {
				status, err := w.broadcaster.Status(ctx, hash)
				w.send(ctx, evConfirmResult{seq: seq, status: status, err: err})
			}()
		case cmdFinalize:
			c.req.resolve(c.resp, c.err)
		}
	}
}

// send posts an event to the loop, or drops it if ctx is cancelled. This
// prevents background goroutines from blocking forever when run() exits.
func (w *worker) send(ctx context.Context, e event) {
	select {
	case w.events <- e:
	case <-ctx.Done():
	}
}
