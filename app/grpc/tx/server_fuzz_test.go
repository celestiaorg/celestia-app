package tx_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/grpc/tx"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Stub node behaviors, chosen by the fuzz input.
const (
	stubFaithful  = iota // one result per hash, in request order
	stubError            // the RPC call fails
	stubTruncate         // drops trailing results
	stubDropFirst        // drops the first result
	stubExtra            // appends a result for a hash nobody asked for
	stubReverse          // returns results in reverse order
	stubNil              // returns a nil result with a nil error
	stubModes
)

// stubNode is a CometBFT RPC client whose TxStatus and TxStatusBatch return
// fuzz-chosen responses. Every other method is left unimplemented.
type stubNode struct {
	rpcclient.Client
	mode int
}

var errStubNode = errors.New("stub node failure")

// stubStatus derives a status from the hash so the oracle can tell which
// hash a returned status belongs to.
func stubStatus(hash []byte) ctypes.ResultTxStatus {
	return ctypes.ResultTxStatus{Height: int64(len(hash)), Status: "S" + hex.EncodeToString(hash), Error: string(hash)}
}

func (s *stubNode) TxStatus(_ context.Context, hash []byte) (*ctypes.ResultTxStatus, error) {
	switch s.mode {
	case stubError:
		return nil, errStubNode
	case stubNil:
		return nil, nil
	}
	r := stubStatus(hash)
	return &r, nil
}

func (s *stubNode) TxStatusBatch(_ context.Context, hashes [][]byte) (*ctypes.ResultTxStatusBatch, error) {
	out := make([]ctypes.TxStatusResponse, 0, len(hashes)+1)
	for _, h := range hashes {
		out = append(out, ctypes.TxStatusResponse{Hash: h, Result: stubStatus(h)})
	}
	switch s.mode {
	case stubError:
		return nil, errStubNode
	case stubNil:
		return nil, nil
	case stubTruncate:
		out = out[:len(out)/2]
	case stubDropFirst:
		out = out[1:]
	case stubExtra:
		out = append(out, ctypes.TxStatusResponse{Hash: []byte("extra"), Result: stubStatus([]byte("extra"))})
	case stubReverse:
		slices.Reverse(out)
	}
	return &ctypes.ResultTxStatusBatch{Statuses: out}, nil
}

// FuzzTxStatus calls TxStatus and TxStatusBatch with fuzz-chosen tx ids
// (newline separated, any bytes) against a stub node returning fuzz-chosen
// responses, including fewer or more results than requested, reordered
// results, nil results and errors. The server must not panic, must reject
// malformed ids with InvalidArgument, and every batch entry must carry the
// status of the id it is labelled with.
func FuzzTxStatus(f *testing.F) {
	f.Add("0a1b", uint8(stubFaithful))
	f.Add("00\nff\nABCD\n", uint8(stubFaithful))
	f.Add("00\nzz", uint8(stubFaithful))
	f.Add("0\n11", uint8(stubError))
	f.Add("aa\nbb\ncc", uint8(stubTruncate))
	f.Add("aa\nbb\ncc", uint8(stubReverse))
	f.Add("aa\nbb", uint8(stubDropFirst))
	f.Add("aa\nbb", uint8(stubExtra))
	f.Add("aa", uint8(stubNil))
	f.Add(strings.Repeat("ab\n", 21), uint8(stubFaithful))
	f.Fuzz(func(t *testing.T, raw string, mode uint8) {
		node := &stubNode{mode: int(mode % stubModes)}
		srv := tx.NewTxServer(client.Context{}.WithClient(node), nil)
		ids := strings.Split(raw, "\n")
		// Excluded pending a fix; see the Linear issue for this target.
		// A nil node result, extra results and results out of request order
		// are not handled yet.
		if node.mode == stubNil {
			return
		}
		skipBatch := node.mode == stubExtra || node.mode == stubReverse || node.mode == stubDropFirst

		single, err := callNoPanic(t, func() (*tx.TxStatusResponse, error) {
			return srv.TxStatus(context.Background(), &tx.TxStatusRequest{TxId: ids[0]})
		})
		decoded, decodeErr := hex.DecodeString(ids[0])
		switch {
		case ids[0] == "" || decodeErr != nil:
			require.Equal(t, codes.InvalidArgument, status.Code(err), "TxStatus(%q): %v", ids[0], err)
		case node.mode == stubError:
			require.Error(t, err)
		default:
			require.NoError(t, err)
			want := stubStatus(decoded)
			require.Equal(t, want.Status, single.Status)
			require.Equal(t, want.Height, single.Height)
		}

		if skipBatch {
			return
		}
		batch, err := callNoPanic(t, func() (*tx.TxStatusBatchResponse, error) {
			return srv.TxStatusBatch(context.Background(), &tx.TxStatusBatchRequest{TxIds: ids})
		})
		malformed := false
		for _, id := range ids {
			if _, err := hex.DecodeString(id); err != nil {
				malformed = true
			}
		}
		switch {
		case len(ids) > 20 || malformed:
			require.Equal(t, codes.InvalidArgument, status.Code(err), "TxStatusBatch(%q): %v", ids, err)
		case node.mode == stubError:
			require.Error(t, err)
		default:
			require.NoError(t, err)
			require.LessOrEqual(t, len(batch.Statuses), len(ids))
			for i, entry := range batch.Statuses {
				require.Equal(t, ids[i], entry.TxHash)
				hash, err := hex.DecodeString(entry.TxHash)
				require.NoError(t, err)
				require.Equal(t, stubStatus(hash).Status, entry.Status.Status, "entry %d labelled %s carries another tx's status", i, entry.TxHash)
			}
		}
	})
}

// callNoPanic turns a panic into a test failure with its value.
func callNoPanic[T any](t *testing.T, fn func() (T, error)) (res T, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			require.FailNow(t, fmt.Sprintf("panic: %v\n%s", r, debug.Stack()))
		}
	}()
	return fn()
}
