package abci

import (
	"context"
	"testing"

	abciv2 "github.com/cometbft/cometbft/abci/types"
	typesv2 "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abciv1 "github.com/tendermint/tendermint/abci/types"
	typesv1 "github.com/tendermint/tendermint/proto/tendermint/types"
	"google.golang.org/grpc"
)

func TestFinalizeBlock(t *testing.T) {
	// minimalRequest returns a RequestFinalizeBlock with enough fields populated
	// to avoid nil dereferences in the method under test.
	minimalRequest := func() *abciv2.RequestFinalizeBlock {
		return &abciv2.RequestFinalizeBlock{
			Height: 1,
			Header: &typesv2.Header{
				LastBlockId: typesv2.BlockID{
					PartSetHeader: typesv2.PartSetHeader{},
				},
			},
		}
	}

	newMockClient := func(endBlockResp *abciv1.ResponseEndBlock) *mockABCIApplicationClient {
		return &mockABCIApplicationClient{
			infoResp:       &abciv1.ResponseInfo{AppVersion: 1},
			beginBlockResp: &abciv1.ResponseBeginBlock{},
			deliverTxResp:  &abciv1.ResponseDeliverTx{},
			endBlockResp:   endBlockResp,
			commitResp:     &abciv1.ResponseCommit{Data: []byte("apphash")},
		}
	}

	t.Run("nil ConsensusParamUpdates should not panic", func(t *testing.T) {
		mockClient := newMockClient(&abciv1.ResponseEndBlock{
			ConsensusParamUpdates: nil,
		})
		client := &RemoteABCIClientV1{ABCIApplicationClient: mockClient}

		resp, err := client.FinalizeBlock(minimalRequest())
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(0), client.endBlockConsensusAppVersion)
	})

	t.Run("nil Version in ConsensusParamUpdates should not panic", func(t *testing.T) {
		mockClient := newMockClient(&abciv1.ResponseEndBlock{
			ConsensusParamUpdates: &abciv1.ConsensusParams{
				Version: nil,
			},
		})
		client := &RemoteABCIClientV1{ABCIApplicationClient: mockClient}

		resp, err := client.FinalizeBlock(minimalRequest())
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(0), client.endBlockConsensusAppVersion)
	})

	t.Run("ConsensusParamUpdates with Version sets app version", func(t *testing.T) {
		mockClient := newMockClient(&abciv1.ResponseEndBlock{
			ConsensusParamUpdates: &abciv1.ConsensusParams{
				Version: &typesv1.VersionParams{
					AppVersion: 42,
				},
			},
		})
		client := &RemoteABCIClientV1{ABCIApplicationClient: mockClient}

		resp, err := client.FinalizeBlock(minimalRequest())
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(42), client.endBlockConsensusAppVersion)
	})
}

func TestConsensusParamsV1ToV2(t *testing.T) {
	t.Run("should return nil if params are nil", func(t *testing.T) {
		got := consensusParamsV1ToV2(nil)
		assert.Nil(t, got)
	})
}

func TestTimeoutInfoV1ToV2(t *testing.T) {
	info := abciv1.TimeoutsInfo{
		TimeoutPropose: 1,
		TimeoutCommit:  2,
	}
	want := abciv2.TimeoutInfo{
		TimeoutPropose: 1,
		TimeoutCommit:  2,
	}
	got := timeoutInfoV1ToV2(info)
	assert.Equal(t, want, got)
}

func TestInfo(t *testing.T) {
	// This test verifies the regression in
	// https://github.com/celestiaorg/celestia-app/issues/4859
	t.Run("should convert TimeoutInfo in the abciv1 response to an abciv2 response", func(t *testing.T) {
		mockClient := &mockABCIApplicationClient{
			infoResp: &abciv1.ResponseInfo{
				Timeouts: abciv1.TimeoutsInfo{
					TimeoutPropose: 1,
					TimeoutCommit:  2,
				},
			},
		}

		client := &RemoteABCIClientV1{
			ABCIApplicationClient: mockClient,
		}

		want := abciv2.TimeoutInfo{
			TimeoutPropose: 1,
			TimeoutCommit:  2,
		}
		got, err := client.Info(&abciv2.RequestInfo{})
		require.NoError(t, err)
		assert.Equal(t, want, got.TimeoutInfo)
	})
}

// mockABCIApplicationClient is a mock implementation of ABCIApplicationClient
type mockABCIApplicationClient struct {
	abciv1.ABCIApplicationClient
	infoResp       *abciv1.ResponseInfo
	beginBlockResp *abciv1.ResponseBeginBlock
	deliverTxResp  *abciv1.ResponseDeliverTx
	endBlockResp   *abciv1.ResponseEndBlock
	commitResp     *abciv1.ResponseCommit
}

func (m *mockABCIApplicationClient) Info(_ context.Context, _ *abciv1.RequestInfo, _ ...grpc.CallOption) (*abciv1.ResponseInfo, error) {
	return m.infoResp, nil
}

func (m *mockABCIApplicationClient) BeginBlock(_ context.Context, _ *abciv1.RequestBeginBlock, _ ...grpc.CallOption) (*abciv1.ResponseBeginBlock, error) {
	return m.beginBlockResp, nil
}

func (m *mockABCIApplicationClient) DeliverTx(_ context.Context, _ *abciv1.RequestDeliverTx, _ ...grpc.CallOption) (*abciv1.ResponseDeliverTx, error) {
	return m.deliverTxResp, nil
}

func (m *mockABCIApplicationClient) EndBlock(_ context.Context, _ *abciv1.RequestEndBlock, _ ...grpc.CallOption) (*abciv1.ResponseEndBlock, error) {
	return m.endBlockResp, nil
}

func (m *mockABCIApplicationClient) Commit(_ context.Context, _ *abciv1.RequestCommit, _ ...grpc.CallOption) (*abciv1.ResponseCommit, error) {
	return m.commitResp, nil
}
