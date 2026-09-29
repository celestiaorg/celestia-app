package fibre_test

import (
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// TestServerRejectsPromisesChainWouldReject checks that the server never signs a
// promise that on-chain PaymentPromise.ValidateBasic would reject, since such a
// promise can never be settled via MsgPayForFibre or MsgPaymentPromiseTimeout.
func TestServerRejectsPromisesChainWouldReject(t *testing.T) {
	server, vals, val := makeTestServerWithConfig(t, nil)
	cases := map[string]func(*types.PaymentPromise){
		"blob version 256":   func(p *types.PaymentPromise) { p.BlobVersion = 256 },
		"negative height":    func(p *types.PaymentPromise) { p.Height = -1 },
		"min int64 height":   func(p *types.PaymentPromise) { p.Height = math.MinInt64 },
		"tx namespace":       func(p *types.PaymentPromise) { p.Namespace = share.TxNamespace.Bytes() },
		"parity namespace":   func(p *types.PaymentPromise) { p.Namespace = share.ParitySharesNamespace.Bytes() },
		"tail pad namespace": func(p *types.PaymentPromise) { p.Namespace = share.TailPaddingNamespace.Bytes() },
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			req := makeTestRequest(t, vals, val, func(r *types.UploadShardRequest) { modify(r.Promise) })
			require.Error(t, req.Promise.ValidateBasic(), "precondition: chain rejects this promise")
			resp, err := server.UploadShard(t.Context(), req)
			require.Error(t, err)
			require.Nil(t, resp)
		})
	}
}
