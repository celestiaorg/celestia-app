package _test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil/testnode"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestVersionIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SDK integration test in short mode.")
	}
	suite.Run(t, new(VersionIntegrationTestSuite))
}

type VersionIntegrationTestSuite struct {
	suite.Suite

	cleanup func() error
	cctx    testnode.Context
	ecfg    encoding.Config
}

func (s *VersionIntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")
	cleanup, _, cctx := testnode.DefaultNetwork(t, time.Millisecond*400)
	s.cleanup = cleanup
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.cctx = cctx
}

func (s *VersionIntegrationTestSuite) TearDownSuite() {
	t := s.T()
	t.Log("tearing down integration test suite")
	require.NoError(t, s.cleanup())
}

func (s *VersionIntegrationTestSuite) TestStandardSDK() {
	// t := s.T()

}
