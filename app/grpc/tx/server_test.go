package tx_test

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/grpc/tx"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newStubServer(mode int) tx.TxServer {
	return tx.NewTxServer(client.Context{}.WithClient(&stubNode{mode: mode}), nil)
}

func TestTxStatusRejectsNilNodeResult(t *testing.T) {
	_, err := newStubServer(stubNil).TxStatus(context.Background(), &tx.TxStatusRequest{TxId: "aa"})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestTxStatusBatchRejectsMalformedNodeResponses(t *testing.T) {
	ids := []string{"aa", "bb", "cc"}
	for name, mode := range map[string]int{
		"nil":        stubNil,
		"truncated":  stubTruncate,
		"drop first": stubDropFirst,
		"extra":      stubExtra,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newStubServer(mode).TxStatusBatch(context.Background(), &tx.TxStatusBatchRequest{TxIds: ids})
			require.Equal(t, codes.Internal, status.Code(err))
		})
	}
}

func TestTxStatusBatchMatchesResultsByHash(t *testing.T) {
	ids := []string{"aa", "bb", "cc"}
	res, err := newStubServer(stubReverse).TxStatusBatch(context.Background(), &tx.TxStatusBatchRequest{TxIds: ids})
	require.NoError(t, err)
	require.Len(t, res.Statuses, len(ids))
	for i, entry := range res.Statuses {
		require.Equal(t, ids[i], entry.TxHash)
		hash, err := hex.DecodeString(ids[i])
		require.NoError(t, err)
		require.Equal(t, stubStatus(hash).Status, entry.Status.Status)
	}
}

func TestTxStatusBatchRejectsEmptyID(t *testing.T) {
	_, err := newStubServer(stubFaithful).TxStatusBatch(context.Background(), &tx.TxStatusBatchRequest{TxIds: []string{"aa", ""}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
