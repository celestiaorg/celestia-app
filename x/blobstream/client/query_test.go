package client_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v4/x/blobstream/client"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
)

func (s *CLITestSuite) TestQueryAttestationByNonce() {
	_, err := s.cctx.WaitForHeightWithTimeout(402, 2*time.Minute)
	s.Require().NoError(err)
	testCases := []struct {
		name      string
		nonce     string
		expectErr bool
	}{
		{
			name:      "query the first valset that's created on chain startup",
			nonce:     "1",
			expectErr: false,
		},
		{
			name:      "query the first data commitment",
			nonce:     "2",
			expectErr: false,
		},
		{
			name:      "negative attestation nonce",
			nonce:     "-1",
			expectErr: true,
		},
		{
			name:      "zero attestation nonce",
			nonce:     "0",
			expectErr: true,
		},
		{
			name:      "higher attestation nonce than latest attestation nonce",
			nonce:     "100",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(_ *testing.T) {
			cmd := client.CmdQueryAttestationByNonce()
			_, err := clitestutil.ExecTestCLICmd(s.cctx.Context, cmd, []string{tc.nonce})
			if tc.expectErr {
				s.Assert().Error(err)
			} else {
				s.Assert().NoError(err)
			}
		})
	}
}
