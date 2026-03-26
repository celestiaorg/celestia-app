package testnode

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	"github.com/celestiaorg/go-square/v3/share"
	abci "github.com/cometbft/cometbft/abci/types"
	tmconfig "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full node integration test in short mode.")
	}
	suite.Run(t, new(IntegrationTestSuite))
}

type IntegrationTestSuite struct {
	suite.Suite

	accounts []string
	cctx     Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()
	s.accounts = testfactory.GenerateAccounts(10)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blobGenState := blobtypes.DefaultGenesis()
	blobGenState.Params.GovMaxSquareSize = uint64(appconsts.SquareSizeUpperBound)

	cfg := DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithModifiers(genesis.SetBlobParams(enc.Codec, blobGenState.Params)).
		WithDelayedPrecommitTimeout(time.Millisecond * 100)

	cctx, _, _ := NewNetwork(t, cfg)
	s.cctx = cctx
}

func (s *IntegrationTestSuite) TestPostData() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastSync, share.RandomBlobNamespace(), random.Bytes(kibibyte))
	require.NoError(err)
}

func (s *IntegrationTestSuite) TestFillBlock() {
	require := s.Require()

	for squareSize := 2; squareSize <= 64; squareSize *= 2 {
		resp, err := s.cctx.FillBlock(squareSize, s.accounts[1], flags.BroadcastSync)
		require.NoError(err)

		err = s.cctx.WaitForBlocks(3)
		require.NoError(err, squareSize)

		res, err := QueryWithoutProof(s.cctx.Context, resp.TxHash)
		require.NoError(err, squareSize)
		require.Equal(abci.CodeTypeOK, res.TxResult.Code, squareSize)

		b, err := s.cctx.Client.Block(s.cctx.GoContext(), &res.Height)
		require.NoError(err, squareSize)
		require.Equal(uint64(squareSize), b.Block.SquareSize, squareSize)
	}
}

func (s *IntegrationTestSuite) TestFillBlock_InvalidSquareSizeError() {
	tests := []struct {
		name        string
		squareSize  int
		expectedErr error
	}{
		{
			name:        "when squareSize less than 2",
			squareSize:  0,
			expectedErr: fmt.Errorf("unsupported squareSize: 0"),
		},
		{
			name:        "when squareSize is greater than 2 but not a power of 2",
			squareSize:  18,
			expectedErr: fmt.Errorf("unsupported squareSize: 18"),
		},
		{
			name:       "when squareSize is greater than 2 and a power of 2",
			squareSize: 16,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, actualErr := s.cctx.FillBlock(tc.squareSize, s.accounts[2], flags.BroadcastAsync)
			s.Equal(tc.expectedErr, actualErr)
		})
	}
}

// Test_defaultAppVersion tests that the default app version is set correctly in
// testnode node.
func (s *IntegrationTestSuite) Test_defaultAppVersion() {
	t := s.T()
	blockRes, err := s.cctx.Client.Block(s.cctx.GoContext(), nil)
	require.NoError(t, err)
	require.Equal(t, appconsts.Version, blockRes.Block.Version.App)
}

func TestGetGenDocProvider(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Create a genesis file in SDK AppGenesis format (with InitialHeight as integer)
	genesisFile := filepath.Join(configDir, "genesis.json")
	// This is the format that the SDK's init command generates
	genesisContent := `{
		"app_name": "celestia-appd",
		"app_version": "test",
		"genesis_time": "2024-01-01T00:00:00Z",
		"chain_id": "test-chain",
		"initial_height": 1,
		"app_hash": null,
		"app_state": {},
		"consensus": {
			"validators": [],
			"params": {
				"block": {"max_bytes": "22020096", "max_gas": "-1"},
				"evidence": {"max_age_num_blocks": "100000", "max_age_duration": "172800000000000", "max_bytes": "1048576"},
				"validator": {"pub_key_types": ["ed25519"]},
				"version": {"app": "0"},
				"abci": {"vote_extensions_enable_height": "0"}
			}
		}
	}`
	require.NoError(t, os.WriteFile(genesisFile, []byte(genesisContent), 0o644))

	// Create a CometBFT config pointing to the temp directory
	cfg := tmconfig.DefaultConfig()
	cfg.SetRoot(tempDir)

	// Get the genesis doc provider and call it
	provider := getGenDocProvider(cfg)
	genDoc, err := provider()

	// Verify no error occurred (this would fail with the old DefaultGenesisDocProviderFunc
	// because it can't handle InitialHeight as an integer)
	require.NoError(t, err)

	// Verify the genesis doc was parsed correctly
	assert.Equal(t, "test-chain", genDoc.ChainID)
	assert.Equal(t, int64(1), genDoc.InitialHeight)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), genDoc.GenesisTime)
}

func TestGetGenDocProvider_FileNotFound(t *testing.T) {
	// Create a CometBFT config pointing to a non-existent directory
	cfg := tmconfig.DefaultConfig()
	cfg.SetRoot("/non/existent/path")

	// Get the genesis doc provider and call it
	provider := getGenDocProvider(cfg)
	_, err := provider()

	// Verify an error occurred
	assert.Error(t, err)
}
