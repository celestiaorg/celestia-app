package v3

import (
	"strings"

	apperrors "github.com/celestiaorg/celestia-app/v9/app/errors"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SubmitError classifies broadcast errors for the async pipeline.
type SubmitError int

const (
	// ErrSequenceMismatch indicates a nonce/sequence mismatch (wrong sequence).
	ErrSequenceMismatch SubmitError = iota
	// ErrMempoolFull indicates the remote node's mempool is full.
	ErrMempoolFull
	// ErrTxInMempoolCache indicates the tx already exists in the node's mempool cache.
	ErrTxInMempoolCache
	// ErrInsufficientFee indicates insufficient fee for the transaction.
	ErrInsufficientFee
	// ErrNetworkError indicates a gRPC transport or connectivity error.
	ErrNetworkError
	// ErrTerminal indicates a non-recoverable broadcast error.
	ErrTerminal
)

// ClassifyBroadcastError classifies a broadcast error into a SubmitError.
// For ErrSequenceMismatch, it also returns the expected sequence number.
// For all other kinds, expectedSeq is 0.
func ClassifyBroadcastError(err error) (kind SubmitError, expectedSeq uint64) {
	if err == nil {
		return ErrTerminal, 0
	}

	// Check for gRPC transport errors first.
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return ErrNetworkError, 0
		}
	}

	// Check for BroadcastTxError from v1.
	broadcastErr, ok := err.(*user.BroadcastTxError)
	if !ok {
		// Non-BroadcastTxError: check string patterns for network-level errors.
		errMsg := err.Error()
		if strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "connection reset") {
			return ErrNetworkError, 0
		}
		return ErrTerminal, 0
	}

	// Classify based on the broadcast error code and message.
	if apperrors.IsNonceMismatchCode(broadcastErr.Code) {
		seq, parseErr := apperrors.ParseExpectedSequence(broadcastErr.ErrorLog)
		if parseErr != nil {
			return ErrTerminal, 0
		}
		return ErrSequenceMismatch, seq
	}

	errMsg := broadcastErr.ErrorLog
	if strings.Contains(errMsg, "mempool is full") {
		return ErrMempoolFull, 0
	}
	if strings.Contains(errMsg, "tx already exists in cache") {
		return ErrTxInMempoolCache, 0
	}
	if broadcastErr.Code == sdkerrors.ErrInsufficientFee.ABCICode() {
		return ErrInsufficientFee, 0
	}

	return ErrTerminal, 0
}
