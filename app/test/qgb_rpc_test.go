package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobstreamRPCQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping blobstream integration test in short mode.")
	}
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().
		WithModifiers(genesis.SetDataCommitmentWindow(ecfg.Codec, 100)).
		WithConsensusParams(app.DefaultInitialConsensusParams())

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	h, err := cctx.WaitForHeightWithTimeout(105, 2*time.Minute)
	require.NoError(t, err, h)
	require.Greater(t, h, int64(101))

	queryClient := types.NewQueryClient(cctx.GRPCClient)

	type test struct {
		name string
		req  func() error
	}
	tests := []test{
		{
			name: "attestation request by nonce",
			req: func() error {
				_, err := queryClient.AttestationRequestByNonce(
					context.Background(),
					&types.QueryAttestationRequestByNonceRequest{Nonce: 1},
				)
				return err
			},
		},
		{
			name: "last unbonding height",
			req: func() error {
				_, err := queryClient.LatestUnbondingHeight(
					context.Background(),
					&types.QueryLatestUnbondingHeightRequest{},
				)
				return err
			},
		},
		{
			name: "earliest attestation nonce",
			req: func() error {
				_, err := queryClient.EarliestAttestationNonce(
					context.Background(),
					&types.QueryEarliestAttestationNonceRequest{},
				)
				return err
			},
		},
		{
			name: "data commitment range for height",
			req: func() error {
				_, err := queryClient.DataCommitmentRangeForHeight(
					context.Background(),
					&types.QueryDataCommitmentRangeForHeightRequest{Height: 10},
				)
				return err
			},
		},
		{
			name: "latest attestation nonce",
			req: func() error {
				_, err := queryClient.LatestAttestationNonce(
					context.Background(),
					&types.QueryLatestAttestationNonceRequest{},
				)
				return err
			},
		},
		{
			name: "last valset before nonce",
			req: func() error {
				_, err := queryClient.LatestValsetRequestBeforeNonce(
					context.Background(),
					&types.QueryLatestValsetRequestBeforeNonceRequest{Nonce: 2},
				)
				return err
			},
		},
		{
			name: "params",
			req: func() error {
				_, err := queryClient.Params(
					context.Background(),
					&types.QueryParamsRequest{},
				)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req()
			assert.NoError(t, err)
		})
	}
}
