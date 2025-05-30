package abci

import (
	"context"
	"testing"

	abciv2 "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abciv1 "github.com/tendermint/tendermint/abci/types"
	"google.golang.org/grpc"
)

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
	infoResp *abciv1.ResponseInfo
}

func (m *mockABCIApplicationClient) Info(ctx context.Context, req *abciv1.RequestInfo, opts ...grpc.CallOption) (*abciv1.ResponseInfo, error) {
	return m.infoResp, nil
}
