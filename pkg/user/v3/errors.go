package v3

import (
	"strings"

	apperrors "github.com/celestiaorg/celestia-app/v9/app/errors"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BroadcastErrorKind classifies broadcast errors for the async pipeline.
// It is a discriminator, not an error value — use Kind* constants below.
type BroadcastErrorKind int

const (
	// KindSequenceMismatch indicates a nonce/sequence mismatch (wrong sequence).
	KindSequenceMismatch BroadcastErrorKind = iota
	// KindMempoolFull indicates the remote node's mempool is full.
	KindMempoolFull
	// KindTxInMempoolCache indicates the tx already exists in the node's mempool cache.
	KindTxInMempoolCache
	// KindInsufficientFee indicates insufficient fee for the transaction.
	KindInsufficientFee
	// KindNetworkError indicates a gRPC transport or connectivity error.
	KindNetworkError
	// KindTerminal indicates a non-recoverable broadcast error.
	KindTerminal
)

// ClassifyBroadcastError classifies a broadcast error into a BroadcastErrorKind.
// For KindSequenceMismatch, it also returns the expected sequence number.
// For all other kinds, expectedSeq is 0.
func ClassifyBroadcastError(err error) (kind BroadcastErrorKind, expectedSeq uint64) {
	if err == nil {
		return KindTerminal, 0
	}

	// Check for gRPC transport errors first.
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return KindNetworkError, 0
		}
	}

	// Check for BroadcastTxError from v1.
	broadcastErr, ok := err.(*user.BroadcastTxError)
	if !ok {
		// Non-BroadcastTxError: check string patterns for network-level errors.
		errMsg := err.Error()
		if strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "connection reset") {
			return KindNetworkError, 0
		}
		return KindTerminal, 0
	}

	// Classify based on the broadcast error code and message.
	if apperrors.IsNonceMismatchCode(broadcastErr.Code) {
		seq, parseErr := apperrors.ParseExpectedSequence(broadcastErr.ErrorLog)
		if parseErr != nil {
			return KindTerminal, 0
		}
		return KindSequenceMismatch, seq
	}

	errMsg := broadcastErr.ErrorLog
	if strings.Contains(errMsg, "mempool is full") {
		return KindMempoolFull, 0
	}
	if strings.Contains(errMsg, "tx already exists in cache") {
		return KindTxInMempoolCache, 0
	}
	if broadcastErr.Code == sdkerrors.ErrInsufficientFee.ABCICode() {
		return KindInsufficientFee, 0
	}

	return KindTerminal, 0
}
